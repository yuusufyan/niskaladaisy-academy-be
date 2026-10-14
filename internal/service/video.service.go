package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/yuusufyan/go-common/pkg/apperror"
	"github.com/yuusufyan/go-common/pkg/logger"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

// VideoInput is the body of POST /videos and PUT /videos/{id}.
// IsPremium defaults to false on create and keeps the current value on update when omitted.
type VideoInput struct {
	Title       string `json:"title" validate:"required,max=200"`
	Description string `json:"description" validate:"max=2000"`
	VideoURL    string `json:"video_url" validate:"required,http_url,max=500"`
	IsPremium   *bool  `json:"is_premium"`
}

// VideoResponse is a video as seen by the admins who manage the catalog.
type VideoResponse struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	VideoURL    string    `json:"video_url"`
	IsPremium   bool      `json:"is_premium"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// VideoCatalogItem is a video as seen by the caller. VideoURL is left out when Locked.
type VideoCatalogItem struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	IsPremium   bool      `json:"is_premium"`
	Locked      bool      `json:"locked"`
	VideoURL    string    `json:"video_url,omitempty"`
}

type videoStore interface {
	List(ctx context.Context, page, limit int) ([]model.Video, int64, error)
	FindByID(ctx context.Context, id uuid.UUID) (*model.Video, error)
	Create(ctx context.Context, v *model.Video) error
	Update(ctx context.Context, id uuid.UUID, fields map[string]any) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type membershipChecker interface {
	IsActive(ctx context.Context, userID uuid.UUID) (bool, error)
}

// VideoService manages the video catalog and decides who may play premium videos.
type VideoService struct {
	videos      videoStore
	memberships membershipChecker
	log         logger.Logger
}

// NewVideoService returns a VideoService.
func NewVideoService(videos videoStore, memberships membershipChecker, log logger.Logger) *VideoService {
	return &VideoService{videos: videos, memberships: memberships, log: log}
}

// List returns one page of the catalog with premium videos locked for the caller when needed.
func (s *VideoService) List(ctx context.Context, a *Access, p Page) ([]VideoCatalogItem, int64, Page, error) {
	p = p.Normalize()
	videos, total, err := s.videos.List(ctx, p.Page, p.Limit)
	if err != nil {
		return nil, 0, p, fmt.Errorf("list videos: %w", err)
	}

	g := s.newGate(a)
	items := make([]VideoCatalogItem, 0, len(videos))
	for i := range videos {
		item, err := g.catalogItem(ctx, &videos[i])
		if err != nil {
			return nil, 0, p, err
		}
		items = append(items, item)
	}
	return items, total, p, nil
}

// Get returns one catalog video with the same locking rules as List.
func (s *VideoService) Get(ctx context.Context, a *Access, id uuid.UUID) (*VideoCatalogItem, error) {
	v, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	item, err := s.newGate(a).catalogItem(ctx, v)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// Create adds a video to the catalog.
func (s *VideoService) Create(ctx context.Context, in VideoInput) (*VideoResponse, error) {
	title, err := videoTitle(in.Title)
	if err != nil {
		return nil, err
	}
	v := &model.Video{
		Title:       title,
		Description: strings.TrimSpace(in.Description),
		VideoURL:    in.VideoURL,
		IsPremium:   in.IsPremium != nil && *in.IsPremium,
	}
	if err := s.videos.Create(ctx, v); err != nil {
		return nil, fmt.Errorf("create video: %w", err)
	}
	return s.response(ctx, v.ID)
}

// Update replaces a video's fields; is_premium is kept when omitted.
func (s *VideoService) Update(ctx context.Context, id uuid.UUID, in VideoInput) (*VideoResponse, error) {
	if _, err := s.find(ctx, id); err != nil {
		return nil, err
	}
	title, err := videoTitle(in.Title)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{
		"title":       title,
		"description": strings.TrimSpace(in.Description),
		"video_url":   in.VideoURL,
	}
	if in.IsPremium != nil {
		fields["is_premium"] = *in.IsPremium
	}
	if err := s.videos.Update(ctx, id, fields); err != nil {
		return nil, fmt.Errorf("update video: %w", err)
	}
	return s.response(ctx, id)
}

// Delete soft-deletes a video so it disappears from the catalog.
func (s *VideoService) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.find(ctx, id); err != nil {
		return err
	}
	if err := s.videos.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete video: %w", err)
	}
	return nil
}

func (s *VideoService) find(ctx context.Context, id uuid.UUID) (*model.Video, error) {
	v, err := s.videos.FindByID(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, apperror.NotFound("video not found")
		}
		return nil, fmt.Errorf("find video: %w", err)
	}
	return v, nil
}

func (s *VideoService) response(ctx context.Context, id uuid.UUID) (*VideoResponse, error) {
	v, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	return &VideoResponse{
		ID:          v.ID,
		Title:       v.Title,
		Description: v.Description,
		VideoURL:    v.VideoURL,
		IsPremium:   v.IsPremium,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
	}, nil
}

func videoTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", apperror.BadRequest("title must not be empty")
	}
	return title, nil
}

func (s *VideoService) newGate(a *Access) *premiumGate {
	return &premiumGate{log: s.log, memberships: s.memberships, access: a}
}

// premiumGate decides, for one request, whether the caller may see premium video URLs.
// The caller's membership is read at most once and only when a premium video shows up.
type premiumGate struct {
	log         logger.Logger
	memberships membershipChecker
	access      *Access
	premium     *bool
}

func (g *premiumGate) catalogItem(ctx context.Context, v *model.Video) (VideoCatalogItem, error) {
	item := VideoCatalogItem{
		ID:          v.ID,
		Title:       v.Title,
		Description: v.Description,
		IsPremium:   v.IsPremium,
		Locked:      true,
	}
	unlocked, err := g.unlocked(ctx, v)
	if err != nil {
		return VideoCatalogItem{}, err
	}
	if unlocked {
		item.Locked = false
		item.VideoURL = v.VideoURL
	}
	return item, nil
}

func (g *premiumGate) unlocked(ctx context.Context, v *model.Video) (bool, error) {
	if !v.IsPremium {
		return true, nil
	}
	if g.premium == nil {
		userID, err := uuid.Parse(g.access.UserID)
		if err != nil {
			return false, fmt.Errorf("parse caller id: %w", err)
		}
		active, err := g.memberships.IsActive(ctx, userID)
		if err != nil {
			return false, err
		}
		g.premium = &active
	}
	if *g.premium {
		return true, nil
	}

	reason := premiumBypassReason(g.access)
	if reason == "" {
		return false, nil
	}
	// Never log the URL itself: the entry exists to review who used the bypass.
	g.log.WithCtx(ctx).WithFields(logrus.Fields{
		"user_id":       g.access.UserID,
		"video_id":      v.ID.String(),
		"bypass_reason": reason,
	}).Info("premium video unlocked by bypass")
	return true, nil
}

// premiumBypassReason returns why the caller may play premium videos without a membership,
// or "" when they may not. Only superadmin and the video permissions qualify.
func premiumBypassReason(a *Access) string {
	if a.IsSuperAdmin {
		return model.RoleSuperAdmin
	}
	for _, p := range []string{model.PermVideoCreate, model.PermVideoUpdate, model.PermVideoDelete} {
		if slices.Contains(a.Permissions, p) {
			return p
		}
	}
	return ""
}
