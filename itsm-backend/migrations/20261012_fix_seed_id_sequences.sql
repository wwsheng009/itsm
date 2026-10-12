-- =============================================================================
-- Fix seed ID sequences after explicit-ID seeding (G5) + fresh-DB orphan cleanup
-- =============================================================================
-- 背景（2026-10-12，FDB-B1 全新库实测，见 docs/multi-tenant/evidence/fresh-db-browser-B1/）：
--   1) 历史迁移以显式 id 播种（如 20260501_enable_rbac_from_db 的 roles id 100–112），
--      对应序列未推进 → 后续 provision_tenant 通过 nextval 插入触发
--      `roles_pkey duplicate key`；
--   2) 上述迁移按 tenant_id=1 口径写入，而当前 ent 初始化下 default 租户 id 可能不是 1
--      （全新库为 2）→ 遗留无归属的 tenant_id=1 孤儿角色行。
-- 本迁移为 forward fix（不改历史文件，避免 schema_migrations checksum 漂移）：
--   - 清理无对应租户的 tenant_id=1 孤儿行（存量库 tenant 1 存在时不生效）；
--   - 校正 9 张自增表序列到 max(id)（幂等，下次 nextval = max+1）。
-- =============================================================================

-- STEP 1: 孤儿行清理（仅当实例不存在租户 1）
DELETE FROM role_permissions rp
WHERE rp.tenant_id = 1
  AND NOT EXISTS (SELECT 1 FROM tenants t WHERE t.id = 1);

DELETE FROM roles r
WHERE r.tenant_id = 1
  AND NOT EXISTS (SELECT 1 FROM tenants t WHERE t.id = 1);

-- STEP 2: 序列校正（幂等）
DO $$
DECLARE
    t text;
    seq text;
    mx bigint;
BEGIN
    FOREACH t IN ARRAY ARRAY['roles','permissions','menus','groups','role_permissions',
                             'sla_definitions','ci_types','approval_workflows','system_configs'] LOOP
        seq := pg_get_serial_sequence(t, 'id');
        IF seq IS NULL THEN
            CONTINUE;
        END IF;
        EXECUTE format('SELECT coalesce(max(id),0) FROM %I', t) INTO mx;
        PERFORM setval(seq, greatest(mx, 1));
    END LOOP;
END $$;
