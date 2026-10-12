package handler

import (
	"github.com/gofiber/fiber/v2"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/middleware"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

type Handlers struct {
	Auth *AuthHandler
	User *UserHandler
	Role *RoleHandler
	Menu *MenuHandler
}

// RegisterUserManagement mounts the auth, user, role, permission and menu routes on api.
func RegisterUserManagement(api fiber.Router, h Handlers, auth fiber.Handler) {
	perm := middleware.RequirePermission

	a := api.Group("/auth")
	a.Post("/login", h.Auth.Login)
	a.Post("/refresh", h.Auth.Refresh)
	a.Post("/logout", h.Auth.Logout)
	a.Get("/me", auth, h.Auth.Me)
	a.Put("/me/password", auth, h.Auth.ChangePassword)

	users := api.Group("/users", auth)
	users.Get("/", perm(model.PermUserRead), h.User.List)
	users.Get("/:id", perm(model.PermUserRead), h.User.Get)
	users.Post("/", perm(model.PermUserCreate), h.User.Create)
	users.Put("/:id", perm(model.PermUserUpdate), h.User.Update)
	users.Put("/:id/password", perm(model.PermUserUpdate), h.User.ResetPassword)
	users.Delete("/:id", perm(model.PermUserDelete), h.User.Delete)

	roles := api.Group("/roles", auth)
	roles.Get("/", perm(model.PermRoleRead), h.Role.List)
	roles.Get("/:id", perm(model.PermRoleRead), h.Role.Get)
	roles.Post("/", perm(model.PermRoleCreate), h.Role.Create)
	roles.Put("/:id", perm(model.PermRoleUpdate), h.Role.Update)
	roles.Delete("/:id", perm(model.PermRoleDelete), h.Role.Delete)

	api.Get("/permissions", auth, perm(model.PermPermissionRead), h.Role.ListPermissions)

	menus := api.Group("/menus", auth)
	menus.Get("/", perm(model.PermMenuRead), h.Menu.Tree)
	menus.Get("/:id", perm(model.PermMenuRead), h.Menu.Get)
	menus.Post("/", perm(model.PermMenuCreate), h.Menu.Create)
	menus.Put("/:id", perm(model.PermMenuUpdate), h.Menu.Update)
	menus.Delete("/:id", perm(model.PermMenuDelete), h.Menu.Delete)
}
