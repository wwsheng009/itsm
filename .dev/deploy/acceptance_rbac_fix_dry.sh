#!/usr/bin/env bash
# 238 demo RBAC 修复（验收准备，DRY-RUN 版）：
#  R1 目录克隆（tenant1 -> tenant2 的 6 个 SLA 合同化新码）
#  R2 官方权限同步 dry-run；R3 provider_tenant_admin 回填 dry-run
#  （apply 由第二步脚本执行，需人工确认 dry-run 输出）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo NO_REPO; exit 1; }
DB=itsm_demo
PSQL="docker exec -i itsm-postgres-prod psql -U itsm -d $DB -v ON_ERROR_STOP=1"

echo "===== [R1] 目录克隆（tenant1 -> tenant2，幂等） ====="
$PSQL <<'SQL'
INSERT INTO permissions (code, name, description, resource, action, tenant_id, created_at, updated_at)
SELECT code, name, description, resource, action, 2, now(), now()
FROM permissions p1
WHERE p1.tenant_id = 1
  AND p1.code IN ('sla:grant:read','sla:grant:write','sla:contract:read','sla:contract:write','sla:settlement:read','sla:violation:settle')
  AND NOT EXISTS (SELECT 1 FROM permissions p2 WHERE p2.tenant_id = 2 AND p2.code = p1.code);
SELECT count(*) AS t2_sla_codes FROM permissions WHERE tenant_id=2 AND code LIKE 'sla:%';
SQL

echo "===== [R2] 官方权限同步 DRY-RUN ====="
cd "$R/itsm-backend" || exit 1
export DB_HOST=127.0.0.1 DB_PORT=5433 DB_NAME="$DB" DB_SSLMODE=disable
export DB_USER="$(grep -E '^DB_USER=' "$R/.env.demo" | cut -d= -f2-)"
export DB_PASSWORD="$(grep -E '^DB_PASSWORD=' "$R/.env.demo" | cut -d= -f2-)"
export RLS_MODE=off
chmod +x /tmp/syncrp_linux /tmp/backfill_pa_linux
/tmp/syncrp_linux -tenant-id 2

echo "===== [R3] provider_tenant_admin 回填 DRY-RUN ====="
/tmp/backfill_pa_linux -tenant-id 2

echo "===== [R5] 当前状态 ====="
docker exec itsm-postgres-prod psql -U itsm -d "$DB" -c "SELECT r.code, count(*) AS cnt FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE r.tenant_id=2 AND p.code IN ('sla:contract:read','sla:contract:write','sla:grant:read','sla:grant:write','sla:settlement:read','sla:violation:settle') GROUP BY 1 ORDER BY 1"
docker exec itsm-postgres-prod psql -U itsm -d "$DB" -tAc "SELECT username||' bootstrap='||is_bootstrap_admin FROM users WHERE id=2"
echo "R-DRY-STATUS: DONE"
