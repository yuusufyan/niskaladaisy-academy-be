package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

type MenuRepository struct {
	db *gorm.DB
}

func NewMenuRepository(db *gorm.DB) *MenuRepository {
	return &MenuRepository{db: db}
}

// List returns every non-deleted menu; activeOnly drops inactive ones.
func (r *MenuRepository) List(ctx context.Context, activeOnly bool) ([]model.Menu, error) {
	q := database.GetDBFromContext(ctx, r.db).Order("sort_order, name")
	if activeOnly {
		q = q.Where("is_active = ?", true)
	}
	var menus []model.Menu
	err := q.Find(&menus).Error
	return menus, err
}

// ListForUser returns the active menus granted through the user's roles plus their ancestors,
// so the frontend always receives a connected tree.
func (r *MenuRepository) ListForUser(ctx context.Context, userID uuid.UUID) ([]model.Menu, error) {
	var menus []model.Menu
	err := database.GetDBFromContext(ctx, r.db).Raw(`
		WITH RECURSIVE allowed AS (
			SELECT m.*
			FROM menus m
			JOIN role_menus rm ON rm.menu_id = m.id
			JOIN user_roles ur ON ur.role_id = rm.role_id
			JOIN roles r ON r.id = ur.role_id AND r.deleted_at IS NULL
			WHERE ur.user_id = ? AND m.deleted_at IS NULL AND m.is_active
			UNION
			SELECT p.*
			FROM menus p
			JOIN allowed a ON a.parent_id = p.id
			WHERE p.deleted_at IS NULL AND p.is_active
		)
		SELECT * FROM allowed ORDER BY sort_order, name`, userID).Scan(&menus).Error
	return menus, err
}

func (r *MenuRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Menu, error) {
	var m model.Menu
	err := database.GetDBFromContext(ctx, r.db).First(&m, "id = ?", id).Error
	return &m, err
}

func (r *MenuRepository) CountByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	var n int64
	err := database.GetDBFromContext(ctx, r.db).Model(&model.Menu{}).Where("id IN ?", ids).Count(&n).Error
	return n, err
}

func (r *MenuRepository) CodeTaken(ctx context.Context, code string, excludeID *uuid.UUID) (bool, error) {
	q := database.GetDBFromContext(ctx, r.db).Model(&model.Menu{}).Where("code = ?", code)
	if excludeID != nil {
		q = q.Where("id <> ?", *excludeID)
	}
	var n int64
	err := q.Count(&n).Error
	return n > 0, err
}

func (r *MenuRepository) CountChildren(ctx context.Context, id uuid.UUID) (int64, error) {
	var n int64
	err := database.GetDBFromContext(ctx, r.db).Model(&model.Menu{}).Where("parent_id = ?", id).Count(&n).Error
	return n, err
}

func (r *MenuRepository) Create(ctx context.Context, m *model.Menu) error {
	return database.GetDBFromContext(ctx, r.db).Create(m).Error
}

func (r *MenuRepository) Update(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	return database.GetDBFromContext(ctx, r.db).Model(&model.Menu{ID: id}).Updates(fields).Error
}

// Delete soft-deletes the menu and removes it from every role.
func (r *MenuRepository) Delete(ctx context.Context, id uuid.UUID) error {
	db := database.GetDBFromContext(ctx, r.db)
	if err := db.Where("menu_id = ?", id).Delete(&model.RoleMenu{}).Error; err != nil {
		return err
	}
	return db.Delete(&model.Menu{ID: id}).Error
}
