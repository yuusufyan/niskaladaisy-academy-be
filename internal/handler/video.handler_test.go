package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/logger"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
	"github.com/yuusufyan/niskaladaisy-academy-be/internal/service"
)

type memVideos struct {
	rows []model.Video
}

func (m *memVideos) List(_ context.Context, page, limit int) ([]model.Video, int64, error) {
	start := min((page-1)*limit, len(m.rows))
	end := min(start+limit, len(m.rows))
	return m.rows[start:end], int64(len(m.rows)), nil
}

func (m *memVideos) FindByID(_ context.Context, id uuid.UUID) (*model.Video, error) {
	for _, v := range m.rows {
		if v.ID == id {
			return &v, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *memVideos) Create(_ context.Context, v *model.Video) error {
	v.ID = uuid.New()
	v.CreatedAt, v.UpdatedAt = time.Now(), time.Now()
	m.rows = append(m.rows, *v)
	return nil
}

func (m *memVideos) Update(_ context.Context, id uuid.UUID, fields map[string]any) error {
	for i := range m.rows {
		if m.rows[i].ID != id {
			continue
		}
		m.rows[i].Title = fields["title"].(string)
		m.rows[i].Description = fields["description"].(string)
		m.rows[i].VideoURL = fields["video_url"].(string)
		if p, ok := fields["is_premium"]; ok {
			m.rows[i].IsPremium = p.(bool)
		}
	}
	return nil
}

func (m *memVideos) Delete(_ context.Context, id uuid.UUID) error {
	for i := range m.rows {
		if m.rows[i].ID == id {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			return nil
		}
	}
	return nil
}

type videoFixture struct {
	app     *fiber.App
	videos  *memVideos
	free    model.Video
	premium model.Video

	creator, updater, deleter, reader, user, member, super string
}

func newVideoFixture(t *testing.T) *videoFixture {
	t.Helper()
	ids := map[string]uuid.UUID{}
	for _, name := range []string{"creator", "updater", "deleter", "reader", "user", "member", "super"} {
		ids[name] = uuid.New()
	}
	auth := newAuth(t, map[uuid.UUID]testIdentity{
		ids["creator"]: {perms: []string{model.PermVideoCreate}},
		ids["updater"]: {perms: []string{model.PermVideoUpdate}},
		ids["deleter"]: {perms: []string{model.PermVideoDelete}},
		ids["reader"]:  {perms: []string{model.PermUserRead, model.PermMembershipUpdate}},
		ids["user"]:    {},
		ids["member"]:  {},
		ids["super"]:   {superAdmin: true},
	})

	f := &videoFixture{
		videos:  &memVideos{},
		free:    model.Video{ID: uuid.New(), Title: "Free", VideoURL: "https://cdn.example.test/free.mp4"},
		premium: model.Video{ID: uuid.New(), Title: "Premium", VideoURL: "https://cdn.example.test/premium.mp4", IsPremium: true},
	}
	f.videos.rows = []model.Video{f.free, f.premium}

	memberships := memMemberships{ids["member"]: {UserID: ids["member"], Status: model.MembershipStatusPremium}}
	membershipSvc := service.NewMembershipService(memberships, memUsers{})
	log := logger.NewWithConfig(logger.Config{Output: io.Discard})
	h := NewVideoHandler(service.NewVideoService(f.videos, membershipSvc, log), NewValidator())

	f.app = newTestApp()
	RegisterVideos(f.app.Group("/api/v1"), h, auth)

	tok := func(name string) string { return issueToken(t, ids[name], time.Hour) }
	f.creator, f.updater, f.deleter = tok("creator"), tok("updater"), tok("deleter")
	f.reader, f.user, f.member, f.super = tok("reader"), tok("user"), tok("member"), tok("super")
	return f
}

func videoPath(id uuid.UUID) string { return "/api/v1/videos/" + id.String() }

const validVideoBody = `{"title":"Intro","video_url":"https://cdn.example.test/intro.mp4"}`

func TestVideoRoutesRequireToken(t *testing.T) {
	f := newVideoFixture(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/videos"},
		{http.MethodGet, videoPath(f.premium.ID)},
		{http.MethodPost, "/api/v1/videos"},
		{http.MethodPut, videoPath(f.free.ID)},
		{http.MethodDelete, videoPath(f.free.ID)},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resp := doRequest(t, f.app, tc.method, tc.path, "", validVideoBody)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
	if len(f.videos.rows) != 2 || f.videos.rows[0] != f.free {
		t.Fatal("catalog must not change")
	}
}

func TestVideoManagementForbidsOtherPermissions(t *testing.T) {
	f := newVideoFixture(t)
	tests := []struct {
		name, method, path string
		tokens             []string
	}{
		{"create", http.MethodPost, "/api/v1/videos", []string{f.user, f.reader, f.updater, f.deleter}},
		{"update", http.MethodPut, videoPath(f.free.ID), []string{f.user, f.reader, f.creator, f.deleter}},
		{"delete", http.MethodDelete, videoPath(f.free.ID), []string{f.user, f.reader, f.creator, f.updater}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, token := range tt.tokens {
				resp := doRequest(t, f.app, tt.method, tt.path, token, validVideoBody)
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("status = %d, want 403", resp.StatusCode)
				}
			}
		})
	}
	if len(f.videos.rows) != 2 || f.videos.rows[0] != f.free {
		t.Fatal("catalog must not change")
	}
}

func TestVideoCreate(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		f := newVideoFixture(t)
		resp := doRequest(t, f.app, http.MethodPost, "/api/v1/videos", f.creator, validVideoBody)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var got service.VideoResponse
		if err := json.Unmarshal(decode(t, resp).Data, &got); err != nil {
			t.Fatal(err)
		}
		if got.ID == uuid.Nil || got.IsPremium || got.Description != "" || got.VideoURL != "https://cdn.example.test/intro.mp4" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("extra body fields are ignored", func(t *testing.T) {
		f := newVideoFixture(t)
		spoofID := uuid.New()
		body := `{"title":"Video Uji","video_url":"https://cdn.example.test/x.mp4","is_premium":true,"id":"` + spoofID.String() +
			`","created_by":"` + uuid.NewString() + `","updated_by":"` + uuid.NewString() + `","is_active":true}`
		resp := doRequest(t, f.app, http.MethodPost, "/api/v1/videos", f.creator, body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var got service.VideoResponse
		if err := json.Unmarshal(decode(t, resp).Data, &got); err != nil {
			t.Fatal(err)
		}
		if got.ID == spoofID || !got.IsPremium {
			t.Fatalf("got %+v", got)
		}
		saved := f.videos.rows[len(f.videos.rows)-1]
		if saved.CreatedBy != "" || saved.UpdatedBy != "" {
			t.Fatalf("body must not set audit columns: %+v", saved)
		}
	})
}

func TestVideoWriteValidation(t *testing.T) {
	tests := []struct{ name, body string }{
		{"empty title", `{"title":"","video_url":"https://cdn.example.test/a.mp4"}`},
		{"blank title", `{"title":"   ","video_url":"https://cdn.example.test/a.mp4"}`},
		{"title too long", `{"title":"` + strings.Repeat("a", 201) + `","video_url":"https://cdn.example.test/a.mp4"}`},
		{"description too long", `{"title":"a","description":"` + strings.Repeat("a", 2001) + `","video_url":"https://cdn.example.test/a.mp4"}`},
		{"missing url", `{"title":"a"}`},
		{"not a url", `{"title":"a","video_url":"bukan-url"}`},
		{"javascript scheme", `{"title":"a","video_url":"javascript:alert(1)"}`},
		{"data scheme", `{"title":"a","video_url":"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg=="}`},
		{"file scheme", `{"title":"a","video_url":"file:///etc/passwd"}`},
		{"url too long", `{"title":"a","video_url":"https://cdn.example.test/` + strings.Repeat("a", 480) + `"}`},
		{"is_premium not a boolean", `{"title":"a","video_url":"https://cdn.example.test/a.mp4","is_premium":"yes"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newVideoFixture(t)
			if resp := doRequest(t, f.app, http.MethodPost, "/api/v1/videos", f.creator, tt.body); resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("POST status = %d, want 400", resp.StatusCode)
			}
			if resp := doRequest(t, f.app, http.MethodPut, videoPath(f.free.ID), f.updater, tt.body); resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("PUT status = %d, want 400", resp.StatusCode)
			}
			if len(f.videos.rows) != 2 || f.videos.rows[0] != f.free {
				t.Fatal("catalog must not change")
			}
		})
	}
}

func TestVideoUpdateThenCatalogFollowsGating(t *testing.T) {
	f := newVideoFixture(t)
	body := `{"title":"Free now premium","video_url":"https://cdn.example.test/free.mp4","is_premium":true,"id":"` + uuid.NewString() + `"}`
	resp := doRequest(t, f.app, http.MethodPut, videoPath(f.free.ID), f.updater, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got service.VideoResponse
	if err := json.Unmarshal(decode(t, resp).Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != f.free.ID || !got.IsPremium || got.Title != "Free now premium" {
		t.Fatalf("got %+v", got)
	}

	item := getCatalogItem(t, f, f.free.ID, f.user)
	if !item["locked"].(bool) || item["video_url"] != nil {
		t.Fatalf("video turned premium must be locked for a regular user: %v", item)
	}
}

func TestVideoDelete(t *testing.T) {
	f := newVideoFixture(t)
	resp := doRequest(t, f.app, http.MethodDelete, videoPath(f.free.ID), f.deleter, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if env := decode(t, resp); string(env.Data) != "" && string(env.Data) != "null" {
		t.Fatalf("data = %s, want null", env.Data)
	}
	if resp := doRequest(t, f.app, http.MethodGet, videoPath(f.free.ID), f.user, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted video status = %d, want 404", resp.StatusCode)
	}
	for _, item := range listCatalog(t, f, "/api/v1/videos", f.user) {
		if item["id"] == f.free.ID.String() {
			t.Fatal("deleted video must not be listed")
		}
	}
}

func TestVideoUnknownID(t *testing.T) {
	f := newVideoFixture(t)
	unknown := videoPath(uuid.New())
	for _, tc := range []struct{ method, token, body string }{
		{http.MethodGet, f.user, ""},
		{http.MethodPut, f.updater, validVideoBody},
		{http.MethodDelete, f.deleter, ""},
	} {
		if resp := doRequest(t, f.app, tc.method, unknown, tc.token, tc.body); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", tc.method, resp.StatusCode)
		}
		if resp := doRequest(t, f.app, tc.method, "/api/v1/videos/abc", tc.token, tc.body); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s invalid id status = %d, want 400", tc.method, resp.StatusCode)
		}
	}
}

func TestVideoCatalogGating(t *testing.T) {
	f := newVideoFixture(t)
	tests := []struct {
		name       string
		token      string
		wantLocked bool
	}{
		{"regular user", f.user, true},
		{"holder of other admin permissions", f.reader, true},
		{"active member", f.member, false},
		{"superadmin", f.super, false},
		{"video.create holder", f.creator, false},
		{"video.update holder", f.updater, false},
		{"video.delete holder", f.deleter, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := getCatalogItem(t, f, f.premium.ID, tt.token)
			assertGated(t, item, f.premium, tt.wantLocked)

			item = getCatalogItem(t, f, f.free.ID, tt.token)
			assertGated(t, item, f.free, false)

			for _, item := range listCatalog(t, f, "/api/v1/videos", tt.token) {
				v := f.free
				if item["id"] == f.premium.ID.String() {
					v = f.premium
				}
				assertGated(t, item, v, v.IsPremium && tt.wantLocked)
			}
		})
	}
}

func TestVideoCatalogPagination(t *testing.T) {
	tests := []struct {
		query     string
		wantLimit int
		wantPage  int
	}{
		{"?limit=100000", 100, 1},
		{"?limit=-5", 10, 1},
		{"?page=0&limit=1", 1, 1},
		{"?page=9999&limit=10", 10, 9999},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			f := newVideoFixture(t)
			for i := 0; i < 120; i++ {
				f.videos.rows = append(f.videos.rows, model.Video{ID: uuid.New(), VideoURL: "https://cdn.example.test/v.mp4"})
			}
			resp := doRequest(t, f.app, http.MethodGet, "/api/v1/videos"+tt.query, f.user, "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			env := decode(t, resp)
			// The shared response envelope omits data for an empty page.
			var items []json.RawMessage
			if len(env.Data) > 0 {
				if err := json.Unmarshal(env.Data, &items); err != nil {
					t.Fatal(err)
				}
			}
			if env.Meta.Limit != tt.wantLimit || env.Meta.Page != tt.wantPage || env.Meta.Total != 122 || len(items) > tt.wantLimit {
				t.Fatalf("meta %+v with %d items", env.Meta, len(items))
			}
		})
	}
}

func getCatalogItem(t *testing.T, f *videoFixture, id uuid.UUID, token string) map[string]any {
	t.Helper()
	resp := doRequest(t, f.app, http.MethodGet, videoPath(id), token, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var item map[string]any
	if err := json.Unmarshal(decode(t, resp).Data, &item); err != nil {
		t.Fatal(err)
	}
	return item
}

func listCatalog(t *testing.T, f *videoFixture, path, token string) []map[string]any {
	t.Helper()
	resp := doRequest(t, f.app, http.MethodGet, path, token, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var items []map[string]any
	if err := json.Unmarshal(decode(t, resp).Data, &items); err != nil {
		t.Fatal(err)
	}
	return items
}

// assertGated checks a catalog item as decoded JSON, so a locked item must not carry the key at all.
func assertGated(t *testing.T, item map[string]any, v model.Video, wantLocked bool) {
	t.Helper()
	if item["locked"] != wantLocked || item["is_premium"] != v.IsPremium {
		t.Fatalf("item %v: want locked=%v", item, wantLocked)
	}
	url, hasURL := item["video_url"]
	if wantLocked && hasURL {
		t.Fatalf("locked item must not have video_url: %v", item)
	}
	if !wantLocked && url != v.VideoURL {
		t.Fatalf("unlocked item video_url = %v, want %s", url, v.VideoURL)
	}
}
