// Package model holds the GORM entities of the service.
package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
)

type User struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	IsActive     bool       `json:"is_active"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	Roles        []Role     `gorm:"many2many:user_roles" json:"roles,omitempty"`
	database.AuditModel
}

// HasRole reports whether the user holds the role with the given code.
func (u *User) HasRole(code string) bool {
	for _, r := range u.Roles {
		if r.Code == code {
			return true
		}
	}
	return false
}

type UserRole struct {
	UserID uuid.UUID `gorm:"type:uuid;primaryKey"`
	RoleID uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (UserRole) TableName() string { return "user_roles" }
