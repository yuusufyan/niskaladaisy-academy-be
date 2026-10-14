package handler

import (
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/apperror"
	"github.com/yuusufyan/go-common/response"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/middleware"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

// MembershipHandler serves the membership routes.
type MembershipHandler struct {
	memberships *service.MembershipService
	v           *validator.Validate
}

// NewMembershipHandler returns a MembershipHandler.
func NewMembershipHandler(memberships *service.MembershipService, v *validator.Validate) *MembershipHandler {
	return &MembershipHandler{memberships: memberships, v: v}
}

// Get returns the membership of the user in the path.
func (h *MembershipHandler) Get(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	m, err := h.memberships.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Success", m)
}

// Set changes the membership of the user in the path. Only status and expires_at are read
// from the body; the user always comes from the path.
func (h *MembershipHandler) Set(c *fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var in service.SetMembershipInput
	if err := bind(c, h.v, &in); err != nil {
		return err
	}
	m, err := h.memberships.Set(c.UserContext(), id, in)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Membership updated", m)
}

// Me returns the caller's own membership. The user comes only from the token; any
// query or body is ignored.
func (h *MembershipHandler) Me(c *fiber.Ctx) error {
	id, err := uuid.Parse(middleware.GetAccess(c).UserID)
	if err != nil {
		return apperror.Unauthorized("invalid token subject")
	}
	m, err := h.memberships.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	return response.Success(c, fiber.StatusOK, "Success", m)
}
