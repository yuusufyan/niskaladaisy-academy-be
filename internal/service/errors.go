package service

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// isUniqueViolation catches the race where two requests pass the "already taken" check together.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Page holds normalized pagination input.
type Page struct {
	Page  int
	Limit int
}

// Normalize clamps page to >= 1 and limit to 1..100 (default 10).
func (p Page) Normalize() Page {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.Limit < 1 {
		p.Limit = 10
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	return p
}
