package handler

import (
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/yuusufyan/go-common/response"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

type RoleHandler struct {
	roles *service.RoleService
	v     *validator.Validate
}

func NewRoleHandler(roles *service.RoleService, v *validator.Validate) *RoleHandler {
	return &RoleHandler{roles: roles, v: v}
}

// List supports ?search=, ?page= and ?limit=.
func (h *RoleHandler) List(c *fiber.Ctx) error {
	roles, total, p, err := h.roles.List(c.UserContext(), service.ListRolesInput{Search: c.Query("search"), Page: pageQuery(c)})
	if err != nil {
		return err
	}
	return response.Paginate(c, "Success", roles, total, p.Page, p.Limit)
}

func (h *RoleHandler) Get(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	role, err := h.roles.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Success", role)
}

func (h *RoleHandler) Create(c *fiber.Ctx) error {
	var in service.CreateRoleInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	role, err := h.roles.Create(c.UserContext(), in)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusCreated, "Role created", role)
}

func (h *RoleHandler) Update(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var in service.UpdateRoleInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	role, err := h.roles.Update(c.UserContext(), id, in)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Role updated", role)
}

func (h *RoleHandler) Delete(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := h.roles.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Role deleted", nil)
}

func (h *RoleHandler) ListPermissions(c *fiber.Ctx) error {
	perms, err := h.roles.ListPermissions(c.UserContext())
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Success", perms)
}
