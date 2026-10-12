package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

type RoleFilter struct {
	Search string
	Page   int
	Limit  int
}

type RoleRepository struct {
	db *gorm.DB
}

func NewRoleRepository(db *gorm.DB) *RoleRepository {
	return &RoleRepository{db: db}
}

func (r *RoleRepository) List(ctx context.Context, f RoleFilter) ([]model.Role, int64, error) {
	q := database.GetDBFromContext(ctx, r.db).Model(&model.Role{})
	if f.Search != "" {
		s := "%" + strings.ToLower(f.Search) + "%"
		q = q.Where("LOWER(name) LIKE ? OR LOWER(code) LIKE ?", s, s)
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var roles []model.Role
	err := q.Session(&gorm.Session{}).
		Order("is_system DESC, name ASC").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&roles).Error
	return roles, total, err
}

func (r *RoleRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Role, error) {
	var role model.Role
	err := database.GetDBFromContext(ctx, r.db).
		Preload("Permissions", func(db *gorm.DB) *gorm.DB { return db.Order("module, code") }).
		Preload("Menus", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order, name") }).
		First(&role, "id = ?", id).Error
	return &role, err
}

// FindByIDs returns the non-deleted roles among ids.
func (r *RoleRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]model.Role, error) {
	var roles []model.Role
	if len(ids) == 0 {
		return roles, nil
	}
	err := database.GetDBFromContext(ctx, r.db).Where("id IN ?", ids).Find(&roles).Error
	return roles, err
}

func (r *RoleRepository) FindByCode(ctx context.Context, code string) (*model.Role, error) {
	var role model.Role
	err := database.GetDBFromContext(ctx, r.db).First(&role, "code = ?", code).Error
	return &role, err
}

func (r *RoleRepository) CodeTaken(ctx context.Context, code string) (bool, error) {
	var n int64
	err := database.GetDBFromContext(ctx, r.db).Model(&model.Role{}).Where("code = ?", code).Count(&n).Error
	return n > 0, err
}

func (r *RoleRepository) Create(ctx context.Context, role *model.Role) error {
	return database.GetDBFromContext(ctx, r.db).Omit("Permissions", "Menus").Create(role).Error
}

func (r *RoleRepository) Update(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	return database.GetDBFromContext(ctx, r.db).Model(&model.Role{ID: id}).Updates(fields).Error
}

func (r *RoleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return database.GetDBFromContext(ctx, r.db).Delete(&model.Role{ID: id}).Error
}

// CountUsers counts the non-deleted users holding the role.
func (r *RoleRepository) CountUsers(ctx context.Context, roleID uuid.UUID) (int64, error) {
	var n int64
	err := database.GetDBFromContext(ctx, r.db).
		Table("user_roles").
		Joins("JOIN users ON users.id = user_roles.user_id AND users.deleted_at IS NULL").
		Where("user_roles.role_id = ?", roleID).
		Count(&n).Error
	return n, err
}

// ReplacePermissions sets the role's permissions to exactly permissionIDs.
func (r *RoleRepository) ReplacePermissions(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error {
	db := database.GetDBFromContext(ctx, r.db)
	if err := db.Where("role_id = ?", roleID).Delete(&model.RolePermission{}).Error; err != nil {
		return err
	}
	if len(permissionIDs) == 0 {
		return nil
	}
	rows := make([]model.RolePermission, len(permissionIDs))
	for i, id := range permissionIDs {
		rows[i] = model.RolePermission{RoleID: roleID, PermissionID: id}
	}
	return db.Create(&rows).Error
}

// ReplaceMenus sets the role's menus to exactly menuIDs.
func (r *RoleRepository) ReplaceMenus(ctx context.Context, roleID uuid.UUID, menuIDs []uuid.UUID) error {
	db := database.GetDBFromContext(ctx, r.db)
	if err := db.Where("role_id = ?", roleID).Delete(&model.RoleMenu{}).Error; err != nil {
		return err
	}
	if len(menuIDs) == 0 {
		return nil
	}
	rows := make([]model.RoleMenu, len(menuIDs))
	for i, id := range menuIDs {
		rows[i] = model.RoleMenu{RoleID: roleID, MenuID: id}
	}
	return db.Create(&rows).Error
}
