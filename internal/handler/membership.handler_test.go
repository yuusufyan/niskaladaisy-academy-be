package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/apperror"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

type memMemberships map[uuid.UUID]model.Membership

func (m memMemberships) FindByUserID(_ context.Context, userID uuid.UUID) (*model.Membership, error) {
	row, ok := m[userID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return &row, nil
}

func (m memMemberships) Upsert(_ context.Context, row *model.Membership) error {
	row.UpdatedAt = time.Now()
	m[row.UserID] = *row
	return nil
}

type memUsers map[uuid.UUID]bool

func (m memUsers) Get(_ context.Context, id uuid.UUID) (*model.User, error) {
	if !m[id] {
		return nil, apperror.NotFound("user not found")
	}
	return &model.User{ID: id}, nil
}

type envelope struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
	Meta struct {
		Total int64 `json:"total"`
		Page  int   `json:"page"`
		Limit int   `json:"limit"`
	} `json:"meta"`
}

func decode(t *testing.T, resp *http.Response) envelope {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return env
}

func decodeMembership(t *testing.T, resp *http.Response) service.MembershipResponse {
	t.Helper()
	var m service.MembershipResponse
	if err := json.Unmarshal(decode(t, resp).Data, &m); err != nil {
		t.Fatalf("decode membership: %v", err)
	}
	return m
}

type membershipFixture struct {
	app         *fiber.App
	memberships memMemberships
	target      uuid.UUID
	other       uuid.UUID
	admin       string // membership.update
	reader      string // user.read
	user        string // no permission
	userID      uuid.UUID
}

func newMembershipFixture(t *testing.T) *membershipFixture {
	t.Helper()
	adminID, readerID, userID := uuid.New(), uuid.New(), uuid.New()
	f := &membershipFixture{
		memberships: memMemberships{},
		target:      uuid.New(),
		other:       uuid.New(),
		userID:      userID,
	}
	users := memUsers{f.target: true, f.other: true, adminID: true, readerID: true, userID: true}
	auth := newAuth(t, map[uuid.UUID]testIdentity{
		adminID:  {perms: []string{model.PermMembershipUpdate}},
		readerID: {perms: []string{model.PermUserRead}},
		userID:   {},
	})

	h := NewMembershipHandler(service.NewMembershipService(f.memberships, users), NewValidator())
	f.app = newTestApp()
	RegisterUserManagement(f.app.Group("/api/v1"), Handlers{Membership: h}, auth)

	f.admin = issueToken(t, adminID, time.Hour)
	f.reader = issueToken(t, readerID, time.Hour)
	f.user = issueToken(t, userID, time.Hour)
	return f
}

func (f *membershipFixture) path(id uuid.UUID) string {
	return "/api/v1/users/" + id.String() + "/membership"
}

func TestMembershipRoutesRequireToken(t *testing.T) {
	f := newMembershipFixture(t)
	expired := issueToken(t, f.userID, -time.Minute)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, f.path(f.target)},
		{http.MethodGet, f.path(f.target)},
		{http.MethodGet, "/api/v1/me/membership"},
	} {
		for name, token := range map[string]string{"missing": "", "expired": expired, "malformed": "not-a-jwt"} {
			t.Run(tc.method+" "+tc.path+" "+name, func(t *testing.T) {
				resp := doRequest(t, f.app, tc.method, tc.path, token, `{"status":"premium"}`)
				if resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401", resp.StatusCode)
				}
			})
		}
	}
	if len(f.memberships) != 0 {
		t.Fatal("membership must not change")
	}
}

func TestMembershipRoutesForbidWithoutPermission(t *testing.T) {
	f := newMembershipFixture(t)

	tests := []struct {
		name, method, token string
	}{
		{"set without membership.update", http.MethodPut, f.user},
		{"set with only user.read", http.MethodPut, f.reader},
		{"get without user.read", http.MethodGet, f.user},
		{"get with only membership.update", http.MethodGet, f.admin},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doRequest(t, f.app, tt.method, f.path(f.target), tt.token, `{"status":"premium"}`)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
		})
	}
	if len(f.memberships) != 0 {
		t.Fatal("membership must not change")
	}
}

func TestMembershipSet(t *testing.T) {
	future := time.Now().Add(90 * 24 * time.Hour).UTC().Truncate(time.Second)

	t.Run("premium without expiry", func(t *testing.T) {
		f := newMembershipFixture(t)
		resp := doRequest(t, f.app, http.MethodPut, f.path(f.target), f.admin, `{"status":"premium"}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		got := decodeMembership(t, resp)
		if got.UserID != f.target || got.Status != model.MembershipStatusPremium || got.ExpiresAt != nil || !got.IsActive || got.UpdatedAt == nil {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("premium with expiry then back to free", func(t *testing.T) {
		f := newMembershipFixture(t)
		body := `{"status":"premium","expires_at":"` + future.Format(time.RFC3339) + `"}`
		got := decodeMembership(t, doRequest(t, f.app, http.MethodPut, f.path(f.target), f.admin, body))
		if got.ExpiresAt == nil || !got.ExpiresAt.Equal(future) || !got.IsActive {
			t.Fatalf("got %+v", got)
		}

		got = decodeMembership(t, doRequest(t, f.app, http.MethodPut, f.path(f.target), f.admin, `{"status":"free"}`))
		if got.Status != model.MembershipStatusFree || got.ExpiresAt != nil || got.IsActive {
			t.Fatalf("got %+v", got)
		}
		if f.memberships[f.target].ExpiresAt != nil {
			t.Fatal("stored expiry must be cleared")
		}
	})

	t.Run("extra body fields are ignored", func(t *testing.T) {
		f := newMembershipFixture(t)
		body := `{"status":"premium","user_id":"` + f.other.String() + `","id":"` + uuid.NewString() +
			`","created_by":"` + uuid.NewString() + `","updated_by":"` + uuid.NewString() + `","is_active":false}`
		resp := doRequest(t, f.app, http.MethodPut, f.path(f.target), f.admin, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if got := decodeMembership(t, resp); got.UserID != f.target || !got.IsActive {
			t.Fatalf("got %+v", got)
		}
		if _, ok := f.memberships[f.other]; ok || len(f.memberships) != 1 {
			t.Fatalf("only the path user may change, got %v", f.memberships)
		}
		saved := f.memberships[f.target]
		if saved.ID != uuid.Nil || saved.CreatedBy != "" || saved.UpdatedBy != "" {
			t.Fatalf("body must not set id or audit columns: %+v", saved)
		}
	})
}

func TestMembershipSetRejects(t *testing.T) {
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	tests := []struct {
		name     string
		target   func(f *membershipFixture) string
		body     string
		wantCode int
	}{
		{name: "unknown status", body: `{"status":"vip"}`, wantCode: http.StatusBadRequest},
		{name: "missing status", body: `{}`, wantCode: http.StatusBadRequest},
		{name: "expiry in the past", body: `{"status":"premium","expires_at":"2020-01-01T00:00:00Z"}`, wantCode: http.StatusBadRequest},
		{name: "free with expiry", body: `{"status":"free","expires_at":"` + future + `"}`, wantCode: http.StatusBadRequest},
		{name: "expiry is not a date", body: `{"status":"premium","expires_at":"tanggal-ngasal"}`, wantCode: http.StatusBadRequest},
		{name: "expiry in a local format", body: `{"status":"premium","expires_at":"31 Desember 2026"}`, wantCode: http.StatusBadRequest},
		{name: "expiry without time", body: `{"status":"premium","expires_at":"2026-12-31"}`, wantCode: http.StatusBadRequest},
		{name: "malformed json", body: `{"status":`, wantCode: http.StatusBadRequest},
		{name: "invalid user id", target: func(*membershipFixture) string { return "/api/v1/users/abc/membership" }, body: `{"status":"premium"}`, wantCode: http.StatusBadRequest},
		{name: "unknown user", target: func(f *membershipFixture) string { return f.path(uuid.New()) }, body: `{"status":"premium"}`, wantCode: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMembershipFixture(t)
			path := f.path(f.target)
			if tt.target != nil {
				path = tt.target(f)
			}
			resp := doRequest(t, f.app, http.MethodPut, path, f.admin, tt.body)
			if resp.StatusCode != tt.wantCode {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}
			if len(f.memberships) != 0 {
				t.Fatal("membership must not change")
			}
		})
	}
}

func TestMembershipGet(t *testing.T) {
	f := newMembershipFixture(t)
	f.memberships[f.target] = model.Membership{UserID: f.target, Status: model.MembershipStatusPremium}

	resp := doRequest(t, f.app, http.MethodGet, f.path(f.target), f.reader, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := decodeMembership(t, resp); got.UserID != f.target || !got.IsActive {
		t.Fatalf("got %+v", got)
	}

	if resp := doRequest(t, f.app, http.MethodGet, f.path(uuid.New()), f.reader, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown user status = %d, want 404", resp.StatusCode)
	}
	if resp := doRequest(t, f.app, http.MethodGet, "/api/v1/users/abc/membership", f.reader, ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d, want 400", resp.StatusCode)
	}
}

func TestMembershipMeUsesTokenIdentityOnly(t *testing.T) {
	f := newMembershipFixture(t)
	f.memberships[f.target] = model.Membership{UserID: f.target, Status: model.MembershipStatusPremium}

	paths := []string{
		"/api/v1/me/membership",
		"/api/v1/me/membership?user_id=" + f.target.String() + "&id=" + f.target.String(),
	}
	for _, path := range paths {
		for _, body := range []string{"", `{"user_id":"` + f.target.String() + `","id":"` + f.target.String() + `"}`} {
			resp := doRequest(t, f.app, http.MethodGet, path, f.user, body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			got := decodeMembership(t, resp)
			if got.UserID != f.userID || got.Status != model.MembershipStatusFree || got.IsActive || got.ExpiresAt != nil {
				t.Fatalf("%s: got %+v, want the caller's own free membership", path, got)
			}
		}
	}
}
