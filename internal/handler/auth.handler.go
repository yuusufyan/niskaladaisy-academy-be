package handler

import (
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/response"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/middleware"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

type AuthHandler struct {
	auth  *service.AuthService
	users *service.UserService
	menus *service.MenuService
	v     *validator.Validate
}

func NewAuthHandler(auth *service.AuthService, users *service.UserService, menus *service.MenuService, v *validator.Validate) *AuthHandler {
	return &AuthHandler{auth: auth, users: users, menus: menus, v: v}
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var in service.LoginInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	res, err := h.auth.Login(c.UserContext(), in, clientMeta(c))
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Login successful", res)
}

func (h *AuthHandler) Refresh(c *fiber.Ctx) error {
	var in service.RefreshInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	tokens, err := h.auth.Refresh(c.UserContext(), in, clientMeta(c))
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Token refreshed", tokens)
}

func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	var in service.RefreshInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	if err := h.auth.Logout(c.UserContext(), in); err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Logout successful", nil)
}

type meResponse struct {
	User        *model.User   `json:"user"`
	Roles       []string      `json:"roles"`
	Permissions []string      `json:"permissions"`
	Menus       []*model.Menu `json:"menus"`
}

// Me returns everything the frontend needs to build its navigation and guard its screens.
func (h *AuthHandler) Me(c *fiber.Ctx) error {
	a := middleware.GetAccess(c)
	ctx := c.UserContext()

	user, err := h.users.Get(ctx, uuid.MustParse(a.UserID))
	if err != nil {
		return err
	}
	menus, err := h.menus.TreeFor(ctx, a)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Success", meResponse{
		User:        user,
		Roles:       a.Roles,
		Permissions: a.Permissions,
		Menus:       menus,
	})
}

func (h *AuthHandler) ChangePassword(c *fiber.Ctx) error {
	var in service.ChangePasswordInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	if err := h.auth.ChangePassword(c.UserContext(), middleware.GetAccess(c), in); err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Password changed, please log in again", nil)
}
