package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/apperror"
	"gorm.io/gorm"

	"github.com/yuusufyan/niskaladaisy-academy-be/internal/model"
)

type fakeMembershipStore struct {
	rows      map[uuid.UUID]*model.Membership
	findErr   error
	upsertErr error
	upserts   []model.Membership
}

func newFakeMembershipStore() *fakeMembershipStore {
	return &fakeMembershipStore{rows: map[uuid.UUID]*model.Membership{}}
}

func (f *fakeMembershipStore) FindByUserID(_ context.Context, userID uuid.UUID) (*model.Membership, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	m, ok := f.rows[userID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *m
	return &cp, nil
}

func (f *fakeMembershipStore) Upsert(_ context.Context, m *model.Membership) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	m.UpdatedAt = time.Now()
	f.upserts = append(f.upserts, *m)
	cp := *m
	f.rows[m.UserID] = &cp
	return nil
}

type fakeUsers map[uuid.UUID]bool

func (f fakeUsers) Get(_ context.Context, id uuid.UUID) (*model.User, error) {
	if !f[id] {
		return nil, apperror.NotFound("user not found")
	}
	return &model.User{ID: id}, nil
}

func ptrTime(t time.Time) *time.Time { return &t }

func assertAppError(t *testing.T, err error, code int) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("want AppError %d, got %v", code, err)
	}
	if appErr.Code != code {
		t.Fatalf("want status %d, got %d (%s)", code, appErr.Code, appErr.Message)
	}
}

func TestMembershipServiceGet(t *testing.T) {
	userID := uuid.New()
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)

	tests := []struct {
		name       string
		row        *model.Membership
		wantStatus string
		wantActive bool
		wantExpiry bool
	}{
		{name: "never set is free", wantStatus: model.MembershipStatusFree},
		{name: "free", row: &model.Membership{Status: model.MembershipStatusFree}, wantStatus: model.MembershipStatusFree},
		{name: "premium without expiry", row: &model.Membership{Status: model.MembershipStatusPremium}, wantStatus: model.MembershipStatusPremium, wantActive: true},
		{name: "premium expiring later", row: &model.Membership{Status: model.MembershipStatusPremium, ExpiresAt: &future}, wantStatus: model.MembershipStatusPremium, wantActive: true, wantExpiry: true},
		{name: "premium already expired", row: &model.Membership{Status: model.MembershipStatusPremium, ExpiresAt: &past}, wantStatus: model.MembershipStatusPremium, wantExpiry: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeMembershipStore()
			if tt.row != nil {
				tt.row.UserID = userID
				store.rows[userID] = tt.row
			}
			svc := NewMembershipService(store, fakeUsers{userID: true})

			got, err := svc.Get(context.Background(), userID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.UserID != userID || got.Status != tt.wantStatus || got.IsActive != tt.wantActive {
				t.Fatalf("got %+v", got)
			}
			if (got.ExpiresAt != nil) != tt.wantExpiry {
				t.Fatalf("expires_at = %v, want set=%v", got.ExpiresAt, tt.wantExpiry)
			}
			if (got.UpdatedAt != nil) != (tt.row != nil) {
				t.Fatalf("updated_at = %v, want set=%v", got.UpdatedAt, tt.row != nil)
			}
		})
	}
}

func TestMembershipServiceGetErrors(t *testing.T) {
	userID := uuid.New()

	t.Run("unknown user", func(t *testing.T) {
		svc := NewMembershipService(newFakeMembershipStore(), fakeUsers{})
		_, err := svc.Get(context.Background(), userID)
		assertAppError(t, err, http.StatusNotFound)
	})

	t.Run("store failure", func(t *testing.T) {
		boom := errors.New("boom")
		store := newFakeMembershipStore()
		store.findErr = boom
		svc := NewMembershipService(store, fakeUsers{userID: true})
		if _, err := svc.Get(context.Background(), userID); !errors.Is(err, boom) {
			t.Fatalf("want wrapped store error, got %v", err)
		}
	})
}

func TestMembershipServiceSet(t *testing.T) {
	userID := uuid.New()
	future := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)

	tests := []struct {
		name       string
		existing   *model.Membership
		in         SetMembershipInput
		wantActive bool
		wantExpiry *time.Time
	}{
		{
			name:       "premium without expiry",
			in:         SetMembershipInput{Status: model.MembershipStatusPremium},
			wantActive: true,
		},
		{
			name:       "premium with future expiry",
			in:         SetMembershipInput{Status: model.MembershipStatusPremium, ExpiresAt: &future},
			wantActive: true,
			wantExpiry: &future,
		},
		{
			name:     "free clears a previous expiry",
			existing: &model.Membership{Status: model.MembershipStatusPremium, ExpiresAt: &future},
			in:       SetMembershipInput{Status: model.MembershipStatusFree},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeMembershipStore()
			if tt.existing != nil {
				tt.existing.UserID = userID
				store.rows[userID] = tt.existing
			}
			svc := NewMembershipService(store, fakeUsers{userID: true})

			got, err := svc.Set(context.Background(), userID, tt.in)
			if err != nil {
				t.Fatalf("Set: %v", err)
			}
			if got.UserID != userID || got.Status != tt.in.Status || got.IsActive != tt.wantActive || got.UpdatedAt == nil {
				t.Fatalf("got %+v", got)
			}
			if (got.ExpiresAt == nil) != (tt.wantExpiry == nil) || (tt.wantExpiry != nil && !got.ExpiresAt.Equal(*tt.wantExpiry)) {
				t.Fatalf("expires_at = %v, want %v", got.ExpiresAt, tt.wantExpiry)
			}
			if len(store.upserts) != 1 {
				t.Fatalf("want 1 upsert, got %d", len(store.upserts))
			}
			saved := store.upserts[0]
			if saved.UserID != userID || saved.Status != tt.in.Status || (saved.ExpiresAt == nil) != (tt.wantExpiry == nil) {
				t.Fatalf("saved %+v", saved)
			}
		})
	}
}

func TestMembershipServiceSetRejects(t *testing.T) {
	userID := uuid.New()
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-time.Minute)

	tests := []struct {
		name     string
		users    fakeUsers
		in       SetMembershipInput
		wantCode int
	}{
		{name: "expiry in the past", users: fakeUsers{userID: true}, in: SetMembershipInput{Status: model.MembershipStatusPremium, ExpiresAt: &past}, wantCode: http.StatusBadRequest},
		{name: "free with expiry", users: fakeUsers{userID: true}, in: SetMembershipInput{Status: model.MembershipStatusFree, ExpiresAt: &future}, wantCode: http.StatusBadRequest},
		{name: "unknown user", users: fakeUsers{}, in: SetMembershipInput{Status: model.MembershipStatusPremium}, wantCode: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeMembershipStore()
			svc := NewMembershipService(store, tt.users)

			_, err := svc.Set(context.Background(), userID, tt.in)
			assertAppError(t, err, tt.wantCode)
			if len(store.upserts) != 0 {
				t.Fatalf("membership must not change, got %d upserts", len(store.upserts))
			}
		})
	}
}

func TestMembershipServiceSetStoreFailure(t *testing.T) {
	userID := uuid.New()
	boom := errors.New("boom")
	store := newFakeMembershipStore()
	store.upsertErr = boom
	svc := NewMembershipService(store, fakeUsers{userID: true})

	if _, err := svc.Set(context.Background(), userID, SetMembershipInput{Status: model.MembershipStatusPremium}); !errors.Is(err, boom) {
		t.Fatalf("want wrapped store error, got %v", err)
	}
}

func TestMembershipServiceIsActive(t *testing.T) {
	userID := uuid.New()
	past := time.Now().Add(-time.Hour)

	tests := []struct {
		name string
		row  *model.Membership
		want bool
	}{
		{name: "no row", want: false},
		{name: "premium", row: &model.Membership{Status: model.MembershipStatusPremium}, want: true},
		{name: "expired premium", row: &model.Membership{Status: model.MembershipStatusPremium, ExpiresAt: ptrTime(past)}, want: false},
		{name: "free", row: &model.Membership{Status: model.MembershipStatusFree}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeMembershipStore()
			if tt.row != nil {
				store.rows[userID] = tt.row
			}
			got, err := NewMembershipService(store, fakeUsers{}).IsActive(context.Background(), userID)
			if err != nil {
				t.Fatalf("IsActive: %v", err)
			}
			if got != tt.want {
				t.Fatalf("IsActive = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("store failure", func(t *testing.T) {
		boom := errors.New("boom")
		store := newFakeMembershipStore()
		store.findErr = boom
		if _, err := NewMembershipService(store, fakeUsers{}).IsActive(context.Background(), userID); !errors.Is(err, boom) {
			t.Fatalf("want wrapped store error, got %v", err)
		}
	})
}
