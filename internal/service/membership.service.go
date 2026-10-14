package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/apperror"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

// SetMembershipInput is the body of PUT /users/{id}/membership.
// ExpiresAt must be RFC 3339; omit it (or send null) for a premium without end date.
type SetMembershipInput struct {
	Status    string     `json:"status" validate:"required,oneof=free premium"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// MembershipResponse is a user's membership. IsActive is computed at read time.
// UpdatedAt is null when no status was ever set for the user.
type MembershipResponse struct {
	UserID    uuid.UUID  `json:"user_id"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at"`
	IsActive  bool       `json:"is_active"`
	UpdatedAt *time.Time `json:"updated_at"`
}

type membershipStore interface {
	FindByUserID(ctx context.Context, userID uuid.UUID) (*model.Membership, error)
	Upsert(ctx context.Context, m *model.Membership) error
}

type userGetter interface {
	Get(ctx context.Context, id uuid.UUID) (*model.User, error)
}

// MembershipService manages the premium membership of users.
type MembershipService struct {
	memberships membershipStore
	users       userGetter
}

// NewMembershipService returns a MembershipService. users answers 404 for unknown users.
func NewMembershipService(memberships membershipStore, users userGetter) *MembershipService {
	return &MembershipService{memberships: memberships, users: users}
}

// Get returns the user's membership, or a free one when no status was ever set.
func (s *MembershipService) Get(ctx context.Context, userID uuid.UUID) (*MembershipResponse, error) {
	if _, err := s.users.Get(ctx, userID); err != nil {
		return nil, err
	}
	m, err := s.memberships.FindByUserID(ctx, userID)
	if err != nil {
		if isNotFound(err) {
			return &MembershipResponse{UserID: userID, Status: model.MembershipStatusFree}, nil
		}
		return nil, fmt.Errorf("find membership: %w", err)
	}
	return newMembershipResponse(m, time.Now()), nil
}

// Set overwrites the user's membership. Setting free always clears the expiry.
func (s *MembershipService) Set(ctx context.Context, userID uuid.UUID, in SetMembershipInput) (*MembershipResponse, error) {
	now := time.Now()
	if in.ExpiresAt != nil {
		if in.Status == model.MembershipStatusFree {
			return nil, apperror.BadRequest("expires_at must be empty when status is free")
		}
		if !in.ExpiresAt.After(now) {
			return nil, apperror.BadRequest("expires_at must be in the future")
		}
	}
	if _, err := s.users.Get(ctx, userID); err != nil {
		return nil, err
	}

	m := &model.Membership{UserID: userID, Status: in.Status, ExpiresAt: in.ExpiresAt}
	if err := s.memberships.Upsert(ctx, m); err != nil {
		return nil, fmt.Errorf("save membership: %w", err)
	}
	return newMembershipResponse(m, now), nil
}

// IsActive reports whether the user currently has premium access. It always reads the
// database so a status change applies on the next request.
func (s *MembershipService) IsActive(ctx context.Context, userID uuid.UUID) (bool, error) {
	m, err := s.memberships.FindByUserID(ctx, userID)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("find membership: %w", err)
	}
	return m.IsActive(time.Now()), nil
}

func newMembershipResponse(m *model.Membership, now time.Time) *MembershipResponse {
	updatedAt := m.UpdatedAt
	return &MembershipResponse{
		UserID:    m.UserID,
		Status:    m.Status,
		ExpiresAt: m.ExpiresAt,
		IsActive:  m.IsActive(now),
		UpdatedAt: &updatedAt,
	}
}
