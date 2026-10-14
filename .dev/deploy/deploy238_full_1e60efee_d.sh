#!/usr/bin/env bash
# 238 demo 全栈更新 D 步：等待健康 → RLS 011/012 注入 itsm_demo → 全量验证（幂等，可重复执行）
set -u
NEW="${NEW:-1.8.0-dev.1e60efee}"
OLD="${OLD:-1.8.0-dev.b86819be}"
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

echo "===== [D2] RLS 011/012 -> $DB ====="
docker cp /tmp/011_sla_definition_grant_policies.sql itsm-postgres-prod:/tmp/rls011.sql
docker cp /tmp/012_msp_contract_policies.sql itsm-postgres-prod:/tmp/rls012.sql
docker exec itsm-postgres-prod psql -U itsm -d "$DB" -v ON_ERROR_STOP=1 -f /tmp/rls011.sql 2>&1 | grep -E 'NOTICE|ERROR|error' | tail -4
docker exec itsm-postgres-prod psql -U itsm -d "$DB" -v ON_ERROR_STOP=1 -f /tmp/rls012.sql 2>&1 | grep -E 'NOTICE|ERROR|error' | tail -4

echo "===== [D3] 迁移与结构抽查 ====="
echo "--- schema_migrations 20261014* ---"
$PSQL "SELECT version FROM schema_migrations WHERE version LIKE '20261014%' ORDER BY version"
echo "--- new columns ---"
$PSQL "SELECT table_name||'.'||column_name FROM information_schema.columns WHERE (table_name='sla_violations' AND column_name IN ('resolution_state','settled_flag','waived_by')) OR (table_name='sla_definitions' AND column_name='owner_provider_tenant_id') OR (table_name='ticket_types' AND column_name='sla_exempt') ORDER BY 1"
echo "--- new tables ---"
$PSQL "SELECT to_regclass('public.msp_contracts')||' | '||to_regclass('public.msp_contract_sla_bindings')||' | '||to_regclass('public.sla_definition_grants')"

echo "===== [D4] RLS 状态与策略 ====="
$PSQL "SELECT relname||'|rls='||relrowsecurity FROM pg_class WHERE relname IN ('sla_definition_grants','sla_definitions','msp_contracts','msp_contract_sla_bindings') ORDER BY relname"
$PSQL "SELECT polname FROM pg_policy WHERE polname IN ('sla_grant_provider_all','sla_grant_customer_read','sla_definition_customer_read','msp_contract_provider_all','msp_contract_customer_read','msp_contract_binding_provider_all','msp_contract_binding_customer_read') ORDER BY polname"

echo "===== [D5] 容器与路由（HTTP） ====="
docker inspect -f '{{.Config.Image}}' itsm-backend-demo itsm-worker-demo itsm-frontend-demo 2>/dev/null
curl -s -o /dev/null -w "demo-http=%{http_code}\n" http://127.0.0.1:8089/
curl -s -o /dev/null -w "readyz=%{http_code}\n" http://127.0.0.1:8091/api/v1/readyz
curl -s -o /dev/null -w "settle-new=%{http_code}\n" -X PUT http://127.0.0.1:8091/api/v1/sla/violations/1/settle
curl -s -o /dev/null -w "contracts-new=%{http_code}\n" http://127.0.0.1:8091/api/v1/sla/contracts
curl -s -o /dev/null -w "grants-new=%{http_code}\n" http://127.0.0.1:8091/api/v1/sla/grants

echo "===== [D6] A/B 特征对比 ====="
echo "--- frontend asset token sla-contracts ---"
docker run --rm "itsm-frontend:$OLD" sh -c "grep -rl sla-contracts /usr/share/nginx/html/assets/ 2>/dev/null | wc -l" 2>/dev/null || echo "old-image-grep-N/A"
docker run --rm "itsm-frontend:$NEW" sh -c "grep -rl sla-contracts /usr/share/nginx/html/assets/ 2>/dev/null | wc -l" 2>/dev/null || echo "new-image-grep-N/A"
echo "--- backend binary token (soft) ---"
docker run --rm "itsm-backend:$OLD" sh -c "grep -c 'violations/:id/settle' /app/main 2>/dev/null" 2>/dev/null || echo "old-bin-grep-N/A"
docker run --rm "itsm-backend:$NEW" sh -c "grep -c 'violations/:id/settle' /app/main 2>/dev/null" 2>/dev/null || echo "new-bin-grep-N/A"
echo "D-STATUS: DONE"
