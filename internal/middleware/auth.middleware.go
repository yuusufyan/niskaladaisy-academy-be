// Package middleware holds the authentication and authorization guards.
package middleware

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/yuusufyan/go-common/pkg/apperror"
	"github.com/yuusufyan/go-common/pkg/database"
	"github.com/yuusufyan/go-common/pkg/utils"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

const accessKey = "access"

// Auth validates the bearer access token and loads the user's current access from the database,
// so role or status changes apply on the next request instead of when the token expires.
func Auth(jwtSecret string, access *service.AccessService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
		if !ok || token == "" {
			return apperror.Unauthorized("missing bearer token")
		}
		claims, err := utils.VerifyToken(token, jwtSecret, service.TokenTypeAccess)
		if err != nil {
			return apperror.Unauthorized("invalid or expired token")
		}

		a, err := access.Get(c.UserContext(), claims["sub"].(string))
		if err != nil {
			return err
		}

		c.Locals(accessKey, a)
		// The audit plugin reads the user from the context to fill created_by/updated_by.
		c.SetUserContext(context.WithValue(c.UserContext(), database.UserContextKey, a))
		return c.Next()
	}
}

// RequirePermission allows the request when the user holds any of perms.
func RequirePermission(perms ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		a := GetAccess(c)
		if a == nil {
			return apperror.Unauthorized("unauthorized")
		}
		if !a.Can(perms...) {
			return apperror.Forbidden("you do not have permission to perform this action")
		}
		return c.Next()
	}
}

// GetAccess returns the access set by Auth, or nil on unauthenticated routes.
func GetAccess(c *fiber.Ctx) *service.Access {
	a, _ := c.Locals(accessKey).(*service.Access)
	return a
}
