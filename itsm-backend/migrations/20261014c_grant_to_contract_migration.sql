-- SLA 合同化专项 P1-2 并轨闸门①：存量 grant → 合同链单向搬迁（方案 §4.4，Accepted v1.11）
--
-- 语义：
--   - 每个有 active grant 的 (provider, customer) 生成一条"自动迁移合同"
--     （contract_no = AUTO-MIGRATED-<customer_tenant_id>-<YYYYMMDD>，长期有效、月度结算）；
--   - 该 customer 的全部 active grant 逐条转为合同绑定（含 ticket_type_id 类型维度）；
--   - 幂等：ON CONFLICT DO NOTHING（合同按 uk_msp_contracts_tenant_no 去重；
--     绑定按 uk_msp_contract_bindings_type 去重）。
--
-- 与 ③（grant 表转只读观察一周期后归档）的关系：本迁移不回写不删除 grant；
-- grant 写端点已在应用层 410（写入口唯一化），表的物理归档由运维观察期后执行。
--
-- 说明：RLS 采用"只 ENABLE 不 FORCE"（003–012 约定），迁移以 owner 连接执行不受策略约束；
-- 表结构由 ent Schema.Create 先行创建（本迁移在 20261014b 之后执行）。

DO $$
DECLARE
    migrated_contracts int;
    migrated_bindings  int;
BEGIN
    IF to_regclass('public.sla_definition_grants') IS NULL
       OR to_regclass('public.msp_contracts') IS NULL
       OR to_regclass('public.msp_contract_sla_bindings') IS NULL THEN
        RAISE NOTICE 'sla 20261014c: skip (tables not found)';
        RETURN;
    END IF;

    -- ① 自动迁移合同（每个 active grant 的 (provider, customer) 一条；
    --    contract_no 固定不用日期——跨天手工重跑也幂等，且每客户至多一条自动迁移合同）
    INSERT INTO msp_contracts (
        provider_tenant_id, customer_tenant_id, contract_no, name,
        effective_from, status, settlement_cycle, tenant_id, created_at, updated_at
    )
    SELECT DISTINCT
        g.provider_tenant_id,
        g.customer_tenant_id,
        'AUTO-MIGRATED-' || g.customer_tenant_id,
        '自动迁移合同（来自 P0 下发授权）',
        now(), 'active', 'monthly', g.provider_tenant_id, now(), now()
    FROM sla_definition_grants g
    WHERE g.is_active
    ON CONFLICT (tenant_id, contract_no) DO NOTHING;
    GET DIAGNOSTICS migrated_contracts = ROW_COUNT;

    -- ② 绑定搬迁（active grant → binding；同合同同类型仅一条）
    INSERT INTO msp_contract_sla_bindings (
        contract_id, sla_definition_id, ticket_type_id, tenant_id, created_at, updated_at
    )
    SELECT
        c.id, g.sla_definition_id, g.ticket_type_id, g.tenant_id, now(), now()
    FROM sla_definition_grants g
    JOIN msp_contracts c
      ON c.provider_tenant_id = g.provider_tenant_id
     AND c.customer_tenant_id = g.customer_tenant_id
     AND c.contract_no = 'AUTO-MIGRATED-' || g.customer_tenant_id
    WHERE g.is_active
    ON CONFLICT (contract_id, ticket_type_id) DO NOTHING;
    GET DIAGNOSTICS migrated_bindings = ROW_COUNT;

    RAISE NOTICE 'sla 20261014c: migrated % contracts, % bindings', migrated_contracts, migrated_bindings;
END $$;
