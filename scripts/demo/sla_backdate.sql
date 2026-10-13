-- =====================================================================
-- 演示库专用：为部分 [DEMO] 工单回填「已超时」SLA 截止时间
-- =====================================================================
-- 目的：演示「SLA 监控 / 超时工单 / 预警」需要逾期样本；正常路径无法通过 API
--       指定历史截止时间，故提供本幂等脚本（仅用于演示库 itsm_demo）。
--
-- 用法（在 238 主机执行，注意不要在非演示库运行）：
--   docker cp scripts/demo/sla_backdate.sql itsm-postgres-prod:/tmp/ 2>/dev/null || true
--   docker exec -i itsm-postgres-prod psql -U itsm -d itsm_demo < scripts/demo/sla_backdate.sql
--
-- 幂等：仅当截止时间仍在未来时才回填（已超时的不重复修改）。
-- 范围：每租户最近 2 张未关闭的 [DEMO] 工单（含响应/解决两个截止时间）。
-- =====================================================================

WITH targets AS (
    SELECT id,
           row_number() OVER (PARTITION BY tenant_id ORDER BY id) AS rn
    FROM tickets
    WHERE title LIKE '[DEMO]%'
      AND status NOT IN ('resolved', 'closed', 'cancelled')
      AND sla_resolution_deadline IS NOT NULL
      AND sla_resolution_deadline > now()
)
UPDATE tickets t
SET sla_resolution_deadline = now() - interval '6 hours',
    sla_response_deadline   = LEAST(
        COALESCE(t.sla_response_deadline, now()),
        now() - interval '2 hours'
    ),
    updated_at = now()
FROM targets
WHERE t.id = targets.id
  AND targets.rn <= 2;

-- 回执：超时未关闭分布
SELECT tenant_id,
       count(*) FILTER (WHERE sla_resolution_deadline < now()) AS overdue_open
FROM tickets
WHERE status NOT IN ('resolved', 'closed', 'cancelled')
GROUP BY tenant_id
ORDER BY tenant_id;
