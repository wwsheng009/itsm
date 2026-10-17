-- =============================================================================
-- 项目管理 P2 采购/资产结项登记（PE-26）回滚
--
-- 说明：仅回滚本切片兜底索引（表本身由 ent Schema 管理，不在此删除）。
-- 回滚：20261028_project_management_pe26.sql 的逆操作。
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.project_asset_links') IS NOT NULL THEN
        DROP INDEX IF EXISTS uq_project_asset_links_active_target;
        DROP INDEX IF EXISTS idx_project_asset_links_tenant_project;
        RAISE NOTICE 'project-pe26 20261028 rollback: project_asset_links indexes dropped';
    ELSE
        RAISE NOTICE 'project-pe26 20261028 rollback: skip (table not found)';
    END IF;
END $$;