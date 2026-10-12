-- +goose Up
-- Refresh tokens now live in Redis with a TTL; see internal/repository/token.repository.go.
DROP TABLE IF EXISTS refresh_tokens;

-- +goose Down
CREATE TABLE refresh_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash varchar(64)  NOT NULL UNIQUE,
    expires_at timestamptz  NOT NULL,
    revoked_at timestamptz,
    user_agent varchar(255) NOT NULL DEFAULT '',
    ip_address varchar(64)  NOT NULL DEFAULT '',
    created_at timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens (user_id);
