-- SLA 合同化专项 P1-1 合同模型包（方案 Accepted v1.11，docs/multi-tenant/plan/sla-contract-multitenant-plan-2026-10-14.md §4.4）
--
-- 内容：
--   1) msp_contracts / msp_contract_sla_bindings 索引兜底（表由 ent Schema.Create 创建；
--      缺失则跳过——对齐 P0-1 的幂等双轨惯例）。
--   2) 实施期修正 IM-13（绑定表类型维度）/IM-14（tenant_id 列）已在 ent schema 中体现，
--      本文件仅兜底索引。
--
-- 并轨闸门（§4.4）：存量 grant 搬迁与写入口唯一化在 P1-2 包落地（需应用层写入逻辑，
-- 非纯 DDL）——本文件不含数据搬迁。

DO $$
BEGIN
    IF to_regclass('public.msp_contracts') IS NULL THEN
        RAISE NOTICE 'sla 20261014b: skip contract indexes (table not found)';
    ELSE
        CREATE UNIQUE INDEX IF NOT EXISTS uk_msp_contracts_tenant_no
            ON msp_contracts (tenant_id, contract_no);
        CREATE INDEX IF NOT EXISTS idx_msp_contracts_customer_status
            ON msp_contracts (tenant_id, customer_tenant_id, status);
        RAISE NOTICE 'sla 20261014b: contract indexes ensured';
    END IF;

    IF to_regclass('public.msp_contract_sla_bindings') IS NULL THEN
        RAISE NOTICE 'sla 20261014b: skip binding indexes (table not found)';
    ELSE
        CREATE UNIQUE INDEX IF NOT EXISTS uk_msp_contract_bindings_type
            ON msp_contract_sla_bindings (contract_id, ticket_type_id);
        CREATE INDEX IF NOT EXISTS idx_msp_contract_bindings_definition
            ON msp_contract_sla_bindings (sla_definition_id);
        CREATE INDEX IF NOT EXISTS idx_msp_contract_bindings_tenant
            ON msp_contract_sla_bindings (tenant_id);
        RAISE NOTICE 'sla 20261014b: binding indexes ensured';
    END IF;
END $$;
