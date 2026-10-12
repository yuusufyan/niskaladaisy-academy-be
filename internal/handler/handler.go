// Package handler holds the HTTP handlers and route registration.
package handler

import (
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/apperror"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

// NewValidator reports validation errors with the JSON field names the client sent.
func NewValidator() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			return f.Name
		}
		return name
	})
	return v
}

func bind(c *fiber.Ctx, v *validator.Validate, dst any) error {
	if err := c.BodyParser(dst); err != nil {
		return apperror.BadRequest("invalid request body")
	}
	return apperror.Validate(v, dst)
}

func paramID(c *fiber.Ctx) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return uuid.Nil, apperror.BadRequest("invalid id")
	}
	return id, nil
}

func pageQuery(c *fiber.Ctx) service.Page {
	return service.Page{Page: c.QueryInt("page", 1), Limit: c.QueryInt("limit", 10)}
}

func clientMeta(c *fiber.Ctx) service.ClientMeta {
	return service.ClientMeta{UserAgent: c.Get(fiber.HeaderUserAgent), IPAddress: c.IP()}
}
