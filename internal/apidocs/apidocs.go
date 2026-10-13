// Package apidocs serves the OpenAPI spec and a Swagger UI for testing the API from the browser.
package apidocs

import (
	_ "embed"

	"github.com/gofiber/fiber/v2"
)

// Keep openapi.yaml in sync with handler/router.go when routes or payloads change.
//
//go:embed openapi.yaml
var spec []byte

const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Niskala Daisy Academy API</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css" crossorigin="anonymous">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js" crossorigin="anonymous"></script>
  <script>
    SwaggerUIBundle({
      url: "/docs/openapi.yaml",
      dom_id: "#swagger-ui",
      persistAuthorization: true,
      displayRequestDuration: true,
      tryItOutEnabled: true,
    });
  </script>
</body>
</html>`

// Register mounts the Swagger UI on /docs and the raw spec on /docs/openapi.yaml.
func Register(app fiber.Router) {
	app.Get("/docs", func(c *fiber.Ctx) error {
		// Helmet's require-corp policy would block the Swagger UI assets loaded from the CDN.
		c.Set("Cross-Origin-Embedder-Policy", "unsafe-none")
		c.Type("html")
		return c.SendString(page)
	})
	app.Get("/docs/openapi.yaml", func(c *fiber.Ctx) error {
		c.Type("yaml")
		return c.Send(spec)
	})
}
