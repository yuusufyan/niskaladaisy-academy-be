// Package service holds the business logic of the service.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/yuusufyan/go-common/pkg/apperror"
	"github.com/yuusufyan/go-common/pkg/logger"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/repository"
)

const (
	accessCacheTTL        = 10 * time.Minute
	accessCacheVersionKey = "um:access:version"
)

// Access is the authenticated user's identity and effective permissions.
// UserID is read by the go-common audit plugin to fill created_by/updated_by.
type Access struct {
	UserID       string   `json:"user_id"`
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	Roles        []string `json:"roles"`
	Permissions  []string `json:"permissions"`
	IsSuperAdmin bool     `json:"is_super_admin"`
}

// Can reports whether the user holds any of the given permissions. Superadmin always can.
func (a *Access) Can(perms ...string) bool {
	if a.IsSuperAdmin {
		return true
	}
	for _, p := range perms {
		if slices.Contains(a.Permissions, p) {
			return true
		}
	}
	return false
}

// AccessService resolves a user's access from the database, cached in Redis when available.
type AccessService struct {
	users *repository.UserRepository
	perms *repository.PermissionRepository
	rdb   *redis.Client
	log   logger.Logger
}

func NewAccessService(users *repository.UserRepository, perms *repository.PermissionRepository, rdb *redis.Client, log logger.Logger) *AccessService {
	return &AccessService{users: users, perms: perms, rdb: rdb, log: log}
}

// Get returns the access of an active user, or 401 when the user is missing or inactive.
func (s *AccessService) Get(ctx context.Context, userID string) (*Access, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, apperror.Unauthorized("invalid token subject")
	}

	key := s.cacheKey(ctx, userID)
	if key != "" {
		if raw, err := s.rdb.Get(ctx, key).Bytes(); err == nil {
			var a Access
			if json.Unmarshal(raw, &a) == nil {
				return &a, nil
			}
		}
	}

	a, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}

	if key != "" {
		if raw, err := json.Marshal(a); err == nil {
			if err := s.rdb.Set(ctx, key, raw, accessCacheTTL).Err(); err != nil {
				s.log.WithCtx(ctx).WithError(err).Warn("cache user access")
			}
		}
	}
	return a, nil
}

// Invalidate drops every cached access. Call it after any change to users, roles, permissions or menus.
func (s *AccessService) Invalidate(ctx context.Context) {
	if s.rdb == nil {
		return
	}
	if err := s.rdb.Incr(ctx, accessCacheVersionKey).Err(); err != nil {
		s.log.WithCtx(ctx).WithError(err).Warn("invalidate user access cache")
	}
}

// cacheKey embeds a global version so Invalidate only needs one INCR. Empty means no caching.
func (s *AccessService) cacheKey(ctx context.Context, userID string) string {
	if s.rdb == nil {
		return ""
	}
	ver, err := s.rdb.Get(ctx, accessCacheVersionKey).Result()
	if errors.Is(err, redis.Nil) {
		ver = "0"
	} else if err != nil {
		return ""
	}
	return fmt.Sprintf("um:access:%s:%s", ver, userID)
}

func (s *AccessService) load(ctx context.Context, id uuid.UUID) (*Access, error) {
	user, err := s.users.FindByID(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, apperror.Unauthorized("user no longer exists")
		}
		return nil, err
	}
	if !user.IsActive {
		return nil, apperror.Unauthorized("user is inactive")
	}

	roles := make([]string, 0, len(user.Roles))
	for _, r := range user.Roles {
		roles = append(roles, r.Code)
	}
	isSuper := user.HasRole(model.RoleSuperAdmin)

	var perms []string
	if isSuper {
		perms, err = s.perms.AllCodes(ctx)
	} else {
		perms, err = s.perms.CodesForUser(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	if perms == nil {
		perms = []string{}
	}

	return &Access{
		UserID:       user.ID.String(),
		Name:         user.Name,
		Email:        user.Email,
		Roles:        roles,
		Permissions:  perms,
		IsSuperAdmin: isSuper,
	}, nil
}
