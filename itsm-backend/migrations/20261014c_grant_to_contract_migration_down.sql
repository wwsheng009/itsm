-- SLA 合同化专项 P1-2 并轨闸门① rollback（20261014c_grant_to_contract_migration.sql）
--
-- 数据搬迁不可逆：自动迁移合同（AUTO-MIGRATED-<customer_tenant_id>）与绑定
-- 为业务的真实承接载体，回滚删除会中断客户 SLA 匹配。
-- 如需撤销，请人工评估后按业务决策处理（建议保持只增不减）。
--
-- 本 down 文件为仓库 up/down 配对惯例占位，不执行任何删除。

DO $$
BEGIN
    RAISE NOTICE 'sla 20261014c rollback: data migration is intentionally irreversible; no-op';
END $$;
