package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/apperror"
	"github.com/yuusufyan/go-common/pkg/database"
	"github.com/yuusufyan/go-common/pkg/utils"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/repository"
)

type ListUsersInput struct {
	Search   string
	IsActive *bool
	Page
}

type CreateUserInput struct {
	Name     string      `json:"name" validate:"required,max=150"`
	Email    string      `json:"email" validate:"required,email,max=150"`
	Password string      `json:"password" validate:"required,min=8,max=72"`
	IsActive *bool       `json:"is_active"`
	RoleIDs  []uuid.UUID `json:"role_ids"`
}

// UpdateUserInput leaves roles untouched when role_ids is omitted.
type UpdateUserInput struct {
	Name     string       `json:"name" validate:"required,max=150"`
	Email    string       `json:"email" validate:"required,email,max=150"`
	IsActive *bool        `json:"is_active"`
	RoleIDs  *[]uuid.UUID `json:"role_ids"`
}

type ResetPasswordInput struct {
	NewPassword string `json:"new_password" validate:"required,min=8,max=72"`
}

type UserService struct {
	tx     database.TxManager
	users  *repository.UserRepository
	roles  *repository.RoleRepository
	tokens *repository.TokenRepository
	access *AccessService
}

func NewUserService(tx database.TxManager, users *repository.UserRepository, roles *repository.RoleRepository, tokens *repository.TokenRepository, access *AccessService) *UserService {
	return &UserService{tx: tx, users: users, roles: roles, tokens: tokens, access: access}
}

func (s *UserService) List(ctx context.Context, in ListUsersInput) ([]model.User, int64, Page, error) {
	p := in.Page.Normalize()
	users, total, err := s.users.List(ctx, repository.UserFilter{
		Search:   strings.TrimSpace(in.Search),
		IsActive: in.IsActive,
		Page:     p.Page,
		Limit:    p.Limit,
	})
	return users, total, p, err
}

func (s *UserService) Get(ctx context.Context, id uuid.UUID) (*model.User, error) {
	user, err := s.users.FindByID(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, apperror.NotFound("user not found")
		}
		return nil, err
	}
	return user, nil
}

func (s *UserService) Create(ctx context.Context, actor *Access, in CreateUserInput) (*model.User, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if err := s.ensureEmailFree(ctx, email, nil); err != nil {
		return nil, err
	}
	roleIDs := dedupe(in.RoleIDs)
	if err := s.checkAssignableRoles(ctx, actor, roleIDs); err != nil {
		return nil, err
	}
	hash, err := utils.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	user := &model.User{
		Name:         strings.TrimSpace(in.Name),
		Email:        email,
		PasswordHash: hash,
		IsActive:     in.IsActive == nil || *in.IsActive,
	}
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.users.Create(ctx, user); err != nil {
			return err
		}
		return s.users.ReplaceRoles(ctx, user.ID, roleIDs)
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperror.Conflict("email is already used")
		}
		return nil, err
	}
	return s.Get(ctx, user.ID)
}

func (s *UserService) Update(ctx context.Context, actor *Access, id uuid.UUID, in UpdateUserInput) (*model.User, error) {
	user, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkCanManage(actor, user); err != nil {
		return nil, err
	}
	isSelf := actor.UserID == id.String()
	if isSelf && in.IsActive != nil && !*in.IsActive {
		return nil, apperror.BadRequest("you cannot deactivate your own account")
	}

	email := strings.ToLower(strings.TrimSpace(in.Email))
	if err := s.ensureEmailFree(ctx, email, &id); err != nil {
		return nil, err
	}

	var roleIDs []uuid.UUID
	if in.RoleIDs != nil {
		roleIDs = dedupe(*in.RoleIDs)
		if err := s.checkAssignableRoles(ctx, actor, roleIDs); err != nil {
			return nil, err
		}
		// Without this a superadmin could lock everyone out by dropping their own role.
		if isSelf && user.HasRole(model.RoleSuperAdmin) {
			super, err := s.roles.FindByCode(ctx, model.RoleSuperAdmin)
			if err != nil {
				return nil, err
			}
			if !containsID(roleIDs, super.ID) {
				return nil, apperror.BadRequest("you cannot remove your own superadmin role")
			}
		}
	}

	fields := map[string]any{"name": strings.TrimSpace(in.Name), "email": email}
	if in.IsActive != nil {
		fields["is_active"] = *in.IsActive
	}

	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.users.Update(ctx, id, fields); err != nil {
			return err
		}
		if in.RoleIDs == nil {
			return nil
		}
		return s.users.ReplaceRoles(ctx, id, roleIDs)
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperror.Conflict("email is already used")
		}
		return nil, err
	}
	s.access.Invalidate(ctx)
	if in.IsActive != nil && !*in.IsActive {
		if err := s.tokens.RevokeAllForUser(ctx, id); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, id)
}

// ResetPassword lets an admin set a new password; the user's sessions are revoked.
func (s *UserService) ResetPassword(ctx context.Context, actor *Access, id uuid.UUID, in ResetPasswordInput) error {
	user, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.checkCanManage(actor, user); err != nil {
		return err
	}
	hash, err := utils.HashPassword(in.NewPassword)
	if err != nil {
		return err
	}
	if err := s.users.Update(ctx, id, map[string]any{"password_hash": hash}); err != nil {
		return err
	}
	return s.tokens.RevokeAllForUser(ctx, id)
}

func (s *UserService) Delete(ctx context.Context, actor *Access, id uuid.UUID) error {
	if actor.UserID == id.String() {
		return apperror.BadRequest("you cannot delete your own account")
	}
	user, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.checkCanManage(actor, user); err != nil {
		return err
	}
	if err := s.users.Delete(ctx, id); err != nil {
		return err
	}
	s.access.Invalidate(ctx)
	return s.tokens.RevokeAllForUser(ctx, id)
}

func (s *UserService) ensureEmailFree(ctx context.Context, email string, excludeID *uuid.UUID) error {
	taken, err := s.users.EmailTaken(ctx, email, excludeID)
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("email is already used")
	}
	return nil
}

// checkAssignableRoles rejects unknown roles, and system roles for non-superadmins,
// so holding user.update is not enough to grant yourself superadmin.
func (s *UserService) checkAssignableRoles(ctx context.Context, actor *Access, roleIDs []uuid.UUID) error {
	if len(roleIDs) == 0 {
		return nil
	}
	roles, err := s.roles.FindByIDs(ctx, roleIDs)
	if err != nil {
		return err
	}
	if len(roles) != len(roleIDs) {
		return apperror.BadRequest("one or more roles do not exist")
	}
	if actor.IsSuperAdmin {
		return nil
	}
	for _, r := range roles {
		if r.IsSystem {
			return apperror.Forbidden("only a superadmin can assign the " + r.Code + " role")
		}
	}
	return nil
}

// checkCanManage stops non-superadmins from changing superadmin accounts.
func (s *UserService) checkCanManage(actor *Access, target *model.User) error {
	if !actor.IsSuperAdmin && target.HasRole(model.RoleSuperAdmin) {
		return apperror.Forbidden("only a superadmin can manage a superadmin account")
	}
	return nil
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
