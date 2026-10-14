package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

// MembershipRepository stores the membership set for each user.
type MembershipRepository struct {
	db *gorm.DB
}

// NewMembershipRepository returns a MembershipRepository backed by db.
func NewMembershipRepository(db *gorm.DB) *MembershipRepository {
	return &MembershipRepository{db: db}
}

// FindByUserID returns gorm.ErrRecordNotFound when no status was ever set for the user.
func (r *MembershipRepository) FindByUserID(ctx context.Context, userID uuid.UUID) (*model.Membership, error) {
	var m model.Membership
	err := database.GetDBFromContext(ctx, r.db).First(&m, "user_id = ?", userID).Error
	return &m, err
}

// Upsert creates the user's membership or overwrites its status and expiry.
// It goes through Create so the audit plugin fills created_by/updated_by, and the
// ON CONFLICT keeps two concurrent first-time requests from failing on the unique index.
func (r *MembershipRepository) Upsert(ctx context.Context, m *model.Membership) error {
	return database.GetDBFromContext(ctx, r.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "expires_at", "updated_at", "updated_by"}),
	}).Create(m).Error
}
