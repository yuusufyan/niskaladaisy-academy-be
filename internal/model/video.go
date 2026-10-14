package model

import (
	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
)

// Video is a tutorial video in the catalog. VideoURL of a premium video is only
// shown to callers allowed to play it.
type Video struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	VideoURL    string    `json:"video_url"`
	IsPremium   bool      `json:"is_premium"`
	database.AuditModel
}
