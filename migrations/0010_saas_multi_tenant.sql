-- 0010_saas_multi_tenant.sql — SaaS tenant lifecycle (specs/08.2, docs/adr/adr-006).
-- Platform admins belong to a dedicated system tenant (code='platform') and are
-- flagged on users; they manage tenants via /platform/* endpoints.

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_platform_admin BOOLEAN NOT NULL DEFAULT false;

INSERT INTO tenants (id, name, code, status, metadata, created_by)
VALUES ('platform-system-tenant', '平台运营（系统）', 'platform', 'active',
        '{"system": true}', 'migration')
ON CONFLICT (code) DO NOTHING;
