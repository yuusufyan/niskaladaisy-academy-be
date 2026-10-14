package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

// VideoRepository stores the video catalog. Soft-deleted videos are never returned.
type VideoRepository struct {
	db *gorm.DB
}

// NewVideoRepository returns a VideoRepository backed by db.
func NewVideoRepository(db *gorm.DB) *VideoRepository {
	return &VideoRepository{db: db}
}

// List returns one page of videos, newest first, and the total count.
func (r *VideoRepository) List(ctx context.Context, page, limit int) ([]model.Video, int64, error) {
	q := database.GetDBFromContext(ctx, r.db).Model(&model.Video{})

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var videos []model.Video
	err := q.Session(&gorm.Session{}).
		Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&videos).Error
	return videos, total, err
}

// FindByID returns a video by ID.
func (r *VideoRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Video, error) {
	var v model.Video
	err := database.GetDBFromContext(ctx, r.db).First(&v, "id = ?", id).Error
	return &v, err
}

// Create inserts a video.
func (r *VideoRepository) Create(ctx context.Context, v *model.Video) error {
	return database.GetDBFromContext(ctx, r.db).Create(v).Error
}

// Update sets fields on a video.
func (r *VideoRepository) Update(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	return database.GetDBFromContext(ctx, r.db).Model(&model.Video{ID: id}).Updates(fields).Error
}

// Delete soft-deletes a video.
func (r *VideoRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return database.GetDBFromContext(ctx, r.db).Delete(&model.Video{ID: id}).Error
}
