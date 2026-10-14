-- +goose Up
-- One row per user, created the first time an admin sets the status; no row means free.
CREATE TABLE memberships (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status     varchar(20) NOT NULL DEFAULT 'free' CHECK (status IN ('free', 'premium')),
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    created_by varchar(36) NOT NULL DEFAULT '',
    updated_by varchar(36) NOT NULL DEFAULT '',
    CONSTRAINT chk_memberships_free_has_no_expiry CHECK (status = 'premium' OR expires_at IS NULL)
);
-- Also the ON CONFLICT target of the membership upsert.
CREATE UNIQUE INDEX uq_memberships_user_id ON memberships (user_id);
CREATE INDEX idx_memberships_deleted_at ON memberships (deleted_at);

CREATE TABLE videos (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title       varchar(200)  NOT NULL,
    description varchar(2000) NOT NULL DEFAULT '',
    video_url   varchar(500)  NOT NULL,
    is_premium  boolean       NOT NULL DEFAULT false,
    created_at  timestamptz   NOT NULL DEFAULT now(),
    updated_at  timestamptz   NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    created_by  varchar(36)   NOT NULL DEFAULT '',
    updated_by  varchar(36)   NOT NULL DEFAULT ''
);
CREATE INDEX idx_videos_deleted_at ON videos (deleted_at);

-- +goose Down
DROP TABLE IF EXISTS videos;
DROP TABLE IF EXISTS memberships;
