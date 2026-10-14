package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/logger"
	"github.com/yuusufyan/go-common/pkg/utils"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/utils/tests"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/middleware"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/repository"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

const testJWTSecret = "test-only-secret-with-at-least-32-chars"

var superAdminRoleID = uuid.MustParse("00000000-0000-4000-8000-000000000001")

// testIdentity is a user the fake access database knows about.
type testIdentity struct {
	superAdmin bool
	perms      []string
}

// newTestAccessService returns an AccessService whose user, role and permission lookups are
// answered in memory from identities, so middleware.Auth runs unchanged without PostgreSQL.
func newTestAccessService(t *testing.T, identities map[uuid.UUID]testIdentity) *service.AccessService {
	t.Helper()
	sqlDB := sql.OpenDB(accessConnector{identities: identities})
	t.Cleanup(func() { sqlDB.Close() })

	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{
		ConnPool:             sqlDB,
		Logger:               gormlogger.Discard,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	log := logger.NewWithConfig(logger.Config{Output: io.Discard})
	return service.NewAccessService(repository.NewUserRepository(db), repository.NewPermissionRepository(db), nil, log)
}

// newTestApp assembles a Fiber app with the production error handler.
func newTestApp() *fiber.App {
	return fiber.New(fiber.Config{ErrorHandler: utils.NewErrorHandler(logger.NewWithConfig(logger.Config{Output: io.Discard}))})
}

func issueToken(t *testing.T, userID uuid.UUID, ttl time.Duration) string {
	t.Helper()
	now := time.Now()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  userID.String(),
		"type": service.TokenTypeAccess,
		"iat":  now.Unix(),
		"exp":  now.Add(ttl).Unix(),
	}).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

func newAuth(t *testing.T, identities map[uuid.UUID]testIdentity) fiber.Handler {
	return middleware.Auth(testJWTSecret, newTestAccessService(t, identities))
}

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

// accessConnector serves the queries AccessService issues through the user and
// permission repositories; anything else is answered with no rows.
type accessConnector struct {
	identities map[uuid.UUID]testIdentity
}

func (c accessConnector) Connect(context.Context) (driver.Conn, error) { return accessConn(c), nil }
func (c accessConnector) Driver() driver.Driver                        { return accessDriver{} }

type accessDriver struct{}

func (accessDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use accessConnector")
}

type accessConn accessConnector

func (accessConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (accessConn) Close() error                        { return nil }
func (accessConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }

func (c accessConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	firstID := func() (uuid.UUID, testIdentity, bool) {
		if len(args) == 0 {
			return uuid.Nil, testIdentity{}, false
		}
		s, _ := args[0].Value.(string)
		id, err := uuid.Parse(s)
		if err != nil {
			return uuid.Nil, testIdentity{}, false
		}
		ident, ok := c.identities[id]
		return id, ident, ok
	}

	switch {
	case strings.Contains(query, "role_permissions"):
		rows := &memRows{cols: []string{"code"}}
		if _, ident, ok := firstID(); ok {
			for _, p := range ident.perms {
				rows.data = append(rows.data, []driver.Value{p})
			}
		}
		return rows, nil
	case strings.Contains(query, "`user_roles`"):
		rows := &memRows{cols: []string{"user_id", "role_id"}}
		if id, ident, ok := firstID(); ok && ident.superAdmin {
			rows.data = append(rows.data, []driver.Value{id.String(), superAdminRoleID.String()})
		}
		return rows, nil
	case strings.Contains(query, "`roles`"):
		rows := &memRows{cols: []string{"id", "code"}}
		for _, a := range args {
			if a.Value == superAdminRoleID.String() {
				rows.data = append(rows.data, []driver.Value{superAdminRoleID.String(), model.RoleSuperAdmin})
			}
		}
		return rows, nil
	case strings.Contains(query, "`permissions`"):
		rows := &memRows{cols: []string{"code"}}
		for _, p := range []string{model.PermMembershipUpdate, model.PermUserRead, model.PermVideoCreate, model.PermVideoDelete, model.PermVideoUpdate} {
			rows.data = append(rows.data, []driver.Value{p})
		}
		return rows, nil
	case strings.Contains(query, "`users`"):
		rows := &memRows{cols: []string{"id", "name", "email", "is_active"}}
		if id, _, ok := firstID(); ok {
			rows.data = append(rows.data, []driver.Value{id.String(), "Test User", "user@example.test", true})
		}
		return rows, nil
	}
	return &memRows{}, nil
}

type memRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *memRows) Columns() []string { return r.cols }
func (r *memRows) Close() error      { return nil }

func (r *memRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}
