-- +goose Up
-- Codes must match the constants in internal/model/permission.go.
INSERT INTO permissions (code, name, module, description) VALUES
    ('user.read',       'View users',        'user',       'List and view users'),
    ('user.create',     'Create users',      'user',       'Create new users'),
    ('user.update',     'Update users',      'user',       'Edit users, their roles and reset passwords'),
    ('user.delete',     'Delete users',      'user',       'Delete users'),
    ('role.read',       'View roles',        'role',       'List and view roles'),
    ('role.create',     'Create roles',      'role',       'Create new roles'),
    ('role.update',     'Update roles',      'role',       'Edit roles, their permissions and menus'),
    ('role.delete',     'Delete roles',      'role',       'Delete roles'),
    ('permission.read', 'View permissions',  'permission', 'List available permissions'),
    ('menu.read',       'View menus',        'menu',       'List and view menus'),
    ('menu.create',     'Create menus',      'menu',       'Create new menus'),
    ('menu.update',     'Update menus',      'menu',       'Edit menus'),
    ('menu.delete',     'Delete menus',      'menu',       'Delete menus');

-- superadmin bypasses every permission and menu check, so it needs no mappings.
INSERT INTO roles (code, name, description, is_system) VALUES
    ('superadmin', 'Super Admin', 'Full access to every feature', true);

INSERT INTO menus (code, name, path, icon, sort_order) VALUES
    ('dashboard',       'Dashboard',       '/dashboard', 'layout-dashboard', 1),
    ('user_management', 'User Management', '',           'users',            90);

INSERT INTO menus (parent_id, code, name, path, icon, sort_order)
SELECT m.id, v.code, v.name, v.path, v.icon, v.sort_order
FROM menus m,
     (VALUES ('users', 'Users', '/user-management/users', 'user',   1),
             ('roles', 'Roles', '/user-management/roles', 'shield', 2),
             ('menus', 'Menus', '/user-management/menus', 'list',   3)
     ) AS v (code, name, path, icon, sort_order)
WHERE m.code = 'user_management';

-- +goose Down
DELETE FROM menus WHERE code IN ('users', 'roles', 'menus');
DELETE FROM menus WHERE code IN ('dashboard', 'user_management');
DELETE FROM roles WHERE code = 'superadmin';
DELETE FROM permissions WHERE module IN ('user', 'role', 'permission', 'menu');
