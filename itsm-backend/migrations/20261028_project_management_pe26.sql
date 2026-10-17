-- =============================================================================
-- 项目管理 P2 采购/资产结项登记（PE-26）数据层兜底迁移
--
-- 背景：project_asset_links 承载结项清单「资产/许可登记」的结构化台账链接
--   （link_type=asset|asset_license|vendor|contract，同租户弱引用 target_id）。
--   权威定义 docs/project-management/plan/p2-procurement-design.md §3（D-49/D-50）；
--   表由 ent Schema.Create 先行创建，本文件幂等补齐索引（对齐 pe21~pe24 双轨惯例）。
-- 说明：未删行唯一为 partial unique index（ent 不支持 partial index，故在本文件兜底）。
-- 应用：go run -tags migrate . （或 psql -f）
-- 回滚：20261028_project_management_pe26_down.sql
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.project_asset_links') IS NULL THEN
        RAISE NOTICE 'project-pe26 20261028: skip (table not found; ent migrate 未执行?)';
        RETURN;
    END IF;

    CREATE INDEX IF NOT EXISTS idx_project_asset_links_tenant_project
        ON project_asset_links (tenant_id, project_id, created_at);

    CREATE UNIQUE INDEX IF NOT EXISTS uq_project_asset_links_active_target
        ON project_asset_links (tenant_id, project_id, link_type, target_id)
        WHERE deleted_at IS NULL;

    RAISE NOTICE 'project-pe26 20261028: project_asset_links indexes ensured';
END $$;