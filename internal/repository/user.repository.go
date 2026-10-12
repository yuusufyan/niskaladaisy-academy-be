// Package repository holds the database access of the service.
// Every method resolves its *gorm.DB through database.GetDBFromContext so it joins a running transaction.
package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

type UserFilter struct {
	Search   string
	IsActive *bool
	Page     int
	Limit    int
}

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) List(ctx context.Context, f UserFilter) ([]model.User, int64, error) {
	q := database.GetDBFromContext(ctx, r.db).Model(&model.User{})
	if f.Search != "" {
		s := "%" + strings.ToLower(f.Search) + "%"
		q = q.Where("LOWER(name) LIKE ? OR LOWER(email) LIKE ?", s, s)
	}
	if f.IsActive != nil {
		q = q.Where("is_active = ?", *f.IsActive)
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var users []model.User
	err := q.Session(&gorm.Session{}).
		Preload("Roles").
		Order("created_at DESC").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&users).Error
	return users, total, err
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	var u model.User
	err := database.GetDBFromContext(ctx, r.db).Preload("Roles").First(&u, "id = ?", id).Error
	return &u, err
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User
	err := database.GetDBFromContext(ctx, r.db).First(&u, "LOWER(email) = LOWER(?)", email).Error
	return &u, err
}

// EmailTaken reports whether another (non-deleted) user already uses the email.
func (r *UserRepository) EmailTaken(ctx context.Context, email string, excludeID *uuid.UUID) (bool, error) {
	q := database.GetDBFromContext(ctx, r.db).Model(&model.User{}).Where("LOWER(email) = LOWER(?)", email)
	if excludeID != nil {
		q = q.Where("id <> ?", *excludeID)
	}
	var n int64
	err := q.Count(&n).Error
	return n > 0, err
}

func (r *UserRepository) Create(ctx context.Context, u *model.User) error {
	return database.GetDBFromContext(ctx, r.db).Omit("Roles").Create(u).Error
}

func (r *UserRepository) Update(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	return database.GetDBFromContext(ctx, r.db).Model(&model.User{ID: id}).Updates(fields).Error
}

func (r *UserRepository) TouchLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	return database.GetDBFromContext(ctx, r.db).Model(&model.User{ID: id}).UpdateColumn("last_login_at", at).Error
}

func (r *UserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return database.GetDBFromContext(ctx, r.db).Delete(&model.User{ID: id}).Error
}

// ReplaceRoles sets the user's roles to exactly roleIDs.
func (r *UserRepository) ReplaceRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	db := database.GetDBFromContext(ctx, r.db)
	if err := db.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
		return err
	}
	if len(roleIDs) == 0 {
		return nil
	}
	rows := make([]model.UserRole, len(roleIDs))
	for i, id := range roleIDs {
		rows[i] = model.UserRole{UserID: userID, RoleID: id}
	}
	return db.Create(&rows).Error
}
