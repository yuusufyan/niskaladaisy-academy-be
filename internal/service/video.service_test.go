package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/logger"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

const premiumURL = "https://cdn.example.test/premium/intro.mp4"

type fakeVideoStore struct {
	videos    []model.Video
	err       error
	listPage  int
	listLimit int
	created   []model.Video
	updates   []map[string]any
	deleted   []uuid.UUID
}

func (f *fakeVideoStore) List(_ context.Context, page, limit int) ([]model.Video, int64, error) {
	f.listPage, f.listLimit = page, limit
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.videos, int64(len(f.videos)), nil
}

func (f *fakeVideoStore) FindByID(_ context.Context, id uuid.UUID) (*model.Video, error) {
	if f.err != nil {
		return nil, f.err
	}
	for i := range f.videos {
		if f.videos[i].ID == id {
			v := f.videos[i]
			return &v, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeVideoStore) Create(_ context.Context, v *model.Video) error {
	v.ID = uuid.New()
	v.CreatedAt, v.UpdatedAt = time.Now(), time.Now()
	f.created = append(f.created, *v)
	f.videos = append(f.videos, *v)
	return nil
}

func (f *fakeVideoStore) Update(_ context.Context, id uuid.UUID, fields map[string]any) error {
	f.updates = append(f.updates, fields)
	for i := range f.videos {
		if f.videos[i].ID != id {
			continue
		}
		f.videos[i].Title = fields["title"].(string)
		f.videos[i].Description = fields["description"].(string)
		f.videos[i].VideoURL = fields["video_url"].(string)
		if p, ok := fields["is_premium"]; ok {
			f.videos[i].IsPremium = p.(bool)
		}
	}
	return nil
}

func (f *fakeVideoStore) Delete(_ context.Context, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeMembershipChecker struct {
	active bool
	err    error
	calls  int
}

func (f *fakeMembershipChecker) IsActive(context.Context, uuid.UUID) (bool, error) {
	f.calls++
	return f.active, f.err
}

func newVideoServiceForTest(store *fakeVideoStore, checker *fakeMembershipChecker) (*VideoService, *bytes.Buffer) {
	var buf bytes.Buffer
	log := logger.NewWithConfig(logger.Config{JSON: true, Output: &buf})
	return NewVideoService(store, checker, log), &buf
}

func callerWith(perms ...string) *Access {
	return &Access{UserID: uuid.NewString(), Permissions: perms}
}

func logEntries(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		entries = append(entries, e)
	}
	return entries
}

func TestVideoServiceGetGating(t *testing.T) {
	superAdmin := callerWith()
	superAdmin.IsSuperAdmin = true

	tests := []struct {
		name       string
		caller     *Access
		premium    bool
		member     bool
		wantLocked bool
		wantReason string
	}{
		{name: "free video for regular user", caller: callerWith(), premium: false, wantLocked: false},
		{name: "premium video for regular user", caller: callerWith(), premium: true, wantLocked: true},
		{name: "premium video for active member", caller: callerWith(), premium: true, member: true, wantLocked: false},
		{name: "premium video for superadmin", caller: superAdmin, premium: true, wantLocked: false, wantReason: "superadmin"},
		{name: "premium video for video.create holder", caller: callerWith(model.PermVideoCreate), premium: true, wantReason: model.PermVideoCreate},
		{name: "premium video for video.update holder", caller: callerWith(model.PermVideoUpdate), premium: true, wantReason: model.PermVideoUpdate},
		{name: "premium video for video.delete holder", caller: callerWith(model.PermVideoDelete), premium: true, wantReason: model.PermVideoDelete},
		{name: "premium video for membership.update holder", caller: callerWith(model.PermMembershipUpdate), premium: true, wantLocked: true},
		{name: "premium video for user.read holder", caller: callerWith(model.PermUserRead), premium: true, wantLocked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			video := model.Video{ID: uuid.New(), Title: "Intro", VideoURL: premiumURL, IsPremium: tt.premium}
			checker := &fakeMembershipChecker{active: tt.member}
			svc, logs := newVideoServiceForTest(&fakeVideoStore{videos: []model.Video{video}}, checker)

			got, err := svc.Get(context.Background(), tt.caller, video.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Locked != tt.wantLocked {
				t.Fatalf("locked = %v, want %v", got.Locked, tt.wantLocked)
			}
			wantURL := premiumURL
			if tt.wantLocked {
				wantURL = ""
			}
			if got.VideoURL != wantURL {
				t.Fatalf("video_url = %q, want %q", got.VideoURL, wantURL)
			}
			if !tt.premium && checker.calls != 0 {
				t.Fatalf("membership must not be read for a free video")
			}

			entries := logEntries(t, logs)
			if strings.Contains(logs.String(), premiumURL) {
				t.Fatalf("log must never contain the video URL: %s", logs.String())
			}
			if tt.wantReason == "" {
				if len(entries) != 0 {
					t.Fatalf("want no bypass log, got %v", entries)
				}
				return
			}
			if len(entries) != 1 {
				t.Fatalf("want 1 bypass log, got %d", len(entries))
			}
			e := entries[0]
			if e["user_id"] != tt.caller.UserID || e["video_id"] != video.ID.String() || e["bypass_reason"] != tt.wantReason {
				t.Fatalf("unexpected bypass log %v", e)
			}
		})
	}
}

func TestVideoServiceGetLockedItemOmitsURLInJSON(t *testing.T) {
	video := model.Video{ID: uuid.New(), VideoURL: premiumURL, IsPremium: true}
	svc, _ := newVideoServiceForTest(&fakeVideoStore{videos: []model.Video{video}}, &fakeMembershipChecker{})

	got, err := svc.Get(context.Background(), callerWith(), video.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "video_url") {
		t.Fatalf("locked item must not carry a video_url key: %s", raw)
	}
}

func TestVideoServiceGetErrors(t *testing.T) {
	t.Run("missing video", func(t *testing.T) {
		svc, _ := newVideoServiceForTest(&fakeVideoStore{}, &fakeMembershipChecker{})
		_, err := svc.Get(context.Background(), callerWith(), uuid.New())
		assertAppError(t, err, http.StatusNotFound)
	})

	t.Run("membership lookup fails", func(t *testing.T) {
		boom := errors.New("boom")
		video := model.Video{ID: uuid.New(), IsPremium: true}
		svc, _ := newVideoServiceForTest(&fakeVideoStore{videos: []model.Video{video}}, &fakeMembershipChecker{err: boom})
		if _, err := svc.Get(context.Background(), callerWith(), video.ID); !errors.Is(err, boom) {
			t.Fatalf("want membership error, got %v", err)
		}
	})

	t.Run("caller id is not a uuid", func(t *testing.T) {
		video := model.Video{ID: uuid.New(), IsPremium: true}
		svc, _ := newVideoServiceForTest(&fakeVideoStore{videos: []model.Video{video}}, &fakeMembershipChecker{})
		if _, err := svc.Get(context.Background(), &Access{UserID: "nope"}, video.ID); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestVideoServiceListMixedCatalog(t *testing.T) {
	videos := []model.Video{
		{ID: uuid.New(), VideoURL: "https://cdn.example.test/free-1.mp4"},
		{ID: uuid.New(), VideoURL: premiumURL, IsPremium: true},
		{ID: uuid.New(), VideoURL: "https://cdn.example.test/free-2.mp4"},
		{ID: uuid.New(), VideoURL: premiumURL, IsPremium: true},
	}
	checker := &fakeMembershipChecker{}
	svc, logs := newVideoServiceForTest(&fakeVideoStore{videos: videos}, checker)

	items, total, _, err := svc.List(context.Background(), callerWith(), Page{Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 4 || len(items) != 4 {
		t.Fatalf("got %d items, total %d", len(items), total)
	}
	for i, item := range items {
		if item.ID != videos[i].ID {
			t.Fatalf("item %d out of order", i)
		}
		if item.Locked != videos[i].IsPremium {
			t.Fatalf("item %d locked = %v", i, item.Locked)
		}
		if item.Locked && item.VideoURL != "" || !item.Locked && item.VideoURL != videos[i].VideoURL {
			t.Fatalf("item %d video_url = %q", i, item.VideoURL)
		}
	}
	if checker.calls != 1 {
		t.Fatalf("membership must be read once per request, got %d", checker.calls)
	}
	if logs.Len() != 0 {
		t.Fatalf("want no log, got %s", logs.String())
	}
}

func TestVideoServiceListBypassLogsEachPremiumVideo(t *testing.T) {
	videos := []model.Video{
		{ID: uuid.New(), VideoURL: premiumURL, IsPremium: true},
		{ID: uuid.New(), VideoURL: "https://cdn.example.test/free.mp4"},
		{ID: uuid.New(), VideoURL: premiumURL, IsPremium: true},
	}
	caller := callerWith(model.PermVideoUpdate)
	svc, logs := newVideoServiceForTest(&fakeVideoStore{videos: videos}, &fakeMembershipChecker{})

	items, _, _, err := svc.List(context.Background(), caller, Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for i, item := range items {
		if item.Locked {
			t.Fatalf("item %d must be unlocked for a video.update holder", i)
		}
	}
	entries := logEntries(t, logs)
	if len(entries) != 2 {
		t.Fatalf("want one bypass log per premium video, got %d", len(entries))
	}
	for i, e := range entries {
		want := []uuid.UUID{videos[0].ID, videos[2].ID}[i]
		if e["video_id"] != want.String() || e["bypass_reason"] != model.PermVideoUpdate || e["user_id"] != caller.UserID {
			t.Fatalf("unexpected bypass log %v", e)
		}
	}
	if strings.Contains(logs.String(), premiumURL) {
		t.Fatalf("log must never contain the video URL")
	}
}

func TestVideoServiceListPagination(t *testing.T) {
	tests := []struct {
		name      string
		in        Page
		wantPage  int
		wantLimit int
	}{
		{name: "huge limit is capped", in: Page{Page: 1, Limit: 100000}, wantPage: 1, wantLimit: 100},
		{name: "negative limit falls back to default", in: Page{Page: 1, Limit: -5}, wantPage: 1, wantLimit: 10},
		{name: "page below one", in: Page{Page: 0, Limit: 20}, wantPage: 1, wantLimit: 20},
		{name: "valid values pass through", in: Page{Page: 3, Limit: 25}, wantPage: 3, wantLimit: 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeVideoStore{}
			svc, _ := newVideoServiceForTest(store, &fakeMembershipChecker{})

			_, _, p, err := svc.List(context.Background(), callerWith(), tt.in)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if store.listPage != tt.wantPage || store.listLimit != tt.wantLimit {
				t.Fatalf("store got page=%d limit=%d", store.listPage, store.listLimit)
			}
			if p.Page != tt.wantPage || p.Limit != tt.wantLimit {
				t.Fatalf("returned page %+v", p)
			}
		})
	}
}

func TestVideoServiceListStoreFailure(t *testing.T) {
	boom := errors.New("boom")
	svc, _ := newVideoServiceForTest(&fakeVideoStore{err: boom}, &fakeMembershipChecker{})
	if _, _, _, err := svc.List(context.Background(), callerWith(), Page{}); !errors.Is(err, boom) {
		t.Fatalf("want store error, got %v", err)
	}

	video := model.Video{ID: uuid.New(), IsPremium: true}
	svc, _ = newVideoServiceForTest(&fakeVideoStore{videos: []model.Video{video}}, &fakeMembershipChecker{err: boom})
	if _, _, _, err := svc.List(context.Background(), callerWith(), Page{}); !errors.Is(err, boom) {
		t.Fatalf("want membership error, got %v", err)
	}
}

func TestVideoServiceCreate(t *testing.T) {
	yes := true
	tests := []struct {
		name        string
		in          VideoInput
		wantPremium bool
	}{
		{name: "defaults to free", in: VideoInput{Title: "  Intro  ", VideoURL: "https://cdn.example.test/a.mp4"}},
		{name: "premium", in: VideoInput{Title: "Deep dive", Description: " notes ", VideoURL: "https://cdn.example.test/b.mp4", IsPremium: &yes}, wantPremium: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeVideoStore{}
			svc, _ := newVideoServiceForTest(store, &fakeMembershipChecker{})

			got, err := svc.Create(context.Background(), tt.in)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if got.ID == uuid.Nil || got.IsPremium != tt.wantPremium || got.VideoURL != tt.in.VideoURL {
				t.Fatalf("got %+v", got)
			}
			if got.Title != strings.TrimSpace(tt.in.Title) || got.Description != strings.TrimSpace(tt.in.Description) {
				t.Fatalf("title/description not trimmed: %+v", got)
			}
		})
	}
}

func TestVideoServiceCreateRejectsBlankTitle(t *testing.T) {
	store := &fakeVideoStore{}
	svc, _ := newVideoServiceForTest(store, &fakeMembershipChecker{})

	_, err := svc.Create(context.Background(), VideoInput{Title: "   ", VideoURL: "https://cdn.example.test/a.mp4"})
	assertAppError(t, err, http.StatusBadRequest)
	if len(store.created) != 0 {
		t.Fatal("video must not be stored")
	}
}

func TestVideoServiceUpdate(t *testing.T) {
	yes := true
	tests := []struct {
		name        string
		in          VideoInput
		wantPremium bool
		wantField   bool
	}{
		{name: "omitted is_premium keeps the current value", in: VideoInput{Title: "New", VideoURL: "https://cdn.example.test/n.mp4"}, wantPremium: false},
		{name: "is_premium is changed", in: VideoInput{Title: "New", VideoURL: "https://cdn.example.test/n.mp4", IsPremium: &yes}, wantPremium: true, wantField: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			video := model.Video{ID: uuid.New(), Title: "Old", VideoURL: "https://cdn.example.test/o.mp4"}
			store := &fakeVideoStore{videos: []model.Video{video}}
			svc, _ := newVideoServiceForTest(store, &fakeMembershipChecker{})

			got, err := svc.Update(context.Background(), video.ID, tt.in)
			if err != nil {
				t.Fatalf("Update: %v", err)
			}
			if got.ID != video.ID || got.Title != "New" || got.VideoURL != tt.in.VideoURL || got.IsPremium != tt.wantPremium {
				t.Fatalf("got %+v", got)
			}
			if _, ok := store.updates[0]["is_premium"]; ok != tt.wantField {
				t.Fatalf("is_premium in update fields = %v, want %v", ok, tt.wantField)
			}
		})
	}
}

func TestVideoServiceUpdateRejects(t *testing.T) {
	video := model.Video{ID: uuid.New(), Title: "Old"}

	t.Run("missing video", func(t *testing.T) {
		store := &fakeVideoStore{}
		svc, _ := newVideoServiceForTest(store, &fakeMembershipChecker{})
		_, err := svc.Update(context.Background(), uuid.New(), VideoInput{Title: "x", VideoURL: "https://cdn.example.test/x.mp4"})
		assertAppError(t, err, http.StatusNotFound)
		if len(store.updates) != 0 {
			t.Fatal("nothing must be updated")
		}
	})

	t.Run("blank title", func(t *testing.T) {
		store := &fakeVideoStore{videos: []model.Video{video}}
		svc, _ := newVideoServiceForTest(store, &fakeMembershipChecker{})
		_, err := svc.Update(context.Background(), video.ID, VideoInput{Title: " ", VideoURL: "https://cdn.example.test/x.mp4"})
		assertAppError(t, err, http.StatusBadRequest)
		if len(store.updates) != 0 {
			t.Fatal("nothing must be updated")
		}
	})
}

func TestVideoServiceDelete(t *testing.T) {
	video := model.Video{ID: uuid.New()}
	store := &fakeVideoStore{videos: []model.Video{video}}
	svc, _ := newVideoServiceForTest(store, &fakeMembershipChecker{})

	if err := svc.Delete(context.Background(), video.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != video.ID {
		t.Fatalf("deleted %v", store.deleted)
	}

	err := svc.Delete(context.Background(), uuid.New())
	assertAppError(t, err, http.StatusNotFound)
}

func TestVideoServiceFindStoreFailure(t *testing.T) {
	boom := errors.New("boom")
	svc, _ := newVideoServiceForTest(&fakeVideoStore{err: boom}, &fakeMembershipChecker{})
	if err := svc.Delete(context.Background(), uuid.New()); !errors.Is(err, boom) {
		t.Fatalf("want store error, got %v", err)
	}
}
