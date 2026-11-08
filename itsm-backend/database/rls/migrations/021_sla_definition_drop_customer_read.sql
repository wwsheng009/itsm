-- =============================================================================
-- RLS Migration 021 — 收回 sla_definition_customer_read（Q-09 裁决 B）
--
-- 前置：020 已建 sla_definition_snapshots，L1/provided 已改读客户租户快照。
-- 保留：sla_grant_customer_read（授权元数据 allowlist）。
-- 回滚：021_sla_definition_drop_customer_read_rollback.sql
-- =============================================================================

DO $$
BEGIN
    IF to_regclass('public.sla_definitions') IS NOT NULL THEN
        DROP POLICY IF EXISTS sla_definition_customer_read ON sla_definitions;
        RAISE NOTICE 'rls 021: dropped sla_definition_customer_read';
    END IF;
END $$;
