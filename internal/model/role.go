package model

import (
	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
)

// RoleSuperAdmin bypasses every permission and menu check.
const RoleSuperAdmin = "superadmin"

type Role struct {
	ID          uuid.UUID    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	IsSystem    bool         `json:"is_system"`
	Permissions []Permission `gorm:"many2many:role_permissions" json:"permissions,omitempty"`
	Menus       []Menu       `gorm:"many2many:role_menus" json:"menus,omitempty"`
	database.AuditModel
}

type RolePermission struct {
	RoleID       uuid.UUID `gorm:"type:uuid;primaryKey"`
	PermissionID uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (RolePermission) TableName() string { return "role_permissions" }

type RoleMenu struct {
	RoleID uuid.UUID `gorm:"type:uuid;primaryKey"`
	MenuID uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (RoleMenu) TableName() string { return "role_menus" }
