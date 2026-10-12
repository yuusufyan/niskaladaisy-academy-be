package handler

import (
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/yuusufyan/go-common/response"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

type MenuHandler struct {
	menus *service.MenuService
	v     *validator.Validate
}

func NewMenuHandler(menus *service.MenuService, v *validator.Validate) *MenuHandler {
	return &MenuHandler{menus: menus, v: v}
}

func (h *MenuHandler) Tree(c *fiber.Ctx) error {
	tree, err := h.menus.Tree(c.UserContext())
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Success", tree)
}

func (h *MenuHandler) Get(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	m, err := h.menus.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Success", m)
}

func (h *MenuHandler) Create(c *fiber.Ctx) error {
	var in service.MenuInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	m, err := h.menus.Create(c.UserContext(), in)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusCreated, "Menu created", m)
}

func (h *MenuHandler) Update(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var in service.MenuInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	m, err := h.menus.Update(c.UserContext(), id, in)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Menu updated", m)
}

func (h *MenuHandler) Delete(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := h.menus.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Menu deleted", nil)
}
