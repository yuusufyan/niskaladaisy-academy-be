package main

import (
	"context"
	stdlog "log"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"github.com/yuusufyan/go-common/pkg/database"
	"github.com/yuusufyan/go-common/pkg/logger"
	commonMiddleware "github.com/yuusufyan/go-common/pkg/middleware/fiber"
	"github.com/yuusufyan/go-common/pkg/utils"
	"github.com/yuusufyan/go-common/response"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/config"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/handler"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/middleware"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/migration"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/repository"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/seed"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

func main() {
	cfg := config.Load()
	if len(cfg.JWT.Secret) < 32 {
		stdlog.Fatal("JWT_SECRET must be set to at least 32 characters")
	}

	log := logger.NewWithConfig(logger.Config{
		JSON:   cfg.IsProd(),
		Fields: logrus.Fields{"service": cfg.AppName},
	})

	// Connect registers the audit plugin unless cfg.DB.DisableAuditPlugin is set.
	db, err := database.Connect(&cfg.DB, log, cfg.IsProd())
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := migration.Up(db); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	rdb := database.InitRedis(&cfg.Redis, log)
	if rdb == nil {
		log.Fatal("Redis is required: refresh tokens are stored there")
	}

	txManager := database.NewTxManager(db)
	userRepo := repository.NewUserRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	permRepo := repository.NewPermissionRepository(db)
	menuRepo := repository.NewMenuRepository(db)
	tokenRepo := repository.NewTokenRepository(rdb)

	if err := seed.SuperAdmin(context.Background(), cfg.Seed, txManager, userRepo, roleRepo, log); err != nil {
		log.Fatalf("Failed to seed superadmin: %v", err)
	}

	accessSvc := service.NewAccessService(userRepo, permRepo, rdb, log)
	authSvc, err := service.NewAuthService(cfg.JWT, userRepo, tokenRepo, accessSvc)
	if err != nil {
		log.Fatalf("Failed to init auth service: %v", err)
	}
	userSvc := service.NewUserService(txManager, userRepo, roleRepo, tokenRepo, accessSvc)
	roleSvc := service.NewRoleService(txManager, roleRepo, permRepo, menuRepo, accessSvc)
	menuSvc := service.NewMenuService(menuRepo, accessSvc)

	validate := handler.NewValidator()
	h := handler.Handlers{
		Auth: handler.NewAuthHandler(authSvc, userSvc, menuSvc, validate),
		User: handler.NewUserHandler(userSvc, validate),
		Role: handler.NewRoleHandler(roleSvc, validate),
		Menu: handler.NewMenuHandler(menuSvc, validate),
	}

	app := fiber.New(fiber.Config{
		AppName:      cfg.AppName,
		ErrorHandler: utils.NewErrorHandler(log),
	})
	commonMiddleware.InstallCommonMiddleware(app, log)

	// Health check is registered before InternalSecret so probes can reach it.
	utils.RegisterHealthCheck(app, db, rdb)

	app.Use(commonMiddleware.InternalSecretFromEnv("dev-only-fallback-secret"))

	api := app.Group("/api/v1")
	api.Get("/ping", func(c *fiber.Ctx) error {
		return response.Success(c, fiber.StatusOK, "pong", nil)
	})
	handler.RegisterUserManagement(api, h, middleware.Auth(cfg.JWT.Secret, accessSvc))

	app.Use(utils.NotFoundHandler)

	go func() {
		if err := app.Listen(":" + cfg.AppPort); err != nil {
			log.Fatalf("Server stopped: %v", err)
		}
	}()

	shutdown := utils.NewShutdownHelper(log)
	shutdown.Wait()

	// Stop accepting requests first; Graceful runs its map in random order.
	shutdown.Graceful(map[string]func(ctx context.Context) error{
		"http": app.ShutdownWithContext,
	})
	shutdown.Graceful(map[string]func(ctx context.Context) error{
		"database": func(ctx context.Context) error {
			sqlDB, err := db.DB()
			if err != nil {
				return err
			}
			return sqlDB.Close()
		},
		"redis": func(ctx context.Context) error {
			return rdb.Close()
		},
	})
}
