package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

type PermissionRepository struct {
	db *gorm.DB
}

func NewPermissionRepository(db *gorm.DB) *PermissionRepository {
	return &PermissionRepository{db: db}
}

func (r *PermissionRepository) List(ctx context.Context) ([]model.Permission, error) {
	var perms []model.Permission
	err := database.GetDBFromContext(ctx, r.db).Order("module, code").Find(&perms).Error
	return perms, err
}

func (r *PermissionRepository) CountByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	var n int64
	err := database.GetDBFromContext(ctx, r.db).Model(&model.Permission{}).Where("id IN ?", ids).Count(&n).Error
	return n, err
}

// AllCodes returns every permission code.
func (r *PermissionRepository) AllCodes(ctx context.Context) ([]string, error) {
	var codes []string
	err := database.GetDBFromContext(ctx, r.db).Model(&model.Permission{}).Order("code").Pluck("code", &codes).Error
	return codes, err
}

// CodesForUser returns the distinct permission codes granted through the user's roles.
func (r *PermissionRepository) CodesForUser(ctx context.Context, userID uuid.UUID) ([]string, error) {
	var codes []string
	err := database.GetDBFromContext(ctx, r.db).Raw(`
		SELECT DISTINCT p.code
		FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		JOIN user_roles ur ON ur.role_id = rp.role_id
		JOIN roles r ON r.id = ur.role_id AND r.deleted_at IS NULL
		WHERE ur.user_id = ?
		ORDER BY p.code`, userID).Scan(&codes).Error
	return codes, err
}

// RoleCodesForUser returns the codes of the user's non-deleted roles.
func (r *PermissionRepository) RoleCodesForUser(ctx context.Context, userID uuid.UUID) ([]string, error) {
	var codes []string
	err := database.GetDBFromContext(ctx, r.db).Raw(`
		SELECT r.code
		FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = ? AND r.deleted_at IS NULL
		ORDER BY r.code`, userID).Scan(&codes).Error
	return codes, err
}
