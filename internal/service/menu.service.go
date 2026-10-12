package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/apperror"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/repository"
)

type MenuInput struct {
	ParentID  *uuid.UUID `json:"parent_id"`
	Code      string     `json:"code" validate:"required,max=50"`
	Name      string     `json:"name" validate:"required,max=100"`
	Path      string     `json:"path" validate:"max=255"`
	Icon      string     `json:"icon" validate:"max=100"`
	SortOrder int        `json:"sort_order"`
	IsActive  *bool      `json:"is_active"`
}

type MenuService struct {
	menus  *repository.MenuRepository
	access *AccessService
}

func NewMenuService(menus *repository.MenuRepository, access *AccessService) *MenuService {
	return &MenuService{menus: menus, access: access}
}

// Tree returns every menu, including inactive ones, for the menu management screen.
func (s *MenuService) Tree(ctx context.Context) ([]*model.Menu, error) {
	menus, err := s.menus.List(ctx, false)
	if err != nil {
		return nil, err
	}
	return model.BuildMenuTree(menus), nil
}

// TreeFor returns the active menus the user may open; superadmin sees them all.
func (s *MenuService) TreeFor(ctx context.Context, a *Access) ([]*model.Menu, error) {
	var (
		menus []model.Menu
		err   error
	)
	if a.IsSuperAdmin {
		menus, err = s.menus.List(ctx, true)
	} else {
		menus, err = s.menus.ListForUser(ctx, uuid.MustParse(a.UserID))
	}
	if err != nil {
		return nil, err
	}
	return model.BuildMenuTree(menus), nil
}

func (s *MenuService) Get(ctx context.Context, id uuid.UUID) (*model.Menu, error) {
	m, err := s.menus.FindByID(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, apperror.NotFound("menu not found")
		}
		return nil, err
	}
	return m, nil
}

func (s *MenuService) Create(ctx context.Context, in MenuInput) (*model.Menu, error) {
	code, err := s.validate(ctx, nil, in)
	if err != nil {
		return nil, err
	}
	m := &model.Menu{
		ParentID:  in.ParentID,
		Code:      code,
		Name:      strings.TrimSpace(in.Name),
		Path:      strings.TrimSpace(in.Path),
		Icon:      strings.TrimSpace(in.Icon),
		SortOrder: in.SortOrder,
		IsActive:  in.IsActive == nil || *in.IsActive,
	}
	if err := s.menus.Create(ctx, m); err != nil {
		if isUniqueViolation(err) {
			return nil, apperror.Conflict("menu code is already used")
		}
		return nil, err
	}
	s.access.Invalidate(ctx)
	return s.Get(ctx, m.ID)
}

func (s *MenuService) Update(ctx context.Context, id uuid.UUID, in MenuInput) (*model.Menu, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	code, err := s.validate(ctx, &id, in)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{
		"parent_id":  in.ParentID,
		"code":       code,
		"name":       strings.TrimSpace(in.Name),
		"path":       strings.TrimSpace(in.Path),
		"icon":       strings.TrimSpace(in.Icon),
		"sort_order": in.SortOrder,
	}
	if in.IsActive != nil {
		fields["is_active"] = *in.IsActive
	}
	if err := s.menus.Update(ctx, id, fields); err != nil {
		if isUniqueViolation(err) {
			return nil, apperror.Conflict("menu code is already used")
		}
		return nil, err
	}
	s.access.Invalidate(ctx)
	return s.Get(ctx, id)
}

func (s *MenuService) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	n, err := s.menus.CountChildren(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return apperror.Conflict("delete or move the sub-menus first")
	}
	if err := s.menus.Delete(ctx, id); err != nil {
		return err
	}
	s.access.Invalidate(ctx)
	return nil
}

// validate checks the code and that the parent exists and would not create a cycle.
func (s *MenuService) validate(ctx context.Context, id *uuid.UUID, in MenuInput) (string, error) {
	code := strings.ToLower(strings.TrimSpace(in.Code))
	if !codePattern.MatchString(code) {
		return "", apperror.BadRequest("code must start with a letter and contain only lowercase letters, digits and underscores")
	}
	taken, err := s.menus.CodeTaken(ctx, code, id)
	if err != nil {
		return "", err
	}
	if taken {
		return "", apperror.Conflict("menu code is already used")
	}

	if in.ParentID == nil {
		return code, nil
	}
	if id != nil && *in.ParentID == *id {
		return "", apperror.BadRequest("a menu cannot be its own parent")
	}

	all, err := s.menus.List(ctx, false)
	if err != nil {
		return "", err
	}
	parents := make(map[uuid.UUID]*uuid.UUID, len(all))
	for _, m := range all {
		parents[m.ID] = m.ParentID
	}
	if _, ok := parents[*in.ParentID]; !ok {
		return "", apperror.BadRequest("parent menu does not exist")
	}
	if id != nil {
		// Walk up from the new parent; reaching this menu means the parent is its descendant.
		for cur := in.ParentID; cur != nil; cur = parents[*cur] {
			if *cur == *id {
				return "", apperror.BadRequest("a menu cannot be moved under its own sub-menu")
			}
		}
	}
	return code, nil
}
