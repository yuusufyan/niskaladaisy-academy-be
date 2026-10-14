package apitest

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

func membershipPath(id uuid.UUID) string { return "/api/v1/users/" + id.String() + "/membership" }

func countMembershipRows(t *testing.T, userID uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.Membership{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
		t.Fatalf("count membership rows: %v", err)
	}
	return n
}

// TC-API-01: admin with membership.update sets a never-configured user to premium
// without expires_at.
func TestMembershipSetPremiumWithoutExpiry(t *testing.T) {
	app := setupTest(t)
	_, admin := newAdminUser(t, testDB, model.PermMembershipUpdate)
	target := mustCreateUser(t, testDB)

	resp := doRequest(t, app, http.MethodPut, membershipPath(target), admin, `{"status":"premium"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeMembership(t, resp)
	if got.UserID != target || got.Status != model.MembershipStatusPremium || got.ExpiresAt != nil || !got.IsActive || got.UpdatedAt == nil {
		t.Fatalf("got %+v", got)
	}
	if n := countMembershipRows(t, target); n != 1 {
		t.Fatalf("membership rows for target = %d, want 1", n)
	}
}

// TC-API-02: admin sets premium with a future expires_at; a follow-up read confirms the
// value was actually persisted, not just echoed in the response.
func TestMembershipSetPremiumWithFutureExpiry(t *testing.T) {
	app := setupTest(t)
	_, admin := newAdminUser(t, testDB, model.PermMembershipUpdate)
	_, reader := newAdminUser(t, testDB, model.PermUserRead)
	target := mustCreateUser(t, testDB)
	future := time.Now().Add(180 * 24 * time.Hour).UTC().Truncate(time.Second)

	body := `{"status":"premium","expires_at":"` + future.Format(time.RFC3339) + `"}`
	resp := doRequest(t, app, http.MethodPut, membershipPath(target), admin, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeMembership(t, resp)
	if got.Status != model.MembershipStatusPremium || got.ExpiresAt == nil || !got.ExpiresAt.Equal(future) || !got.IsActive {
		t.Fatalf("PUT response = %+v", got)
	}

	readResp := doRequest(t, app, http.MethodGet, membershipPath(target), reader, "")
	if readResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", readResp.StatusCode)
	}
	readBack := decodeMembership(t, readResp)
	if readBack.Status != model.MembershipStatusPremium || readBack.ExpiresAt == nil || !readBack.ExpiresAt.Equal(future) {
		t.Fatalf("GET after PUT = %+v, want the same expires_at that was set", readBack)
	}
}

// TC-API-03: admin moves a premium (with expiry) membership back to free; expires_at must
// be cleared even though it was previously set.
func TestMembershipSetFreeClearsExpiry(t *testing.T) {
	app := setupTest(t)
	_, admin := newAdminUser(t, testDB, model.PermMembershipUpdate)
	target := mustCreateUser(t, testDB)
	future := time.Now().Add(48 * time.Hour)
	mustSetMembership(t, testDB, target, model.MembershipStatusPremium, &future)

	resp := doRequest(t, app, http.MethodPut, membershipPath(target), admin, `{"status":"free"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeMembership(t, resp)
	if got.Status != model.MembershipStatusFree || got.ExpiresAt != nil || got.IsActive {
		t.Fatalf("got %+v", got)
	}

	var row model.Membership
	if err := testDB.Where("user_id = ?", target).First(&row).Error; err != nil {
		t.Fatalf("reload membership: %v", err)
	}
	if row.ExpiresAt != nil {
		t.Fatalf("stored expires_at = %v, want cleared", row.ExpiresAt)
	}
}

// TC-API-04: expires_at in the past is rejected and leaves the membership untouched.
func TestMembershipSetRejectsPastExpiry(t *testing.T) {
	app := setupTest(t)
	_, admin := newAdminUser(t, testDB, model.PermMembershipUpdate)
	target := mustCreateUser(t, testDB)

	resp := doRequest(t, app, http.MethodPut, membershipPath(target), admin, `{"status":"premium","expires_at":"2020-01-01T00:00:00Z"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if n := countMembershipRows(t, target); n != 0 {
		t.Fatalf("membership rows = %d, want 0 (no row should have been created)", n)
	}
}

// TC-API-05: a status outside free/premium is rejected.
func TestMembershipSetRejectsUnknownStatus(t *testing.T) {
	app := setupTest(t)
	_, admin := newAdminUser(t, testDB, model.PermMembershipUpdate)
	target := mustCreateUser(t, testDB)

	resp := doRequest(t, app, http.MethodPut, membershipPath(target), admin, `{"status":"vip"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	env, _ := decodeEnvelope(t, resp)
	var fieldErrors map[string]string
	if err := json.Unmarshal(env.Data, &fieldErrors); err != nil {
		t.Fatalf("decode validation errors: %v (data: %s)", err, env.Data)
	}
	if _, ok := fieldErrors["status"]; !ok {
		t.Fatalf("validation errors = %v, want a message for field status", fieldErrors)
	}
	if n := countMembershipRows(t, target); n != 0 {
		t.Fatalf("membership rows = %d, want 0", n)
	}
}

// TC-API-06: status=free together with a non-empty expires_at is rejected.
func TestMembershipSetRejectsFreeWithExpiry(t *testing.T) {
	app := setupTest(t)
	_, admin := newAdminUser(t, testDB, model.PermMembershipUpdate)
	target := mustCreateUser(t, testDB)
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	resp := doRequest(t, app, http.MethodPut, membershipPath(target), admin, `{"status":"free","expires_at":"`+future+`"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if n := countMembershipRows(t, target); n != 0 {
		t.Fatalf("membership rows = %d, want 0", n)
	}
}

// TC-API-07: setting the membership of an id that is not a registered user returns 404 and
// creates nothing.
func TestMembershipSetUnknownUserNotFound(t *testing.T) {
	app := setupTest(t)
	_, admin := newAdminUser(t, testDB, model.PermMembershipUpdate)
	unknown := uuid.New()

	resp := doRequest(t, app, http.MethodPut, membershipPath(unknown), admin, `{"status":"premium"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if n := countMembershipRows(t, unknown); n != 0 {
		t.Fatalf("membership rows = %d, want 0", n)
	}
}

// TC-API-08: a caller without membership.update (and not superadmin) is forbidden from
// setting anyone's membership.
func TestMembershipSetForbiddenWithoutPermission(t *testing.T) {
	app := setupTest(t)
	_, plain := newPlainUser(t, testDB)
	target := mustCreateUser(t, testDB)

	resp := doRequest(t, app, http.MethodPut, membershipPath(target), plain, `{"status":"premium"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if n := countMembershipRows(t, target); n != 0 {
		t.Fatalf("membership rows = %d, want 0", n)
	}
}

// TC-API-09: requests without a valid access token are rejected before any permission or
// business logic runs.
func TestMembershipSetRequiresToken(t *testing.T) {
	app := setupTest(t)
	target := mustCreateUser(t, testDB)
	expired := issueAccessToken(t, target, -time.Minute)

	for name, token := range map[string]string{"missing": "", "expired": expired, "malformed": "not-a-jwt"} {
		t.Run(name, func(t *testing.T) {
			resp := doRequest(t, app, http.MethodPut, membershipPath(target), token, `{"status":"premium"}`)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
	if n := countMembershipRows(t, target); n != 0 {
		t.Fatalf("membership rows = %d, want 0", n)
	}
}

// TC-API-10: a user who was never configured by an admin reads as free/inactive without
// needing an explicit database row.
func TestMembershipMeDefaultsToFree(t *testing.T) {
	app := setupTest(t)
	userID, token := newPlainUser(t, testDB)

	resp := doRequest(t, app, http.MethodGet, "/api/v1/me/membership", token, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeMembership(t, resp)
	if got.UserID != userID || got.Status != model.MembershipStatusFree || got.ExpiresAt != nil || got.IsActive || got.UpdatedAt != nil {
		t.Fatalf("got %+v, want a free/inactive membership with no updated_at", got)
	}
}

// TC-API-11: a stored premium status with an expiry in the past must read as inactive,
// computed at read time rather than trusted from the stored status column.
func TestMembershipMeExpiredIsInactive(t *testing.T) {
	app := setupTest(t)
	userID, token := newPlainUser(t, testDB)
	past := time.Now().Add(-48 * time.Hour)
	mustSetMembership(t, testDB, userID, model.MembershipStatusPremium, &past)

	resp := doRequest(t, app, http.MethodGet, "/api/v1/me/membership", token, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeMembership(t, resp)
	if got.Status != model.MembershipStatusPremium {
		t.Fatalf("status = %q, want the raw stored value premium", got.Status)
	}
	if got.IsActive {
		t.Fatal("is_active = true, want false for an expired membership")
	}
}

// TC-API-12: premium with a future expiry, and premium with no expiry at all, both read as
// active.
func TestMembershipMeActiveCases(t *testing.T) {
	future := time.Now().Add(72 * time.Hour)
	tests := []struct {
		name      string
		expiresAt *time.Time
	}{
		{"future expiry", &future},
		{"no expiry", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupTest(t)
			userID, token := newPlainUser(t, testDB)
			mustSetMembership(t, testDB, userID, model.MembershipStatusPremium, tt.expiresAt)

			resp := doRequest(t, app, http.MethodGet, "/api/v1/me/membership", token, "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if got := decodeMembership(t, resp); !got.IsActive {
				t.Fatalf("got %+v, want is_active=true", got)
			}
		})
	}
}

// TC-API-13: GET /me/membership requires a valid token.
func TestMembershipMeRequiresToken(t *testing.T) {
	app := setupTest(t)
	resp := doRequest(t, app, http.MethodGet, "/api/v1/me/membership", "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TC-API-14: an admin holding user.read can view another user's membership.
func TestMembershipGetOtherUser(t *testing.T) {
	app := setupTest(t)
	_, reader := newAdminUser(t, testDB, model.PermUserRead)
	target := mustCreateUser(t, testDB)
	future := time.Now().Add(96 * time.Hour)
	mustSetMembership(t, testDB, target, model.MembershipStatusPremium, &future)

	resp := doRequest(t, app, http.MethodGet, membershipPath(target), reader, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeMembership(t, resp)
	if got.UserID != target || got.Status != model.MembershipStatusPremium || !got.IsActive {
		t.Fatalf("got %+v, want target's own premium/active membership", got)
	}
}

// TC-API-15: reading the membership of an id that is not a registered user returns 404.
func TestMembershipGetUnknownUserNotFound(t *testing.T) {
	app := setupTest(t)
	_, reader := newAdminUser(t, testDB, model.PermUserRead)

	resp := doRequest(t, app, http.MethodGet, membershipPath(uuid.New()), reader, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TC-API-16: a caller without user.read (and not superadmin) cannot read someone else's
// membership.
func TestMembershipGetForbiddenWithoutPermission(t *testing.T) {
	app := setupTest(t)
	_, plain := newPlainUser(t, testDB)
	target := mustCreateUser(t, testDB)

	resp := doRequest(t, app, http.MethodGet, membershipPath(target), plain, "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TC-API-17 (AC-40): GET /users/{id}/membership requires a valid token.
func TestMembershipGetRequiresToken(t *testing.T) {
	app := setupTest(t)
	target := mustCreateUser(t, testDB)

	resp := doRequest(t, app, http.MethodGet, membershipPath(target), "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TC-API-34 (AC-32): GET /me/membership must always answer with the caller's own identity
// from the token, regardless of user_id/id supplied in the query string or the body.
func TestMembershipMeIgnoresSpoofedIdentity(t *testing.T) {
	app := setupTest(t)
	userA, tokenA := newPlainUser(t, testDB)
	userB := mustCreateUser(t, testDB)
	future := time.Now().Add(240 * time.Hour)
	mustSetMembership(t, testDB, userB, model.MembershipStatusPremium, &future)

	paths := []string{
		"/api/v1/me/membership",
		"/api/v1/me/membership?user_id=" + userB.String() + "&id=" + userB.String(),
	}
	bodies := []string{"", `{"user_id":"` + userB.String() + `","id":"` + userB.String() + `"}`}

	for _, path := range paths {
		for _, body := range bodies {
			resp := doRequest(t, app, http.MethodGet, path, tokenA, body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s body=%q: status = %d, want 200", path, body, resp.StatusCode)
			}
			got := decodeMembership(t, resp)
			if got.UserID != userA {
				t.Fatalf("%s body=%q: user_id = %s, want caller's own id %s (never B's)", path, body, got.UserID, userA)
			}
			if got.Status != model.MembershipStatusFree || got.IsActive || got.ExpiresAt != nil {
				t.Fatalf("%s body=%q: got %+v, want A's free/inactive membership, not B's premium one", path, body, got)
			}
		}
	}
}

// TC-API-35 (AC-33): extra fields in the request body (user_id, id, created_by, updated_by,
// is_active) must never redirect the write to another user or forge the audit columns.
func TestMembershipSetIgnoresSpoofedFields(t *testing.T) {
	app := setupTest(t)
	adminID, admin := newAdminUser(t, testDB, model.PermMembershipUpdate)
	target := mustCreateUser(t, testDB)
	otherUser := mustCreateUser(t, testDB)
	spoofID := uuid.New()
	spoofCreatedBy := uuid.New()
	spoofUpdatedBy := uuid.New()

	body := `{"status":"premium","user_id":"` + otherUser.String() + `","id":"` + spoofID.String() +
		`","created_by":"` + spoofCreatedBy.String() + `","updated_by":"` + spoofUpdatedBy.String() + `","is_active":false}`
	resp := doRequest(t, app, http.MethodPut, membershipPath(target), admin, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := decodeMembership(t, resp); got.UserID != target {
		t.Fatalf("response user_id = %s, want path user %s", got.UserID, target)
	}

	if n := countMembershipRows(t, otherUser); n != 0 {
		t.Fatalf("a row was created for the spoofed user_id %s; only the path user may change", otherUser)
	}
	var row model.Membership
	if err := testDB.Where("user_id = ?", target).First(&row).Error; err != nil {
		t.Fatalf("reload membership: %v", err)
	}
	if row.ID == spoofID {
		t.Fatal("stored id matches the spoofed body id; id must be server-generated")
	}
	if row.CreatedBy != adminID.String() {
		t.Fatalf("created_by = %q, want the real caller %s (not the spoofed value)", row.CreatedBy, adminID)
	}
	if row.UpdatedBy != adminID.String() {
		t.Fatalf("updated_by = %q, want the real caller %s (not the spoofed value)", row.UpdatedBy, adminID)
	}
}

// TC-API-41 (AC-36): expires_at that is not a valid ISO 8601 timestamp is rejected.
func TestMembershipSetRejectsMalformedExpiry(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"locale formatted date", `{"status":"premium","expires_at":"31 Desember 2026"}`},
		{"not a date at all", `{"status":"premium","expires_at":"tanggal-ngasal"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupTest(t)
			_, admin := newAdminUser(t, testDB, model.PermMembershipUpdate)
			target := mustCreateUser(t, testDB)

			resp := doRequest(t, app, http.MethodPut, membershipPath(target), admin, tt.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			if n := countMembershipRows(t, target); n != 0 {
				t.Fatalf("membership rows = %d, want 0", n)
			}
		})
	}
}

// TC-API-43 (AC-38): created_by/updated_by are filled through the normal GORM save path
// (the audit plugin), tracking the actual caller of each request rather than being left
// blank or stuck on the first caller.
func TestMembershipAuditColumnsTrackActualCallers(t *testing.T) {
	app := setupTest(t)
	creatorID, creator := newAdminUser(t, testDB, model.PermMembershipUpdate)
	updaterID, updater := newAdminUser(t, testDB, model.PermMembershipUpdate)
	target := mustCreateUser(t, testDB)

	if resp := doRequest(t, app, http.MethodPut, membershipPath(target), creator, `{"status":"premium"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("first PUT status = %d, want 200", resp.StatusCode)
	}
	if resp := doRequest(t, app, http.MethodPut, membershipPath(target), updater, `{"status":"free"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("second PUT status = %d, want 200", resp.StatusCode)
	}

	var row model.Membership
	if err := testDB.Where("user_id = ?", target).First(&row).Error; err != nil {
		t.Fatalf("reload membership: %v", err)
	}
	if row.CreatedBy != creatorID.String() {
		t.Fatalf("created_by = %q, want the first caller %s", row.CreatedBy, creatorID)
	}
	if row.UpdatedBy != updaterID.String() {
		t.Fatalf("updated_by = %q, want the second caller %s", row.UpdatedBy, updaterID)
	}
}

// TC-API-45 (AC-39): a newly created membership row gets a random UUID v4 primary key, not
// a sequential integer or anything predictable.
func TestMembershipRowGetsUUIDv4(t *testing.T) {
	app := setupTest(t)
	_, admin := newAdminUser(t, testDB, model.PermMembershipUpdate)
	target := mustCreateUser(t, testDB)

	if resp := doRequest(t, app, http.MethodPut, membershipPath(target), admin, `{"status":"premium"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var row model.Membership
	if err := testDB.Where("user_id = ?", target).First(&row).Error; err != nil {
		t.Fatalf("reload membership: %v", err)
	}
	if !isUUIDv4(row.ID.String()) {
		t.Fatalf("membership id %s is not a UUID v4", row.ID)
	}
}
