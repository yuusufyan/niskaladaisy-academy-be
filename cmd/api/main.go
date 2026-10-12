package main

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"github.com/yuusufyan/go-common/pkg/database"
	"github.com/yuusufyan/go-common/pkg/logger"
	commonMiddleware "github.com/yuusufyan/go-common/pkg/middleware/fiber"
	"github.com/yuusufyan/go-common/pkg/utils"
	"github.com/yuusufyan/go-common/response"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/config"
)

func main() {
	cfg := config.Load()

	log := logger.NewWithConfig(logger.Config{
		JSON:   cfg.IsProd(),
		Fields: logrus.Fields{"service": cfg.AppName},
	})

	db, err := database.Connect(&cfg.DB, log, cfg.IsProd())
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	if err := db.Use(database.NewAuditPlugin()); err != nil {
		log.Fatalf("Failed to register audit plugin: %v", err)
	}

	rdb := database.InitRedis(&cfg.Redis, log) // nil when Redis is unavailable

	app := fiber.New(fiber.Config{
		AppName:      cfg.AppName,
		ErrorHandler: utils.NewErrorHandler(log),
	})
	commonMiddleware.InstallCommonMiddleware(app, log)

	// Health check is registered before InternalSecret so probes can reach it.
	utils.RegisterHealthCheck(app, db, rdb)

	app.Use(commonMiddleware.InternalSecretFromEnv("dev-only-fallback-secret"))
	app.Use(commonMiddleware.Identity())

	api := app.Group("/api/v1")
	api.Get("/ping", func(c *fiber.Ctx) error {
		return response.Success(c, fiber.StatusOK, "pong", nil)
	})

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
			if rdb == nil {
				return nil
			}
			return rdb.Close()
		},
	})
}
