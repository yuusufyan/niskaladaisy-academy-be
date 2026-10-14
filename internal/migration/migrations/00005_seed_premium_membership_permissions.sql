-- +goose Up
-- Codes must match the constants in internal/model/permission.go.
-- No role mappings on purpose: the video.* permissions also unlock every premium video,
-- so grant them only to roles that manage video content.
INSERT INTO permissions (code, name, module, description) VALUES
    ('membership.update', 'Update memberships', 'membership', 'Set a user''s membership status and expiry'),
    ('video.create',      'Create videos',      'video',      'Create new videos; also unlocks every premium video'),
    ('video.update',      'Update videos',      'video',      'Edit videos; also unlocks every premium video'),
    ('video.delete',      'Delete videos',      'video',      'Delete videos; also unlocks every premium video');

-- +goose Down
DELETE FROM permissions WHERE module IN ('membership', 'video');
