package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
)

// Membership statuses. A user without a membership row is free.
const (
	MembershipStatusFree    = "free"
	MembershipStatusPremium = "premium"
)

// Membership is the premium status an admin set for a user.
type Membership struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID    uuid.UUID  `gorm:"type:uuid" json:"user_id"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at"`
	database.AuditModel
}

// IsActive reports whether the membership grants premium access at now.
// It is computed on read so an expired membership needs no job to downgrade it.
func (m *Membership) IsActive(now time.Time) bool {
	return m.Status == MembershipStatusPremium && (m.ExpiresAt == nil || m.ExpiresAt.After(now))
}
