-- SLA 合同化专项 P0-1 模型包（方案 Accepted v1.11，docs/multi-tenant/plan/sla-contract-multitenant-plan-2026-10-14.md）
--
-- 内容：
--   1) sla_definitions.name 租户内唯一（工单 19，v1.8）：
--      先存量重名机械清洗（保留最早一行原名，其余追加 -dup-<id> 后缀），再建唯一索引。
--      【实施期修正】新表/新列由 ent Schema.Create（AutoMigrate）先行；脏数据上的
--      CREATE UNIQUE INDEX 会阻塞启动，故唯一性不用 ent 声明、只在本文件"先清洗后建"。
--   2) sla_definition_grants（工单 4，D-SLA-1）：partial unique 与辅助索引兜底
--      （与 ent/schema/sla_definition_grant.go 同源声明，幂等双轨——对齐 msp_allocation/022 先例）。
--   3) 新列兜底（IF NOT EXISTS；正常路径由 ent 迁移）：
--      sla_definitions.owner_provider_tenant_id（工单 3）、ticket_types.sla_exempt（工单 18）。
--
-- 顺序保证：本文件内"清洗 → 建索引"严格有序；跨文件顺序 = Schema.Create → 本迁移（bootstrap app.go:1662/1666/1787）。

-- 1) 存量重名清洗（循环至无重名；机械后缀保证收敛）
DO $$
DECLARE
    updated int;
BEGIN
    LOOP
        WITH ranked AS (
            SELECT id, name,
                   ROW_NUMBER() OVER (PARTITION BY tenant_id, name ORDER BY id) AS rn
            FROM sla_definitions
        )
        UPDATE sla_definitions d
        SET name = ranked.name || '-dup-' || ranked.id
        FROM ranked
        WHERE d.id = ranked.id AND ranked.rn > 1;

        GET DIAGNOSTICS updated = ROW_COUNT;
        EXIT WHEN updated = 0;
    END LOOP;
    RAISE NOTICE 'sla 20261014: duplicate names normalized';
END $$;

-- 2) sla_definitions.name 租户内唯一
CREATE UNIQUE INDEX IF NOT EXISTS uk_sla_definitions_tenant_name
    ON sla_definitions (tenant_id, name);

-- 3) sla_definition_grants 索引兜底（表由 ent 创建；缺失则跳过）
DO $$
BEGIN
    IF to_regclass('public.sla_definition_grants') IS NULL THEN
        RAISE NOTICE 'sla 20261014: skip grant indexes (table not found)';
    ELSE
        CREATE UNIQUE INDEX IF NOT EXISTS uk_sla_definition_grants_active
            ON sla_definition_grants (customer_tenant_id, ticket_type_id)
            WHERE is_active;
        CREATE INDEX IF NOT EXISTS idx_sla_definition_grants_provider
            ON sla_definition_grants (provider_tenant_id);
        CREATE INDEX IF NOT EXISTS idx_sla_definition_grants_definition
            ON sla_definition_grants (sla_definition_id);
        RAISE NOTICE 'sla 20261014: grant indexes ensured';
    END IF;
END $$;

-- 4) 新列兜底（幂等）
ALTER TABLE sla_definitions ADD COLUMN IF NOT EXISTS owner_provider_tenant_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ticket_types ADD COLUMN IF NOT EXISTS sla_exempt BOOLEAN NOT NULL DEFAULT FALSE;
