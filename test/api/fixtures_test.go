package apitest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/logger"
	"github.com/yuusufyan/go-common/pkg/utils"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/handler"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/middleware"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/repository"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// resetDB wipes every table this feature's endpoints can read or write, keeping the
// permission catalog seeded by the migrations. Each test starts from a clean slate so
// fixtures created by one test can never leak into another.
func resetDB(t *testing.T) {
	t.Helper()
	stmt := "TRUNCATE TABLE videos, memberships, user_roles, role_permissions, role_menus, roles, users RESTART IDENTITY CASCADE"
	if err := testDB.Exec(stmt).Error; err != nil {
		t.Fatalf("reset test database: %v", err)
	}
}

// setupTest resets the database and assembles a fresh app wired to it with a logger that
// discards output. Use newAppWithLogger directly when a test needs to inspect log output.
func setupTest(t *testing.T) *fiber.App {
	t.Helper()
	resetDB(t)
	return newApp(t)
}

func newApp(t *testing.T) *fiber.App {
	t.Helper()
	return newAppWithLogger(t, logger.NewWithConfig(logger.Config{Output: io.Discard}))
}

// newAppWithLogger assembles the production router (auth + permission guards + handlers)
// against the shared test database, using log for the video service's bypass audit trail.
func newAppWithLogger(t *testing.T, log logger.Logger) *fiber.App {
	t.Helper()

	userRepo := repository.NewUserRepository(testDB)
	permRepo := repository.NewPermissionRepository(testDB)
	membershipRepo := repository.NewMembershipRepository(testDB)
	videoRepo := repository.NewVideoRepository(testDB)

	// No Redis: AccessService.cacheKey treats a nil client as "caching unavailable" and
	// reads access straight from the database every time, which is what these tests want.
	accessSvc := service.NewAccessService(userRepo, permRepo, nil, log)
	// Only MembershipService.Get/Set's 404 lookup is exercised, so the user service only
	// needs its Get method wired to a real repository.
	userSvc := service.NewUserService(nil, userRepo, nil, nil, nil)
	membershipSvc := service.NewMembershipService(membershipRepo, userSvc)
	videoSvc := service.NewVideoService(videoRepo, membershipSvc, log)

	validate := handler.NewValidator()
	membershipHandler := handler.NewMembershipHandler(membershipSvc, validate)
	videoHandler := handler.NewVideoHandler(videoSvc, validate)

	auth := middleware.Auth(jwtTestSecret, accessSvc)

	app := fiber.New(fiber.Config{
		ErrorHandler: utils.NewErrorHandler(logger.NewWithConfig(logger.Config{Output: io.Discard})),
	})
	api := app.Group("/api/v1")
	handler.RegisterUserManagement(api, handler.Handlers{Membership: membershipHandler}, auth)
	handler.RegisterVideos(api, videoHandler, auth)
	return app
}

// --- user / role / permission fixtures -------------------------------------------------

func mustCreateUser(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	u := model.User{
		Name:         "QA Fixture User",
		Email:        fmt.Sprintf("qa-%s@example.test", uuid.NewString()),
		PasswordHash: "test-fixture-hash-not-a-real-credential",
		IsActive:     true,
	}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("create fixture user: %v", err)
	}
	return u.ID
}

func mustCreateRole(t *testing.T, db *gorm.DB, code string) uuid.UUID {
	t.Helper()
	r := model.Role{Code: code, Name: code}
	if err := db.Create(&r).Error; err != nil {
		t.Fatalf("create fixture role %q: %v", code, err)
	}
	return r.ID
}

func mustGrantPermissions(t *testing.T, db *gorm.DB, roleID uuid.UUID, codes ...string) {
	t.Helper()
	for _, code := range codes {
		var perm model.Permission
		if err := db.Where("code = ?", code).First(&perm).Error; err != nil {
			t.Fatalf("look up seeded permission %q (did migrations run?): %v", code, err)
		}
		if err := db.Create(&model.RolePermission{RoleID: roleID, PermissionID: perm.ID}).Error; err != nil {
			t.Fatalf("grant permission %q: %v", code, err)
		}
	}
}

func mustAssignRole(t *testing.T, db *gorm.DB, userID, roleID uuid.UUID) {
	t.Helper()
	if err := db.Create(&model.UserRole{UserID: userID, RoleID: roleID}).Error; err != nil {
		t.Fatalf("assign role: %v", err)
	}
}

// newPlainUser returns an authenticated user with no admin permission at all.
func newPlainUser(t *testing.T, db *gorm.DB) (uuid.UUID, string) {
	t.Helper()
	id := mustCreateUser(t, db)
	return id, issueAccessToken(t, id, time.Hour)
}

// newAdminUser returns a user holding exactly perms (never superadmin), so authorization
// tests exercise the named permission and nothing else.
func newAdminUser(t *testing.T, db *gorm.DB, perms ...string) (uuid.UUID, string) {
	t.Helper()
	id := mustCreateUser(t, db)
	role := mustCreateRole(t, db, "qa-role-"+uuid.NewString())
	mustGrantPermissions(t, db, role, perms...)
	mustAssignRole(t, db, id, role)
	return id, issueAccessToken(t, id, time.Hour)
}

// newSuperAdmin returns a user holding the superadmin role, which bypasses every
// permission check via Access.Can/IsSuperAdmin.
func newSuperAdmin(t *testing.T, db *gorm.DB) (uuid.UUID, string) {
	t.Helper()
	id := mustCreateUser(t, db)
	role := mustCreateRole(t, db, model.RoleSuperAdmin)
	mustAssignRole(t, db, id, role)
	return id, issueAccessToken(t, id, time.Hour)
}

func issueAccessToken(t *testing.T, userID uuid.UUID, ttl time.Duration) string {
	t.Helper()
	now := time.Now()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  userID.String(),
		"type": service.TokenTypeAccess,
		"iat":  now.Unix(),
		"exp":  now.Add(ttl).Unix(),
	}).SignedString([]byte(jwtTestSecret))
	if err != nil {
		t.Fatalf("sign test access token: %v", err)
	}
	return tok
}

// --- membership / video fixtures --------------------------------------------------------

func mustSetMembership(t *testing.T, db *gorm.DB, userID uuid.UUID, status string, expiresAt *time.Time) {
	t.Helper()
	m := model.Membership{UserID: userID, Status: status, ExpiresAt: expiresAt}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("seed fixture membership: %v", err)
	}
}

func mustCreateVideo(t *testing.T, db *gorm.DB, title, videoURL string, premium bool) uuid.UUID {
	t.Helper()
	v := model.Video{Title: title, VideoURL: videoURL, IsPremium: premium}
	if err := db.Create(&v).Error; err != nil {
		t.Fatalf("create fixture video: %v", err)
	}
	return v.ID
}

func mustSoftDeleteVideo(t *testing.T, db *gorm.DB, id uuid.UUID) {
	t.Helper()
	if err := db.Delete(&model.Video{ID: id}).Error; err != nil {
		t.Fatalf("soft delete fixture video: %v", err)
	}
}

// --- HTTP helpers -------------------------------------------------------------------------

func doRequest(t *testing.T, app *fiber.App, method, path, token, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	}
	if token != "" {
		req.Header.Set(fiber.HeaderAuthorization, "Bearer "+token)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

type envelope struct {
	Code    int             `json:"code"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Meta    *responseMeta   `json:"meta"`
}

type responseMeta struct {
	Total        int64 `json:"total"`
	Page         int   `json:"page"`
	Limit        int   `json:"limit"`
	TotalPages   int   `json:"total_pages"`
	TotalResults int64 `json:"total_results"`
}

// decodeEnvelope reads the body once and returns both the typed envelope and the raw
// top-level fields, so callers can tell "data omitted" apart from "data is null".
func decodeEnvelope(t *testing.T, resp *http.Response) (envelope, map[string]json.RawMessage) {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v (body: %s)", err, raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode envelope fields: %v (body: %s)", err, raw)
	}
	return env, fields
}

func decodeMembership(t *testing.T, resp *http.Response) service.MembershipResponse {
	t.Helper()
	env, _ := decodeEnvelope(t, resp)
	var m service.MembershipResponse
	if err := json.Unmarshal(env.Data, &m); err != nil {
		t.Fatalf("decode membership response: %v (data: %s)", err, env.Data)
	}
	return m
}

func decodeVideoResponse(t *testing.T, resp *http.Response) service.VideoResponse {
	t.Helper()
	env, _ := decodeEnvelope(t, resp)
	var v service.VideoResponse
	if err := json.Unmarshal(env.Data, &v); err != nil {
		t.Fatalf("decode video response: %v (data: %s)", err, env.Data)
	}
	return v
}

// decodeCatalogItem decodes a catalog item as a generic map so a locked item's missing
// video_url key can be told apart from an empty string.
func decodeCatalogItem(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	env, _ := decodeEnvelope(t, resp)
	var item map[string]any
	if err := json.Unmarshal(env.Data, &item); err != nil {
		t.Fatalf("decode catalog item: %v (data: %s)", err, env.Data)
	}
	return item
}

func decodeCatalogList(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	env, _ := decodeEnvelope(t, resp)
	if len(env.Data) == 0 {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal(env.Data, &items); err != nil {
		t.Fatalf("decode catalog list: %v (data: %s)", err, env.Data)
	}
	return items
}

func isUUIDv4(s string) bool {
	return uuidV4Pattern.MatchString(strings.ToLower(s))
}
