package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/apperror"
	"github.com/yuusufyan/go-common/pkg/database"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/repository"
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type ListRolesInput struct {
	Search string
	Page
}

type CreateRoleInput struct {
	Code          string      `json:"code" validate:"required,max=50"`
	Name          string      `json:"name" validate:"required,max=100"`
	Description   string      `json:"description" validate:"max=500"`
	PermissionIDs []uuid.UUID `json:"permission_ids"`
	MenuIDs       []uuid.UUID `json:"menu_ids"`
}

// UpdateRoleInput leaves permissions/menus untouched when their field is omitted.
// The code is immutable because it may be referenced from code.
type UpdateRoleInput struct {
	Name          string       `json:"name" validate:"required,max=100"`
	Description   string       `json:"description" validate:"max=500"`
	PermissionIDs *[]uuid.UUID `json:"permission_ids"`
	MenuIDs       *[]uuid.UUID `json:"menu_ids"`
}

type RoleService struct {
	tx     database.TxManager
	roles  *repository.RoleRepository
	perms  *repository.PermissionRepository
	menus  *repository.MenuRepository
	access *AccessService
}

func NewRoleService(tx database.TxManager, roles *repository.RoleRepository, perms *repository.PermissionRepository, menus *repository.MenuRepository, access *AccessService) *RoleService {
	return &RoleService{tx: tx, roles: roles, perms: perms, menus: menus, access: access}
}

func (s *RoleService) List(ctx context.Context, in ListRolesInput) ([]model.Role, int64, Page, error) {
	p := in.Page.Normalize()
	roles, total, err := s.roles.List(ctx, repository.RoleFilter{
		Search: strings.TrimSpace(in.Search),
		Page:   p.Page,
		Limit:  p.Limit,
	})
	return roles, total, p, err
}

func (s *RoleService) Get(ctx context.Context, id uuid.UUID) (*model.Role, error) {
	role, err := s.roles.FindByID(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, apperror.NotFound("role not found")
		}
		return nil, err
	}
	return role, nil
}

func (s *RoleService) ListPermissions(ctx context.Context) ([]model.Permission, error) {
	return s.perms.List(ctx)
}

func (s *RoleService) Create(ctx context.Context, in CreateRoleInput) (*model.Role, error) {
	code := strings.ToLower(strings.TrimSpace(in.Code))
	if !codePattern.MatchString(code) {
		return nil, apperror.BadRequest("code must start with a letter and contain only lowercase letters, digits and underscores")
	}
	taken, err := s.roles.CodeTaken(ctx, code)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, apperror.Conflict("role code is already used")
	}

	permIDs, menuIDs := dedupe(in.PermissionIDs), dedupe(in.MenuIDs)
	if err := s.checkRefs(ctx, permIDs, menuIDs); err != nil {
		return nil, err
	}

	role := &model.Role{
		Code:        code,
		Name:        strings.TrimSpace(in.Name),
		Description: strings.TrimSpace(in.Description),
	}
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.roles.Create(ctx, role); err != nil {
			return err
		}
		if err := s.roles.ReplacePermissions(ctx, role.ID, permIDs); err != nil {
			return err
		}
		return s.roles.ReplaceMenus(ctx, role.ID, menuIDs)
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperror.Conflict("role code is already used")
		}
		return nil, err
	}
	return s.Get(ctx, role.ID)
}

func (s *RoleService) Update(ctx context.Context, id uuid.UUID, in UpdateRoleInput) (*model.Role, error) {
	role, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if role.IsSystem {
		return nil, apperror.Forbidden("system roles cannot be changed")
	}

	var permIDs, menuIDs []uuid.UUID
	if in.PermissionIDs != nil {
		permIDs = dedupe(*in.PermissionIDs)
	}
	if in.MenuIDs != nil {
		menuIDs = dedupe(*in.MenuIDs)
	}
	if err := s.checkRefs(ctx, permIDs, menuIDs); err != nil {
		return nil, err
	}

	err = s.tx.Do(ctx, func(ctx context.Context) error {
		fields := map[string]any{"name": strings.TrimSpace(in.Name), "description": strings.TrimSpace(in.Description)}
		if err := s.roles.Update(ctx, id, fields); err != nil {
			return err
		}
		if in.PermissionIDs != nil {
			if err := s.roles.ReplacePermissions(ctx, id, permIDs); err != nil {
				return err
			}
		}
		if in.MenuIDs != nil {
			return s.roles.ReplaceMenus(ctx, id, menuIDs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.access.Invalidate(ctx)
	return s.Get(ctx, id)
}

// Delete refuses while users still hold the role, so nobody silently loses access.
func (s *RoleService) Delete(ctx context.Context, id uuid.UUID) error {
	role, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return apperror.Forbidden("system roles cannot be deleted")
	}
	n, err := s.roles.CountUsers(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return apperror.Conflict("role is still assigned to users")
	}
	if err := s.roles.Delete(ctx, id); err != nil {
		return err
	}
	s.access.Invalidate(ctx)
	return nil
}

func (s *RoleService) checkRefs(ctx context.Context, permIDs, menuIDs []uuid.UUID) error {
	if len(permIDs) > 0 {
		n, err := s.perms.CountByIDs(ctx, permIDs)
		if err != nil {
			return err
		}
		if int(n) != len(permIDs) {
			return apperror.BadRequest("one or more permissions do not exist")
		}
	}
	if len(menuIDs) > 0 {
		n, err := s.menus.CountByIDs(ctx, menuIDs)
		if err != nil {
			return err
		}
		if int(n) != len(menuIDs) {
			return apperror.BadRequest("one or more menus do not exist")
		}
	}
	return nil
}
