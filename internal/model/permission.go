package model

import (
	"time"

	"github.com/google/uuid"
)

// Permission codes guarded by the routes. Keep in sync with the seed migration.
const (
	PermUserRead   = "user.read"
	PermUserCreate = "user.create"
	PermUserUpdate = "user.update"
	PermUserDelete = "user.delete"

	PermRoleRead   = "role.read"
	PermRoleCreate = "role.create"
	PermRoleUpdate = "role.update"
	PermRoleDelete = "role.delete"

	PermPermissionRead = "permission.read"

	PermMenuRead   = "menu.read"
	PermMenuCreate = "menu.create"
	PermMenuUpdate = "menu.update"
	PermMenuDelete = "menu.delete"

	PermMembershipUpdate = "membership.update"

	// Holding any video permission also unlocks every premium video, for content QA.
	PermVideoCreate = "video.create"
	PermVideoUpdate = "video.update"
	PermVideoDelete = "video.delete"
)

type Permission struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Module      string    `json:"module"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}
