-- +goose Up
-- created_by/updated_by are varchar with '' default because database.AuditModel
-- writes an empty string when no user is in context (e.g. seeding).

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          varchar(150) NOT NULL,
    email         varchar(150) NOT NULL,
    password_hash varchar(255) NOT NULL,
    is_active     boolean      NOT NULL DEFAULT true,
    last_login_at timestamptz,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    updated_at    timestamptz  NOT NULL DEFAULT now(),
    deleted_at    timestamptz,
    created_by    varchar(36)  NOT NULL DEFAULT '',
    updated_by    varchar(36)  NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX uq_users_email ON users (lower(email)) WHERE deleted_at IS NULL;
CREATE INDEX idx_users_deleted_at ON users (deleted_at);

CREATE TABLE roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code        varchar(50)  NOT NULL,
    name        varchar(100) NOT NULL,
    description varchar(500) NOT NULL DEFAULT '',
    is_system   boolean      NOT NULL DEFAULT false,
    created_at  timestamptz  NOT NULL DEFAULT now(),
    updated_at  timestamptz  NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    created_by  varchar(36)  NOT NULL DEFAULT '',
    updated_by  varchar(36)  NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX uq_roles_code ON roles (code) WHERE deleted_at IS NULL;
CREATE INDEX idx_roles_deleted_at ON roles (deleted_at);

-- Permissions are defined by the code (route guards), so they are seeded, not user-editable.
CREATE TABLE permissions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code        varchar(100) NOT NULL UNIQUE,
    name        varchar(150) NOT NULL,
    module      varchar(50)  NOT NULL,
    description varchar(500) NOT NULL DEFAULT '',
    created_at  timestamptz  NOT NULL DEFAULT now()
);

CREATE TABLE menus (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id  uuid REFERENCES menus (id) ON DELETE SET NULL,
    code       varchar(50)  NOT NULL,
    name       varchar(100) NOT NULL,
    path       varchar(255) NOT NULL DEFAULT '',
    icon       varchar(100) NOT NULL DEFAULT '',
    sort_order integer      NOT NULL DEFAULT 0,
    is_active  boolean      NOT NULL DEFAULT true,
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_at timestamptz  NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    created_by varchar(36)  NOT NULL DEFAULT '',
    updated_by varchar(36)  NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX uq_menus_code ON menus (code) WHERE deleted_at IS NULL;
CREATE INDEX idx_menus_parent_id ON menus (parent_id);
CREATE INDEX idx_menus_deleted_at ON menus (deleted_at);

CREATE TABLE user_roles (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id uuid NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);
CREATE INDEX idx_user_roles_role_id ON user_roles (role_id);

CREATE TABLE role_permissions (
    role_id       uuid NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES permissions (id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE role_menus (
    role_id uuid NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    menu_id uuid NOT NULL REFERENCES menus (id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, menu_id)
);

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

-- +goose Down
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS role_menus;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS menus;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS users;
