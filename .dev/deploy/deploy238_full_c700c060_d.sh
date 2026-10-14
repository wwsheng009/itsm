#!/usr/bin/env bash
# 238 demo 全栈更新 D 步：等待健康 → RLS 013 注入 → 增量全量验证（幂等，可重复执行）
set -u
NEW="${NEW:-1.8.0-dev.c700c060}"
OLD="${OLD:-1.8.0-dev.1e60efee}"
DB=itsm_demo
PSQL="docker exec itsm-postgres-prod psql -U itsm -d $DB -tAc"

echo "===== [D1] 健康等待（最多 25x12s=5min） ====="
READY=0
for i in $(seq 1 25); do
  INIT=$(docker inspect -f '{{.State.Status}}/exit={{.State.ExitCode}}' itsm-init-demo 2>/dev/null || echo missing)
  BE=$(docker inspect -f '{{.State.Health.Status}}' itsm-backend-demo 2>/dev/null || echo missing)
  WK=$(docker inspect -f '{{.State.Health.Status}}' itsm-worker-demo 2>/dev/null || echo missing)
  FE=$(docker inspect -f '{{.State.Health.Status}}' itsm-frontend-demo 2>/dev/null || echo missing)
  echo "poll#$i init=$INIT backend=$BE worker=$WK frontend=$FE"
  if [ "$BE" = "healthy" ] && [ "$FE" = "healthy" ] && [ "$WK" = "healthy" ]; then READY=1; break; fi
  sleep 12
done
if [ "$READY" != "1" ]; then
  echo "--- init logs tail ---"; docker logs --tail 12 itsm-init-demo 2>&1 | tail -12
  echo "--- backend logs tail ---"; docker logs --tail 12 itsm-backend-demo 2>&1 | tail -12
  echo "D-STATUS: WAIT"; exit 0
fi

echo "===== [D2] RLS 013 注入（幂等） ====="
RLS013=/tmp/013_ticket_type_distribution_policies.sql
if docker cp "$RLS013" itsm-postgres-prod:/tmp/rls013.sql 2>/dev/null; then
  docker exec itsm-postgres-prod psql -U itsm -d "$DB" -v ON_ERROR_STOP=1 -f /tmp/rls013.sql 2>&1 | grep -E 'NOTICE|ERROR' | tail -3
else
  echo "(docker cp failed; fallback stdin)"
  docker exec -i itsm-postgres-prod psql -U itsm -d "$DB" -v ON_ERROR_STOP=1 -f - < "$RLS013" 2>&1 | grep -E 'NOTICE|ERROR' | tail -3
fi

echo "===== [D3] 迁移与结构（增量 e/f + P3 前置） ====="
echo "--- schema_migrations 20261014* ---"
$PSQL "SELECT version FROM schema_migrations WHERE version LIKE '20261014%' ORDER BY version"
echo "--- incremental columns ---"
$PSQL "SELECT table_name||'.'||column_name FROM information_schema.columns WHERE (table_name='tickets' AND column_name='sla_matched_layer') OR (table_name='sla_violations' AND column_name IN ('matched_layer','resolution_state')) OR (table_name='ticket_types' AND column_name IN ('origin','controlled_by_provider','sync_status')) ORDER BY 1"
echo "--- new tables ---"
$PSQL "SELECT to_regclass('public.ticket_type_distributions')||' | '||to_regclass('public.msp_contracts')||' | '||to_regclass('public.sla_definition_grants')"

echo "===== [D4] RLS 状态与策略 ====="
$PSQL "SELECT relname||'|rls='||relrowsecurity FROM pg_class WHERE relname IN ('ticket_type_distributions','sla_definition_grants','msp_contracts','msp_contract_sla_bindings') ORDER BY relname"
$PSQL "SELECT polname FROM pg_policy WHERE polname IN ('tenant_isolation_ticket_type_distributions','sla_grant_provider_all','sla_grant_customer_read','msp_contract_provider_all','msp_contract_customer_read') ORDER BY polname"

echo "===== [D5] 容器与路由（HTTP） ====="
docker inspect -f '{{.Config.Image}}' itsm-backend-demo itsm-worker-demo itsm-frontend-demo 2>/dev/null
curl -s -o /dev/null -w "demo-http=%{http_code}\n" http://127.0.0.1:8089/
curl -s -o /dev/null -w "readyz=%{http_code}\n" http://127.0.0.1:8091/api/v1/readyz
curl -s -o /dev/null -w "settlement=%{http_code}\n" http://127.0.0.1:8091/api/v1/sla/settlement-report
curl -s -o /dev/null -w "settlement-lock=%{http_code}\n" -X POST http://127.0.0.1:8091/api/v1/sla/settlement-report/lock
curl -s -o /dev/null -w "distributions=%{http_code}\n" http://127.0.0.1:8091/api/v1/msp/customers/1/ticket-types/distributions
curl -s -o /dev/null -w "settle=%{http_code}\n" -X PUT http://127.0.0.1:8091/api/v1/sla/violations/1/settle

echo "===== [D6] A/B 特征对比 ====="
echo "--- frontend asset token sla-settlement ---"
docker run --rm "itsm-frontend:$OLD" sh -c "grep -rl sla-settlement /usr/share/nginx/html/assets/ 2>/dev/null | wc -l" 2>/dev/null || echo "old-fe-N/A"
docker run --rm "itsm-frontend:$NEW" sh -c "grep -rl sla-settlement /usr/share/nginx/html/assets/ 2>/dev/null | wc -l" 2>/dev/null || echo "new-fe-N/A"
echo "--- backend binary token settlement-report (soft) ---"
docker run --rm "itsm-backend:$OLD" sh -c "grep -c settlement-report /app/main 2>/dev/null" 2>/dev/null || echo "old-be-N/A"
docker run --rm "itsm-backend:$NEW" sh -c "grep -c settlement-report /app/main 2>/dev/null" 2>/dev/null || echo "new-be-N/A"

# 卫生：恢复 .env.demo 属主（写入工具链以 root 运行）
chown n9e:n9e /home/n9e/itsm/repo/.env.demo 2>/dev/null || true
echo "D-STATUS: DONE"
