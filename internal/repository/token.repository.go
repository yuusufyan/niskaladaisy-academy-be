package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ErrTokenNotFound means the refresh token is unknown, expired or already used.
var ErrTokenNotFound = errors.New("refresh token not found")

// RefreshSession is what a refresh token points to. The token itself is only stored as a hash.
type RefreshSession struct {
	UserID    uuid.UUID `json:"user_id"`
	UserAgent string    `json:"user_agent"`
	IPAddress string    `json:"ip_address"`
	ExpiresAt time.Time `json:"expires_at"`
}

// consumeScript deletes a session and leaves a "used" marker for its remaining lifetime,
// atomically, so two requests can never rotate the same token.
var consumeScript = redis.NewScript(`
local ttl = redis.call('PTTL', KEYS[1])
local v = redis.call('GET', KEYS[1])
if not v then return false end
redis.call('DEL', KEYS[1])
if ttl > 0 then redis.call('SET', KEYS[2], v, 'PX', ttl) end
return v
`)

// TokenRepository stores refresh sessions in Redis; keys expire on their own with the token.
type TokenRepository struct {
	rdb *redis.Client
}

func NewTokenRepository(rdb *redis.Client) *TokenRepository {
	return &TokenRepository{rdb: rdb}
}

func sessionKey(hash string) string           { return "um:rt:" + hash }
func usedKey(hash string) string              { return "um:rt:used:" + hash }
func userSessionsKey(userID uuid.UUID) string { return "um:rt:user:" + userID.String() }

func (r *TokenRepository) Save(ctx context.Context, hash string, s RefreshSession) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	ttl := time.Until(s.ExpiresAt)
	_, err = r.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, sessionKey(hash), raw, ttl)
		p.SAdd(ctx, userSessionsKey(s.UserID), hash)
		// The index lives as long as the newest session; stale members are harmless.
		p.Expire(ctx, userSessionsKey(s.UserID), ttl)
		return nil
	})
	return err
}

// Consume removes the session and marks the token as used. It returns ErrTokenNotFound
// when the token is unknown, expired or already consumed.
func (r *TokenRepository) Consume(ctx context.Context, hash string) (*RefreshSession, error) {
	raw, err := consumeScript.Run(ctx, r.rdb, []string{sessionKey(hash), usedKey(hash)}).Text()
	if errors.Is(err, redis.Nil) {
		return nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	var s RefreshSession
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, err
	}
	if err := r.rdb.SRem(ctx, userSessionsKey(s.UserID), hash).Err(); err != nil {
		return nil, err
	}
	return &s, nil
}

// UsedBy returns the owner of a token that was already rotated, if it is still within its lifetime.
func (r *TokenRepository) UsedBy(ctx context.Context, hash string) (uuid.UUID, bool, error) {
	raw, err := r.rdb.Get(ctx, usedKey(hash)).Bytes()
	if errors.Is(err, redis.Nil) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	var s RefreshSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return uuid.Nil, false, err
	}
	return s.UserID, true, nil
}

// Delete removes a session without marking it as used (logout).
func (r *TokenRepository) Delete(ctx context.Context, hash string) error {
	raw, err := r.rdb.GetDel(ctx, sessionKey(hash)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	var s RefreshSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	return r.rdb.SRem(ctx, userSessionsKey(s.UserID), hash).Err()
}

// RevokeAllForUser deletes every refresh session of the user.
func (r *TokenRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	hashes, err := r.rdb.SMembers(ctx, userSessionsKey(userID)).Result()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(hashes)+1)
	for _, h := range hashes {
		keys = append(keys, sessionKey(h))
	}
	keys = append(keys, userSessionsKey(userID))
	return r.rdb.Del(ctx, keys...).Err()
}
