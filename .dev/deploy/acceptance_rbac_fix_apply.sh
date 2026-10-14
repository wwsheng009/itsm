#!/usr/bin/env bash
# 238 demo RBAC 修复 APPLY（验收准备）：
#  R2b 官方权限同步 apply → R3b provider_tenant_admin 回填 apply → R4 manager 复合角色边
#  → R5 重启 backend 清缓存 → R6 状态复核
set -u
R=/home/n9e/itsm/repo
DB=itsm_demo
cd "$R/itsm-backend" || exit 1

echo "===== [R2b] 官方权限同步 APPLY ====="
export DB_HOST=127.0.0.1 DB_PORT=5433 DB_NAME="$DB" DB_SSLMODE=disable
export DB_USER="$(grep -E '^DB_USER=' "$R/.env.demo" | cut -d= -f2-)"
export DB_PASSWORD="$(grep -E '^DB_PASSWORD=' "$R/.env.demo" | cut -d= -f2-)"
export RLS_MODE=off
/tmp/syncrp_linux -tenant-id 2 -apply

echo "===== [R3b] provider_tenant_admin 回填 APPLY ====="
/tmp/backfill_pa_linux -tenant-id 2 -dry-run=false

echo "===== [R4] manager 复合角色边（user_roles 2->46，幂等） ====="
docker exec -i itsm-postgres-prod psql -U itsm -d "$DB" -v ON_ERROR_STOP=1 <<'SQL'
INSERT INTO user_roles (user_id, role_id)
SELECT 2, 46 WHERE NOT EXISTS (SELECT 1 FROM user_roles WHERE user_id=2 AND role_id=46);
SELECT user_id, role_id FROM user_roles WHERE user_id=2;
SQL

echo "===== [R5] 重启 backend 清缓存 ====="
docker restart itsm-backend-demo >/dev/null 2>&1
for i in $(seq 1 20); do
  BE=$(docker inspect -f '{{.State.Health.Status}}' itsm-backend-demo 2>/dev/null || echo missing)
  [ "$BE" = "healthy" ] && break
  sleep 6
done
docker inspect -f '{{.State.Health.Status}}' itsm-backend-demo

echo "===== [R6] 状态复核 ====="
docker exec itsm-postgres-prod psql -U itsm -d "$DB" -c "SELECT r.code, count(*) AS cnt FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE r.tenant_id=2 AND r.code IN ('msp_manager','msp_admin','provider_tenant_admin') AND p.code IN ('sla:contract:read','sla:contract:write','sla:grant:read','sla:grant:write','sla:settlement:read','sla:violation:settle') GROUP BY 1 ORDER BY 1"
docker exec itsm-postgres-prod psql -U itsm -d "$DB" -c "SELECT p.code FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE rp.role_id=46 AND p.code LIKE 'sla:%' ORDER BY p.code"
chown n9e:n9e "$R/.env.demo" 2>/dev/null || true
echo "R-STATUS: DONE"
