-- 0012_wildcard_permission.sql — real wildcard permission so tenant_admin keeps
-- covering permissions added after provisioning (previously the "*:*" expansion
-- was baked in at seed time and new catalog codes missed existing roles).

INSERT INTO permissions (id, code, resource, action, name)
VALUES ('perm-wildcard', '*:*', '*', '*', 'All permissions')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r
JOIN permissions p ON p.code = '*:*'
WHERE r.code = 'tenant_admin'
ON CONFLICT DO NOTHING;
