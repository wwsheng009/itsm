#!/usr/bin/env bash
# 238 demo 增量修复 D 步：健康等待 → 修复回归探针 → 结构复核（幂等）
set -u
NEW="${NEW:?NEW required}"
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
  echo "--- backend logs tail ---"; docker logs --tail 12 itsm-backend-demo 2>&1 | tail -12
  echo "D-STATUS: WAIT"; exit 0
fi

echo "===== [D2] 容器镜像 ====="
for c in itsm-backend-demo itsm-worker-demo itsm-frontend-demo; do
  docker inspect -f '{{.Name}}|{{.Config.Image}}|{{.State.Status}}' "$c" 2>/dev/null
done

echo "===== [D3] 修复回归探针（无 token，期望全部 401；MSP 子组仍可用链路） ====="
for p in "/api/v1/sla/contracts" "/api/v1/sla/grants" "/api/v1/sla/settlement-report?customerTenantId=3&from=2026-09-01&to=2026-12-31" "/api/v1/sla/definitions/provided"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:8091$p")
  echo "probe $p -> $code"
done
curl -s -o /dev/null -w "demo-http=%{http_code}\n" http://127.0.0.1:8089/
curl -s -o /dev/null -w "readyz=%{http_code}\n" http://127.0.0.1:8091/api/v1/readyz

echo "===== [D4] 迁移/结构回执（幂等复核） ====="
$PSQL "SELECT count(*) FROM schema_migrations WHERE version LIKE '20261014%'"
$PSQL "SELECT to_regclass('public.ticket_type_distributions')||' | '||to_regclass('public.sla_definition_grants')||' | '||to_regclass('public.msp_contracts')"

# 卫生：恢复 .env.demo 属主（写入工具链以 root 运行）
chown n9e:n9e /home/n9e/itsm/repo/.env.demo 2>/dev/null || true
echo "D-STATUS: DONE"
