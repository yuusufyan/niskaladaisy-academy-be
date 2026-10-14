package apitest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yuusufyan/go-common/pkg/logger"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

func videoPath(id uuid.UUID) string { return "/api/v1/videos/" + id.String() }

func countVideoRows(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.Video{}).Count(&n).Error; err != nil {
		t.Fatalf("count video rows: %v", err)
	}
	return n
}

func loadVideo(t *testing.T, id uuid.UUID) model.Video {
	t.Helper()
	var v model.Video
	if err := testDB.Where("id = ?", id).First(&v).Error; err != nil {
		t.Fatalf("reload video %s: %v", id, err)
	}
	return v
}

// --- EP-04/EP-05: create & update ----------------------------------------------------

// TC-API-18 (AC-17): creating a video with a full, valid body stores it as premium with
// the given fields and a server-generated id.
func TestVideoCreateFullBody(t *testing.T) {
	app := setupTest(t)
	_, creator := newAdminUser(t, testDB, model.PermVideoCreate)

	body := `{"title":"Pengantar Investasi","video_url":"https://cdn.contoh.test/v1.mp4","is_premium":true}`
	resp := doRequest(t, app, http.MethodPost, "/api/v1/videos", creator, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	got := decodeVideoResponse(t, resp)
	if !got.IsPremium || got.VideoURL != "https://cdn.contoh.test/v1.mp4" || !isUUIDv4(got.ID.String()) {
		t.Fatalf("got %+v", got)
	}
}

// TC-API-19 (AC-17): omitting description and is_premium falls back to their defaults.
func TestVideoCreateDefaults(t *testing.T) {
	app := setupTest(t)
	_, creator := newAdminUser(t, testDB, model.PermVideoCreate)

	body := `{"title":"Video Gratis","video_url":"https://cdn.contoh.test/v2.mp4"}`
	resp := doRequest(t, app, http.MethodPost, "/api/v1/videos", creator, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	got := decodeVideoResponse(t, resp)
	if got.IsPremium || got.Description != "" {
		t.Fatalf("got %+v, want is_premium=false and description=\"\"", got)
	}
}

// TC-API-20 (AC-18): an empty title or a malformed video_url is rejected on both POST and
// PUT, and nothing changes in the catalog.
func TestVideoWriteRejectsBlankTitleOrBadURL(t *testing.T) {
	app := setupTest(t)
	_, creator := newAdminUser(t, testDB, model.PermVideoCreate)
	_, updater := newAdminUser(t, testDB, model.PermVideoUpdate)
	existing := mustCreateVideo(t, testDB, "Existing", "https://cdn.contoh.test/existing.mp4", false)

	tests := []struct {
		name string
		body string
	}{
		{"blank title", `{"title":"","video_url":"https://cdn.contoh.test/a.mp4"}`},
		{"malformed url", `{"title":"a","video_url":"bukan-url"}`},
	}
	for _, tt := range tests {
		t.Run("POST "+tt.name, func(t *testing.T) {
			before := countVideoRows(t)
			resp := doRequest(t, app, http.MethodPost, "/api/v1/videos", creator, tt.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			if after := countVideoRows(t); after != before {
				t.Fatalf("video count changed from %d to %d", before, after)
			}
		})
		t.Run("PUT "+tt.name, func(t *testing.T) {
			resp := doRequest(t, app, http.MethodPut, videoPath(existing), updater, tt.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			row := loadVideo(t, existing)
			if row.Title != "Existing" || row.VideoURL != "https://cdn.contoh.test/existing.mp4" {
				t.Fatalf("existing video changed: %+v", row)
			}
		})
	}
}

// TC-API-21 (AC-19): flipping a video to premium through PUT immediately changes how the
// catalog gates it for a regular, non-premium caller.
func TestVideoUpdateTogglesGatingImmediately(t *testing.T) {
	app := setupTest(t)
	_, updater := newAdminUser(t, testDB, model.PermVideoUpdate)
	_, plain := newPlainUser(t, testDB)
	video := mustCreateVideo(t, testDB, "Was Free", "https://cdn.contoh.test/free.mp4", false)

	body := `{"title":"Was Free","video_url":"https://cdn.contoh.test/free.mp4","is_premium":true}`
	resp := doRequest(t, app, http.MethodPut, videoPath(video), updater, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}
	if got := decodeVideoResponse(t, resp); !got.IsPremium {
		t.Fatalf("PUT response is_premium = %v, want true", got.IsPremium)
	}

	getResp := doRequest(t, app, http.MethodGet, videoPath(video), plain, "")
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
	item := decodeCatalogItem(t, getResp)
	if locked, _ := item["locked"].(bool); !locked {
		t.Fatalf("item = %v, want locked=true for a non-premium caller", item)
	}
	if _, hasURL := item["video_url"]; hasURL {
		t.Fatalf("item = %v, want video_url absent once locked", item)
	}
}

// TC-API-22 (AC-20): deleting a video removes it from both the list and the detail route.
func TestVideoDeleteRemovesFromCatalog(t *testing.T) {
	app := setupTest(t)
	_, deleter := newAdminUser(t, testDB, model.PermVideoDelete)
	_, plain := newPlainUser(t, testDB)
	video := mustCreateVideo(t, testDB, "To Delete", "https://cdn.contoh.test/del.mp4", false)
	survivor := mustCreateVideo(t, testDB, "Survivor", "https://cdn.contoh.test/survivor.mp4", false)

	resp := doRequest(t, app, http.MethodDelete, videoPath(video), deleter, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	env, fields := decodeEnvelope(t, resp)
	if raw, ok := fields["data"]; ok && string(raw) != "null" {
		t.Fatalf("data = %s, want null", env.Data)
	}

	listResp := doRequest(t, app, http.MethodGet, "/api/v1/videos", plain, "")
	list := decodeCatalogList(t, listResp)
	if len(list) != 1 || list[0]["id"] != survivor.String() {
		t.Fatalf("catalog list = %v, want only the surviving video %s", list, survivor)
	}
	if getResp := doRequest(t, app, http.MethodGet, videoPath(video), plain, ""); getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET deleted video status = %d, want 404", getResp.StatusCode)
	}
}

// TC-API-23 (AC-21): a caller without the matching video permission cannot create, update
// or delete videos, even when they hold an unrelated admin permission.
func TestVideoManagementForbiddenWithoutMatchingPermission(t *testing.T) {
	app := setupTest(t)
	_, irrelevant := newAdminUser(t, testDB, model.PermUserRead)
	existing := mustCreateVideo(t, testDB, "Untouched", "https://cdn.contoh.test/untouched.mp4", false)
	before := countVideoRows(t)

	tests := []struct {
		name, method, path, body string
	}{
		{"create", http.MethodPost, "/api/v1/videos", `{"title":"x","video_url":"https://cdn.contoh.test/x.mp4"}`},
		{"update", http.MethodPut, videoPath(existing), `{"title":"x","video_url":"https://cdn.contoh.test/x.mp4"}`},
		{"delete", http.MethodDelete, videoPath(existing), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doRequest(t, app, tt.method, tt.path, irrelevant, tt.body)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
		})
	}
	if after := countVideoRows(t); after != before {
		t.Fatalf("video count changed from %d to %d", before, after)
	}
	row := loadVideo(t, existing)
	if row.Title != "Untouched" {
		t.Fatalf("existing video changed: %+v", row)
	}
}

// TC-API-24 (AC-22): video management routes require a valid access token.
func TestVideoManagementRequiresToken(t *testing.T) {
	app := setupTest(t)
	existing := mustCreateVideo(t, testDB, "Untouched", "https://cdn.contoh.test/untouched.mp4", false)
	body := `{"title":"x","video_url":"https://cdn.contoh.test/x.mp4"}`

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/videos", body},
		{http.MethodPut, videoPath(existing), body},
		{http.MethodDelete, videoPath(existing), ""},
	} {
		t.Run(tc.method, func(t *testing.T) {
			resp := doRequest(t, app, tc.method, tc.path, "", tc.body)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

// --- EP-07/EP-08: catalog gating ------------------------------------------------------

// TC-API-25 (AC-23): a non-premium video is unlocked for any authenticated caller.
func TestCatalogNonPremiumVideoIsUnlocked(t *testing.T) {
	app := setupTest(t)
	_, plain := newPlainUser(t, testDB)
	video := mustCreateVideo(t, testDB, "Free", "https://cdn.contoh.test/free.mp4", false)

	item := decodeCatalogItem(t, doRequest(t, app, http.MethodGet, videoPath(video), plain, ""))
	assertUnlocked(t, item, "https://cdn.contoh.test/free.mp4")

	list := decodeCatalogList(t, doRequest(t, app, http.MethodGet, "/api/v1/videos", plain, ""))
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1", len(list))
	}
	assertUnlocked(t, list[0], "https://cdn.contoh.test/free.mp4")
}

// TC-API-26 (AC-24): a caller without active premium sees a premium video as locked, with
// video_url entirely absent from the JSON body (not just empty).
func TestCatalogPremiumVideoLockedWithoutActiveMembership(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	tests := []struct {
		name      string
		expiresAt *time.Time
		set       bool
	}{
		{"never configured", nil, false},
		{"expired premium", &past, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupTest(t)
			userID, token := newPlainUser(t, testDB)
			if tt.set {
				mustSetMembership(t, testDB, userID, model.MembershipStatusPremium, tt.expiresAt)
			}
			video := mustCreateVideo(t, testDB, "Premium", "https://cdn.contoh.test/p.mp4", true)

			resp := doRequest(t, app, http.MethodGet, videoPath(video), token, "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			assertLocked(t, decodeCatalogItem(t, resp))
		})
	}
}

// TC-API-27 (AC-25): a caller with an active premium membership (future expiry, or no
// expiry) sees the premium video unlocked.
func TestCatalogPremiumVideoUnlockedForActiveMember(t *testing.T) {
	future := time.Now().Add(240 * time.Hour)
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
			video := mustCreateVideo(t, testDB, "Premium", "https://cdn.contoh.test/p.mp4", true)

			resp := doRequest(t, app, http.MethodGet, videoPath(video), token, "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			assertUnlocked(t, decodeCatalogItem(t, resp), "https://cdn.contoh.test/p.mp4")
		})
	}
}

// TC-API-28 (AC-26): the list endpoint gates each item independently.
func TestCatalogListGatesPerItem(t *testing.T) {
	app := setupTest(t)
	_, plain := newPlainUser(t, testDB)
	free1 := mustCreateVideo(t, testDB, "Free1", "https://cdn.contoh.test/free1.mp4", false)
	free2 := mustCreateVideo(t, testDB, "Free2", "https://cdn.contoh.test/free2.mp4", false)
	premium1 := mustCreateVideo(t, testDB, "Premium1", "https://cdn.contoh.test/premium1.mp4", true)
	premium2 := mustCreateVideo(t, testDB, "Premium2", "https://cdn.contoh.test/premium2.mp4", true)

	list := decodeCatalogList(t, doRequest(t, app, http.MethodGet, "/api/v1/videos", plain, ""))
	if len(list) != 4 {
		t.Fatalf("list length = %d, want 4", len(list))
	}
	byID := map[string]map[string]any{}
	for _, item := range list {
		byID[item["id"].(string)] = item
	}
	assertUnlocked(t, byID[free1.String()], "https://cdn.contoh.test/free1.mp4")
	assertUnlocked(t, byID[free2.String()], "https://cdn.contoh.test/free2.mp4")
	assertLocked(t, byID[premium1.String()])
	assertLocked(t, byID[premium2.String()])
}

// TC-API-29 (AC-27): the catalog routes require a valid access token.
func TestCatalogRequiresToken(t *testing.T) {
	app := setupTest(t)
	video := mustCreateVideo(t, testDB, "V", "https://cdn.contoh.test/v.mp4", false)

	for _, path := range []string{"/api/v1/videos", videoPath(video)} {
		t.Run(path, func(t *testing.T) {
			resp := doRequest(t, app, http.MethodGet, path, "", "")
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

// TC-API-30 (AC-28): a video id that never existed, and one that has been soft-deleted,
// both answer 404.
func TestCatalogDetailNotFound(t *testing.T) {
	app := setupTest(t)
	_, plain := newPlainUser(t, testDB)
	deleted := mustCreateVideo(t, testDB, "Deleted", "https://cdn.contoh.test/deleted.mp4", false)
	mustSoftDeleteVideo(t, testDB, deleted)

	for name, id := range map[string]uuid.UUID{"never existed": uuid.New(), "soft-deleted": deleted} {
		t.Run(name, func(t *testing.T) {
			resp := doRequest(t, app, http.MethodGet, videoPath(id), plain, "")
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
		})
	}
}

// TC-API-31 (AC-29): a superadmin always sees premium videos unlocked, even without a
// personal active membership.
func TestCatalogSuperadminBypassesGating(t *testing.T) {
	app := setupTest(t)
	_, super := newSuperAdmin(t, testDB)
	video := mustCreateVideo(t, testDB, "Premium", "https://cdn.contoh.test/p.mp4", true)

	item := decodeCatalogItem(t, doRequest(t, app, http.MethodGet, videoPath(video), super, ""))
	assertUnlocked(t, item, "https://cdn.contoh.test/p.mp4")

	list := decodeCatalogList(t, doRequest(t, app, http.MethodGet, "/api/v1/videos", super, ""))
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1", len(list))
	}
	assertUnlocked(t, list[0], "https://cdn.contoh.test/p.mp4")
}

// TC-API-32 (AC-30): holding any of the three video management permissions also unlocks
// every premium video, regardless of personal membership status.
func TestCatalogVideoPermissionHoldersBypassGating(t *testing.T) {
	for _, perm := range []string{model.PermVideoCreate, model.PermVideoUpdate, model.PermVideoDelete} {
		t.Run(perm, func(t *testing.T) {
			app := setupTest(t)
			_, token := newAdminUser(t, testDB, perm)
			video := mustCreateVideo(t, testDB, "Premium", "https://cdn.contoh.test/p.mp4", true)

			item := decodeCatalogItem(t, doRequest(t, app, http.MethodGet, videoPath(video), token, ""))
			assertUnlocked(t, item, "https://cdn.contoh.test/p.mp4")

			list := decodeCatalogList(t, doRequest(t, app, http.MethodGet, "/api/v1/videos", token, ""))
			if len(list) != 1 {
				t.Fatalf("list length = %d, want 1", len(list))
			}
			assertUnlocked(t, list[0], "https://cdn.contoh.test/p.mp4")
		})
	}
}

// TC-API-33 (AC-31): other admin permissions (membership.update, user.read) do not bypass
// gating; their holders follow their own membership status like anyone else.
func TestCatalogOtherAdminPermissionsDoNotBypassGating(t *testing.T) {
	for _, perm := range []string{model.PermMembershipUpdate, model.PermUserRead} {
		t.Run(perm, func(t *testing.T) {
			app := setupTest(t)
			_, token := newAdminUser(t, testDB, perm)
			video := mustCreateVideo(t, testDB, "Premium", "https://cdn.contoh.test/p.mp4", true)

			item := decodeCatalogItem(t, doRequest(t, app, http.MethodGet, videoPath(video), token, ""))
			assertLocked(t, item)

			list := decodeCatalogList(t, doRequest(t, app, http.MethodGet, "/api/v1/videos", token, ""))
			if len(list) != 1 {
				t.Fatalf("list length = %d, want 1", len(list))
			}
			assertLocked(t, list[0])
		})
	}
}

// --- mass assignment, pagination, validation, audit, id enumeration -------------------

// TC-API-36 (AC-33): extra body fields (id, created_by, updated_by, is_active) must never
// control the stored id or audit columns of a video.
func TestVideoWriteIgnoresSpoofedFields(t *testing.T) {
	app := setupTest(t)
	creatorID, creator := newAdminUser(t, testDB, model.PermVideoCreate)
	updaterID, updater := newAdminUser(t, testDB, model.PermVideoUpdate)
	spoofID := uuid.New()

	createBody := `{"title":"Video Uji","video_url":"https://cdn.contoh.test/x.mp4","id":"` + spoofID.String() +
		`","created_by":"` + uuid.NewString() + `","updated_by":"` + uuid.NewString() + `","is_active":true}`
	createResp := doRequest(t, app, http.MethodPost, "/api/v1/videos", creator, createBody)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201", createResp.StatusCode)
	}
	created := decodeVideoResponse(t, createResp)
	if created.ID == spoofID {
		t.Fatal("stored id matches the spoofed body id; id must be server-generated")
	}
	row := loadVideo(t, created.ID)
	if row.CreatedBy != creatorID.String() || row.UpdatedBy != creatorID.String() {
		t.Fatalf("audit columns after create: %+v, want both set to %s", row, creatorID)
	}

	updateBody := `{"title":"Video Uji 2","video_url":"https://cdn.contoh.test/x2.mp4","id":"` + uuid.NewString() +
		`","created_by":"` + uuid.NewString() + `","updated_by":"` + uuid.NewString() + `"}`
	updateResp := doRequest(t, app, http.MethodPut, videoPath(created.ID), updater, updateBody)
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", updateResp.StatusCode)
	}
	row = loadVideo(t, created.ID)
	if row.UpdatedBy != updaterID.String() {
		t.Fatalf("updated_by after PUT = %q, want the real caller %s", row.UpdatedBy, updaterID)
	}
}

// TC-API-37 (AC-34): an oversized limit is clamped to the shared maximum of 100.
func TestCatalogPaginationClampsHighLimit(t *testing.T) {
	app := setupTest(t)
	_, plain := newPlainUser(t, testDB)
	seedVideos(t, 110)

	resp := doRequest(t, app, http.MethodGet, "/api/v1/videos?limit=100000", plain, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	env, _ := decodeEnvelope(t, resp)
	if env.Meta == nil || env.Meta.Limit != 100 {
		t.Fatalf("meta = %+v, want limit=100", env.Meta)
	}
	var items []map[string]any
	if err := json.Unmarshal(env.Data, &items); err != nil {
		t.Fatalf("decode catalog list: %v (data: %s)", err, env.Data)
	}
	if len(items) > 100 {
		t.Fatalf("returned %d items, want at most 100", len(items))
	}
}

// TC-API-38 (AC-34): a negative limit falls back to the default of 10, it is not rejected.
func TestCatalogPaginationClampsNegativeLimit(t *testing.T) {
	app := setupTest(t)
	_, plain := newPlainUser(t, testDB)
	seedVideos(t, 20)

	resp := doRequest(t, app, http.MethodGet, "/api/v1/videos?limit=-5", plain, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	env, _ := decodeEnvelope(t, resp)
	if env.Meta == nil || env.Meta.Limit != 10 {
		t.Fatalf("meta = %+v, want limit=10 (default), not a 400", env.Meta)
	}
}

// TC-API-39: a page far beyond the available data answers 200 with an empty page (the
// envelope's data key is omitted on an empty slice, per the shared response package), and
// meta still reports the real totals.
func TestCatalogPaginationOutOfRangePage(t *testing.T) {
	app := setupTest(t)
	_, plain := newPlainUser(t, testDB)
	seedVideos(t, 23)

	resp := doRequest(t, app, http.MethodGet, "/api/v1/videos?page=9999&limit=10", plain, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (not 404)", resp.StatusCode)
	}
	env, fields := decodeEnvelope(t, resp)
	if raw, ok := fields["data"]; ok && string(raw) != "null" {
		t.Fatalf("data = %s, want the key absent or null for an out-of-range page", raw)
	}
	if env.Meta == nil || env.Meta.Total != 23 || env.Meta.TotalPages != 3 {
		t.Fatalf("meta = %+v, want total=23, total_pages=3", env.Meta)
	}
}

func seedVideos(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		mustCreateVideo(t, testDB, fmt.Sprintf("Seed %d", i), fmt.Sprintf("https://cdn.contoh.test/seed-%d.mp4", i), false)
	}
}

// TC-API-40 (AC-35): a video_url using a scheme other than http/https is rejected, on both
// POST and PUT.
func TestVideoWriteRejectsDangerousURLSchemes(t *testing.T) {
	app := setupTest(t)
	_, creator := newAdminUser(t, testDB, model.PermVideoCreate)
	_, updater := newAdminUser(t, testDB, model.PermVideoUpdate)
	existing := mustCreateVideo(t, testDB, "Existing", "https://cdn.contoh.test/existing.mp4", false)

	urls := []string{
		"javascript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"file:///etc/passwd",
	}
	for _, url := range urls {
		body := `{"title":"a","video_url":"` + url + `"}`
		t.Run("POST "+url, func(t *testing.T) {
			before := countVideoRows(t)
			resp := doRequest(t, app, http.MethodPost, "/api/v1/videos", creator, body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			if after := countVideoRows(t); after != before {
				t.Fatalf("video count changed from %d to %d", before, after)
			}
		})
		t.Run("PUT "+url, func(t *testing.T) {
			resp := doRequest(t, app, http.MethodPut, videoPath(existing), updater, body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			row := loadVideo(t, existing)
			if row.VideoURL != "https://cdn.contoh.test/existing.mp4" {
				t.Fatalf("existing video url changed to %q", row.VideoURL)
			}
		})
	}
}

// TC-API-42 (AC-37): every bypass unlock of a premium video is logged with user_id,
// video_id and bypass_reason, and the log never carries the video_url itself. A member
// unlocking the same video through their own active membership leaves no such entry.
func TestVideoBypassIsLogged(t *testing.T) {
	const videoURL = "https://cdn.contoh.test/bypass-target.mp4"

	t.Run("superadmin via detail route", func(t *testing.T) {
		resetDB(t)
		var buf bytes.Buffer
		app := newAppWithLogger(t, logger.NewWithConfig(logger.Config{JSON: true, Output: &buf}))
		superID, super := newSuperAdmin(t, testDB)
		video := mustCreateVideo(t, testDB, "Premium", videoURL, true)

		if resp := doRequest(t, app, http.MethodGet, videoPath(video), super, ""); resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		entries := bypassLogEntries(t, &buf)
		if len(entries) != 1 {
			t.Fatalf("got %d bypass log entries, want exactly 1: %+v", len(entries), entries)
		}
		if entries[0].UserID != superID.String() || entries[0].VideoID != video.String() || entries[0].BypassReason != "superadmin" {
			t.Fatalf("log entry = %+v, want user_id=%s video_id=%s bypass_reason=superadmin", entries[0], superID, video)
		}
		if strings.Contains(buf.String(), videoURL) {
			t.Fatal("log output contains the video_url; it must never be logged")
		}
	})

	t.Run("video.update holder via list route", func(t *testing.T) {
		resetDB(t)
		var buf bytes.Buffer
		app := newAppWithLogger(t, logger.NewWithConfig(logger.Config{JSON: true, Output: &buf}))
		adminID, admin := newAdminUser(t, testDB, model.PermVideoUpdate)
		video := mustCreateVideo(t, testDB, "Premium", videoURL, true)

		if resp := doRequest(t, app, http.MethodGet, "/api/v1/videos", admin, ""); resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		entries := bypassLogEntries(t, &buf)
		if len(entries) != 1 {
			t.Fatalf("got %d bypass log entries, want exactly 1: %+v", len(entries), entries)
		}
		if entries[0].UserID != adminID.String() || entries[0].VideoID != video.String() || entries[0].BypassReason != model.PermVideoUpdate {
			t.Fatalf("log entry = %+v, want user_id=%s video_id=%s bypass_reason=%s", entries[0], adminID, video, model.PermVideoUpdate)
		}
		if strings.Contains(buf.String(), videoURL) {
			t.Fatal("log output contains the video_url; it must never be logged")
		}
	})

	t.Run("active member is not a bypass", func(t *testing.T) {
		resetDB(t)
		var buf bytes.Buffer
		app := newAppWithLogger(t, logger.NewWithConfig(logger.Config{JSON: true, Output: &buf}))
		memberID, member := newPlainUser(t, testDB)
		future := time.Now().Add(240 * time.Hour)
		mustSetMembership(t, testDB, memberID, model.MembershipStatusPremium, &future)
		video := mustCreateVideo(t, testDB, "Premium", videoURL, true)

		if resp := doRequest(t, app, http.MethodGet, videoPath(video), member, ""); resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if entries := bypassLogEntries(t, &buf); len(entries) != 0 {
			t.Fatalf("got %d bypass log entries, want 0 for a legitimate member: %+v", len(entries), entries)
		}
	})
}

type bypassLogEntry struct {
	Msg          string `json:"msg"`
	UserID       string `json:"user_id"`
	VideoID      string `json:"video_id"`
	BypassReason string `json:"bypass_reason"`
}

func bypassLogEntries(t *testing.T, buf *bytes.Buffer) []bypassLogEntry {
	t.Helper()
	var entries []bypassLogEntry
	sc := bufio.NewScanner(buf)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var entry bypassLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse log line %q: %v", line, err)
		}
		if entry.Msg == "premium video unlocked by bypass" {
			entries = append(entries, entry)
		}
	}
	return entries
}

// TC-API-44 (AC-38): created_by/updated_by of a video track the actual caller of the
// create and the update, which may be two different admins.
func TestVideoAuditColumnsTrackActualCallers(t *testing.T) {
	app := setupTest(t)
	creatorID, creator := newAdminUser(t, testDB, model.PermVideoCreate)
	updaterID, updater := newAdminUser(t, testDB, model.PermVideoUpdate)

	createResp := doRequest(t, app, http.MethodPost, "/api/v1/videos", creator, `{"title":"a","video_url":"https://cdn.contoh.test/a.mp4"}`)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201", createResp.StatusCode)
	}
	created := decodeVideoResponse(t, createResp)

	updateResp := doRequest(t, app, http.MethodPut, videoPath(created.ID), updater, `{"title":"b","video_url":"https://cdn.contoh.test/b.mp4"}`)
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", updateResp.StatusCode)
	}

	row := loadVideo(t, created.ID)
	if row.CreatedBy != creatorID.String() {
		t.Fatalf("created_by = %q, want the creator %s", row.CreatedBy, creatorID)
	}
	if row.UpdatedBy != updaterID.String() {
		t.Fatalf("updated_by = %q, want the updater %s", row.UpdatedBy, updaterID)
	}
	if row.CreatedBy == row.UpdatedBy {
		t.Fatal("created_by and updated_by must differ since two different admins acted")
	}
}

// TC-API-46 (AC-39): each video created gets its own random UUID v4 id, not a predictable
// or sequential one.
func TestVideoIDsAreUUIDv4AndUnique(t *testing.T) {
	app := setupTest(t)
	_, creator := newAdminUser(t, testDB, model.PermVideoCreate)

	first := decodeVideoResponse(t, doRequest(t, app, http.MethodPost, "/api/v1/videos", creator, `{"title":"a","video_url":"https://cdn.contoh.test/a.mp4"}`))
	second := decodeVideoResponse(t, doRequest(t, app, http.MethodPost, "/api/v1/videos", creator, `{"title":"b","video_url":"https://cdn.contoh.test/b.mp4"}`))

	if !isUUIDv4(first.ID.String()) || !isUUIDv4(second.ID.String()) {
		t.Fatalf("ids = %s, %s, want both to be UUID v4", first.ID, second.ID)
	}
	if first.ID == second.ID {
		t.Fatal("two created videos share the same id")
	}
}

// TC-API-47 (AC-41): PUT and DELETE on a video id that was never registered both answer
// 404 (as opposed to a video that existed and was soft-deleted, covered by TC-API-30).
func TestVideoWriteUnknownIDNotFound(t *testing.T) {
	app := setupTest(t)
	_, updater := newAdminUser(t, testDB, model.PermVideoUpdate)
	_, deleter := newAdminUser(t, testDB, model.PermVideoDelete)
	unknown := uuid.New()

	if resp := doRequest(t, app, http.MethodPut, videoPath(unknown), updater, `{"title":"a","video_url":"https://cdn.contoh.test/a.mp4"}`); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("PUT status = %d, want 404", resp.StatusCode)
	}
	if resp := doRequest(t, app, http.MethodDelete, videoPath(unknown), deleter, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE status = %d, want 404", resp.StatusCode)
	}
}

// TC-API-48: title and description must respect their documented maximum lengths (200 and
// 2000 characters respectively), even though the validator tag is not spelled out in the
// acceptance criteria text itself.
func TestVideoWriteRejectsOverlongFields(t *testing.T) {
	app := setupTest(t)
	_, creator := newAdminUser(t, testDB, model.PermVideoCreate)

	tooLongTitle := `{"title":"` + strings.Repeat("a", 201) + `","video_url":"https://cdn.contoh.test/a.mp4"}`
	tooLongDescription := `{"title":"a","description":"` + strings.Repeat("a", 2001) + `","video_url":"https://cdn.contoh.test/a.mp4"}`

	if resp := doRequest(t, app, http.MethodPost, "/api/v1/videos", creator, tooLongTitle); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("title 201 chars: status = %d, want 400", resp.StatusCode)
	}
	if resp := doRequest(t, app, http.MethodPost, "/api/v1/videos", creator, tooLongDescription); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("description 2001 chars: status = %d, want 400", resp.StatusCode)
	}
}

// --- shared gating assertions ----------------------------------------------------------

func assertUnlocked(t *testing.T, item map[string]any, wantURL string) {
	t.Helper()
	if locked, _ := item["locked"].(bool); locked {
		t.Fatalf("item = %v, want locked=false", item)
	}
	url, ok := item["video_url"]
	if !ok {
		t.Fatalf("item = %v, want video_url present when unlocked", item)
	}
	if url != wantURL {
		t.Fatalf("video_url = %v, want %s", url, wantURL)
	}
}

func assertLocked(t *testing.T, item map[string]any) {
	t.Helper()
	if locked, _ := item["locked"].(bool); !locked {
		t.Fatalf("item = %v, want locked=true", item)
	}
	if _, ok := item["video_url"]; ok {
		t.Fatalf("item = %v, want video_url absent when locked", item)
	}
}
