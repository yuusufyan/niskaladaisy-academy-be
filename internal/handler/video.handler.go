package handler

import (
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/yuusufyan/go-common/response"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/middleware"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

// VideoHandler serves the video catalog routes.
type VideoHandler struct {
	videos *service.VideoService
	v      *validator.Validate
}

// NewVideoHandler returns a VideoHandler.
func NewVideoHandler(videos *service.VideoService, v *validator.Validate) *VideoHandler {
	return &VideoHandler{videos: videos, v: v}
}

// List supports ?page= and ?limit= (max 100).
func (h *VideoHandler) List(c *fiber.Ctx) error {
	items, total, p, err := h.videos.List(c.UserContext(), middleware.GetAccess(c), pageQuery(c))
	if err != nil {
		return err
	}
	return response.Paginate(c, "Success", items, total, p.Page, p.Limit)
}

// Get returns one catalog video.
func (h *VideoHandler) Get(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	item, err := h.videos.Get(c.UserContext(), middleware.GetAccess(c), id)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Success", item)
}

// Create adds a video.
func (h *VideoHandler) Create(c *fiber.Ctx) error {
	var in service.VideoInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	v, err := h.videos.Create(c.UserContext(), in)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusCreated, "Video created", v)
}

// Update changes a video.
func (h *VideoHandler) Update(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var in service.VideoInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	v, err := h.videos.Update(c.UserContext(), id, in)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Video updated", v)
}

// Delete removes a video from the catalog.
func (h *VideoHandler) Delete(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := h.videos.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Video deleted", nil)
}
