package handler

import (
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/yuusufyan/go-common/pkg/apperror"
	"github.com/yuusufyan/go-common/response"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/middleware"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

type UserHandler struct {
	users *service.UserService
	v     *validator.Validate
}

func NewUserHandler(users *service.UserService, v *validator.Validate) *UserHandler {
	return &UserHandler{users: users, v: v}
}

// List supports ?search=, ?is_active=true|false, ?page= and ?limit=.
func (h *UserHandler) List(c *fiber.Ctx) error {
	in := service.ListUsersInput{Search: c.Query("search"), Page: pageQuery(c)}
	if raw := c.Query("is_active"); raw != "" {
		v := c.QueryBool("is_active")
		if raw != "true" && raw != "false" {
			return apperror.BadRequest("is_active must be true or false")
		}
		in.IsActive = &v
	}
	users, total, p, err := h.users.List(c.UserContext(), in)
	if err != nil {
		return err
	}
	return response.Paginate(c, "Success", users, total, p.Page, p.Limit)
}

func (h *UserHandler) Get(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	user, err := h.users.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Success", user)
}

func (h *UserHandler) Create(c *fiber.Ctx) error {
	var in service.CreateUserInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	user, err := h.users.Create(c.UserContext(), middleware.GetAccess(c), in)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusCreated, "User created", user)
}

func (h *UserHandler) Update(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var in service.UpdateUserInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	user, err := h.users.Update(c.UserContext(), middleware.GetAccess(c), id, in)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "User updated", user)
}

func (h *UserHandler) ResetPassword(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var in service.ResetPasswordInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	if err := h.users.ResetPassword(c.UserContext(), middleware.GetAccess(c), id, in); err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Password reset", nil)
}

func (h *UserHandler) Delete(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := h.users.Delete(c.UserContext(), middleware.GetAccess(c), id); err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "User deleted", nil)
}
