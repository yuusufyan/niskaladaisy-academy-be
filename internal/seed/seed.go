// Package seed creates the data the service needs before anyone can log in.
package seed

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
	"github.com/yuusufyan/go-common/pkg/logger"
	"github.com/yuusufyan/go-common/pkg/utils"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/config"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/repository"
)

// SuperAdmin creates the superadmin account from SEED_ADMIN_* when that email is not registered yet.
// An existing account is left untouched, so changing the env later never resets a password.
func SuperAdmin(ctx context.Context, cfg config.SeedConfig, tx database.TxManager, users *repository.UserRepository, roles *repository.RoleRepository, log logger.Logger) error {
	email := strings.ToLower(strings.TrimSpace(cfg.AdminEmail))
	if email == "" || cfg.AdminPassword == "" {
		log.Info("SEED_ADMIN_EMAIL/SEED_ADMIN_PASSWORD not set, skipping superadmin seed")
		return nil
	}

	_, err := users.FindByEmail(ctx, email)
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	role, err := roles.FindByCode(ctx, model.RoleSuperAdmin)
	if err != nil {
		return fmt.Errorf("find superadmin role: %w", err)
	}
	hash, err := utils.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}

	user := &model.User{Name: cfg.AdminName, Email: email, PasswordHash: hash, IsActive: true}
	err = tx.Do(ctx, func(ctx context.Context) error {
		if err := users.Create(ctx, user); err != nil {
			return err
		}
		return users.ReplaceRoles(ctx, user.ID, []uuid.UUID{role.ID})
	})
	if err != nil {
		return err
	}
	log.Infof("Seeded superadmin account %s", email)
	return nil
}
