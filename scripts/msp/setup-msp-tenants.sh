#!/usr/bin/env bash
# =============================================================================
# MSP 多租户（单一服务商 + 多客户）初始化脚本
#
# 目标拓扑（DEPLOYMENT_MODE=saas_msp）：
#   MSP001 (msp_provider)            <- 服务商租户
#     ├── MSPCUSTA (msp_customer)    <- 客户 A
#     └── MSPCUSTB (msp_customer)    <- 客户 B
#
# 该脚本在部署主机上执行（需 sudo 免密或提供 SUDO_PASS），负责：
#   0. 以 admin 登录获取 Bearer token；
#   1. 回填 default 租户内置审批组（provision 校验要求 groups>0，存量库缺失）；
#   2. 校正 roles 等表的 id 序列（序列落后会导致 provision duplicate key）；
#   3. 幂等创建提供商/客户租户（含 parentTenantId / mspProviderId 绑定）；
#   4. 调用 provision_tenant 二进制为目标租户克隆模板（roles/permissions/menus/...）；
#   5. 为提供商租户的 msp_* 角色补齐 role_permissions（DB 权威模式下 fail-closed）；
#   6. 创建各租户首个用户与 MSP 技术员（SQL + pgcrypto，原因见 docs/multi-tenant/07-known-gaps.md G1/G2/G3）；
#   7. 由 mspadmin 调用 MSP 接口建立 msp_allocations 分配关系；
#   8. 打印隔离性与能力探针结果（MSP 员工跨客户 / 客户用户不可访问 MSP 接口）。
#
# 依赖：curl、jq、docker（sudo）、provision_tenant 二进制（见 PROVISION_BIN_HOST）。
# 用法：./setup-msp-tenants.sh
# =============================================================================
set -euo pipefail

BASE="${BASE:-http://127.0.0.1:8088}"
JAR="${JAR:-/tmp/msp_cookies.txt}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASS="${ADMIN_PASS:-passw0rd}"
SUDO_PASS="${SUDO_PASS:-passw0rd}"
BACKEND_CONTAINER="${BACKEND_CONTAINER:-itsm-backend-prod}"
PG_CONTAINER="${PG_CONTAINER:-itsm-postgres-prod}"
DB_NAME="${DB_NAME:-itsm_prod}"
DB_USER="${DB_USER:-itsm}"
PROVISION_BIN_HOST="${PROVISION_BIN_HOST:-/tmp/provision_tenant_linux_amd64}"
MSP_STAFF_PASSWORD="${MSP_STAFF_PASSWORD:-Msp@2026Staff!}"
CUSTOMER_PASSWORD="${CUSTOMER_PASSWORD:-Cust@2026User!}"
# 设为 1 时即使目标租户已有模板数据也强制重跑 provision_tenant（用于验证二进制可用性；
# ProvisionTenant 本身幂等，不会产生重复数据）。
FORCE_PROVISION="${FORCE_PROVISION:-0}"

PROVIDER_CODE="MSP001"
PROVIDER_NAME="MSP Provider"
CUSTOMER_A_CODE="MSPCUSTA"
CUSTOMER_A_NAME="Customer A"
CUSTOMER_B_CODE="MSPCUSTB"
CUSTOMER_B_NAME="Customer B"

say() { printf '\n=== %s ===\n' "$*"; }
sdo() { echo "$SUDO_PASS" | sudo -S "$@"; }
psql_q() { sdo docker exec "$PG_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc "$1" | tr -d '\r'; }
psql_c() { sdo docker exec "$PG_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -c "$1" | tr -d '\r'; }
# psql -tAc 的多行结果里取最后一个纯数字（用于 count/returning id）
last_number() { sed -n 's/^\([0-9][0-9]*\)$/\1/p' | tail -n 1; }

# ---------------------------------------------------------------------------
# 0. 登录（Bearer；CSRF 中间件对 Bearer 请求直接放行）
# ---------------------------------------------------------------------------
login() { # user pass jar -> token
  local user=$1 pass=$2 jar=$3
  rm -f "$jar"
  curl -sf -c "$jar" -X POST "$BASE/api/v1/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$user\",\"password\":\"$pass\"}" -o /dev/null
  grep -w access_token "$jar" | awk '{print $NF}'
}

TOKEN=""
api() { # method path [json-body]
  local method=$1 path=$2 body=${3:-}
  if [ -n "$body" ]; then
    curl -s -X "$method" -H "Authorization: Bearer $TOKEN" \
      -H 'Content-Type: application/json' "$BASE$path" -d "$body"
  else
    curl -s -X "$method" -H "Authorization: Bearer $TOKEN" "$BASE$path"
  fi
}

say "0. admin 登录"
TOKEN=$(login "$ADMIN_USER" "$ADMIN_PASS" "$JAR")
[ -n "$TOKEN" ] || { echo "登录失败：无法获取 access_token"; exit 1; }
echo "token_len=${#TOKEN}"

# ---------------------------------------------------------------------------
# 1. 回填 default 租户内置审批组（与 pkg/seeder/seeder.go BuiltinGroups 对齐）
#    provision_tenant 的 readiness 校验要求目标租户 groups>0，
#    而 2026-09-15 新增的组种子未在存量 default 租户生效，需先回填。
# ---------------------------------------------------------------------------
say "1. 回填 default 租户内置审批组"
psql_q "insert into groups (name, description, tenant_id, created_at, updated_at)
        select v.name, v.description, 1, now(), now()
        from (values
          ('approvers-l1','Approval group L1 (l1_support / agent)'),
          ('approvers-l2','Approval group L2 (l2_support / technician)'),
          ('approvers-l3','Approval group L3 (l3_expert / it_admin)'),
          ('approvers-managers','Approval group managers'),
          ('approvers-security','Approval group security'),
          ('approvers-change','Approval group change committee')
        ) as v(name, description)
        where not exists (
          select 1 from groups g where g.tenant_id = 1 and g.name = v.name
        );" >/dev/null
psql_q "select count(*) from groups where tenant_id = 1"

# ---------------------------------------------------------------------------
# 2. 修正 id 序列（存量库常见：序列落后于 max(id) -> duplicate key）
# ---------------------------------------------------------------------------
say "2. 校正序列"
for tbl in roles permissions menus groups role_permissions sla_definitions ci_types \
           approval_workflows system_configs; do
  seq=$(psql_q "select pg_get_serial_sequence('$tbl','id')")
  [ -n "$seq" ] || continue
  psql_q "select setval('$seq', greatest((select coalesce(max(id),0) from $tbl), 1))" >/dev/null
  printf '%s -> %s\n' "$tbl" "$(psql_q "select last_value from $seq")"
done

# ---------------------------------------------------------------------------
# 3. 幂等创建租户
# ---------------------------------------------------------------------------
tenant_id_by_code() { psql_q "select id from tenants where code='$1'"; }

ensure_tenant() { # code name type [parent] [provider]
  local code=$1 name=$2 type=$3 parent=${4:-} provider=${5:-}
  local id payload
  id=$(tenant_id_by_code "$code")
  if [ -z "$id" ]; then
    payload=$(jq -nc --arg n "$name" --arg c "$code" --arg t "$type" '{name:$n,code:$c,type:$t}')
    [ -n "$parent" ] && payload=$(jq -c --argjson p "$parent" '. + {parentTenantId:$p}' <<<"$payload")
    [ -n "$provider" ] && payload=$(jq -c --argjson p "$provider" '. + {mspProviderId:$p}' <<<"$payload")
    api POST /api/v1/tenants "$payload" | jq -c '{code,message,id:.data.id}' >&2
    id=$(tenant_id_by_code "$code")
  else
    echo "{\"code\":0,\"message\":\"exists\",\"id\":$id}" >&2
  fi
  [ -n "$id" ] || { echo "租户 $code 创建失败" >&2; exit 1; }
  printf '%s' "$id"
}

say "3. 创建 MSP 提供商与客户租户"
PROVIDER_ID=$(ensure_tenant "$PROVIDER_CODE" "$PROVIDER_NAME" msp_provider)
CUSTA_ID=$(ensure_tenant "$CUSTOMER_A_CODE" "$CUSTOMER_A_NAME" msp_customer "$PROVIDER_ID" "$PROVIDER_ID")
CUSTB_ID=$(ensure_tenant "$CUSTOMER_B_CODE" "$CUSTOMER_B_NAME" msp_customer "$PROVIDER_ID" "$PROVIDER_ID")
echo "provider=$PROVIDER_ID customerA=$CUSTA_ID customerB=$CUSTB_ID"
psql_c "select id,code,type,coalesce(parent_tenant_id,0) parent,coalesce(msp_provider_id,0) provider from tenants order by id"

# ---------------------------------------------------------------------------
# 4. 目标租户模板供给（provision_tenant，幂等）
# ---------------------------------------------------------------------------
provision_tenant() { # tenant_id
  local tid=$1
  local existing
  existing=$(psql_q "select count(*) from roles where tenant_id=$tid" | last_number)
  if [ "${existing:-0}" -gt 0 ] && [ "$FORCE_PROVISION" != "1" ]; then
    echo "tenant $tid 已完成模板供给（roles=$existing），跳过" >&2
    return 0
  fi
  if [ ! -f "$PROVISION_BIN_HOST" ]; then
    echo "缺少 $PROVISION_BIN_HOST，无法供给租户 $tid；构建方法见 docs/multi-tenant/ 部署文档" >&2
    exit 1
  fi
  sdo docker cp "$PROVISION_BIN_HOST" "$BACKEND_CONTAINER:/tmp/provision_tenant" >/dev/null
  sdo docker exec -w /app "$BACKEND_CONTAINER" /tmp/provision_tenant \
    -tenant-id "$tid" -template-version 1.0.0 2>&1 | tail -n 2
}

say "4. 供给租户模板"
for tid in "$PROVIDER_ID" "$CUSTA_ID" "$CUSTB_ID"; do
  echo "--- tenant $tid ---"
  provision_tenant "$tid"
done
psql_c "select t.id, t.code,
        (select count(*) from roles r where r.tenant_id=t.id) roles,
        (select count(*) from permissions p where p.tenant_id=t.id) perms,
        (select count(*) from role_permissions rp where rp.tenant_id=t.id) role_perms,
        (select count(*) from menus m where m.tenant_id=t.id) menus,
        (select count(*) from groups g where g.tenant_id=t.id) groups
        from tenants t where t.id in (1,$PROVIDER_ID,$CUSTA_ID,$CUSTB_ID) order by t.id"

# ---------------------------------------------------------------------------
# 5. 提供商租户 msp_* 角色授权（DB 权威模式：角色行存在即 fail-closed，
#    必须显式写入 role_permissions；矩阵对齐 middleware/rbac.go RolePermissions）
# ---------------------------------------------------------------------------
seed_role_perms() { # tenant_id role_code filter_sql
  local tid=$1 role=$2 filter=$3
  psql_q "insert into role_permissions (role_id, permission_id, tenant_id)
          select r.id, p.id, r.tenant_id
          from roles r join permissions p on p.tenant_id = r.tenant_id
          where r.tenant_id = $tid and r.code = '$role' and ($filter)
            and not exists (
              select 1 from role_permissions rp2
              where rp2.role_id = r.id and rp2.permission_id = p.id and rp2.tenant_id = r.tenant_id
            );" >/dev/null
  psql_q "select count(*) from role_permissions rp join roles r on r.id = rp.role_id
          where rp.tenant_id = $tid and r.code = '$role'"
}

say "5. 提供商租户 MSP 角色授权"
F_ALL="p.resource like 'msp%'"
F_READ="p.resource like 'msp%' and p.action = 'read'"
F_TECH="(p.resource = 'msp' and p.action = 'read')
        or (p.resource = 'msp_customer' and p.action = 'read')
        or (p.resource = 'msp_ticket')
        or (p.resource = 'msp_allocation' and p.action = 'read')
        or (p.resource = 'msp_report' and p.action = 'read')"
F_SPECIALIST="(p.resource = 'msp' and p.action = 'read')
        or (p.resource = 'msp_customer')
        or (p.resource = 'msp_ticket')
        or (p.resource = 'msp_allocation' and p.action = 'read')
        or (p.resource = 'msp_report' and p.action = 'read')"
for pair in "msp_manager|$F_ALL" "msp_tech|$F_TECH" "msp_viewer|$F_READ" "msp_specialist|$F_SPECIALIST"; do
  role=${pair%%|*}; filter=${pair#*|}
  printf '%s -> %s\n' "$role" "$(seed_role_perms "$PROVIDER_ID" "$role" "$filter")"
done

# ---------------------------------------------------------------------------
# 6. 用户创建
#
#    部署实测结论：新建租户的“首个用户”无法经 HTTP API 创建——
#      a) tenant guard 禁止跨租户写入（super_admin 亦被拦截）；
#      b) bootstrap 流程固定创建 username=admin / email=admin@example.com，
#         而 users.username / users.email 为全局唯一，第二个租户必然冲突。
#    故本脚本所有租户内用户均由 SQL 直接写入（pgcrypto bcrypt 与 Go bcrypt 兼容）；
#    注意 MSP 管理员同样无法经 API 建号（有效角色被解析为 msp_manager，见 07 文档 G3）。
# ---------------------------------------------------------------------------
psql_q "CREATE EXTENSION IF NOT EXISTS pgcrypto" >/dev/null

first_user() { # tenant_id username name email password role msp_role role_codes(逗号分隔)
  local tid=$1 user=$2 name=$3 email=$4 pass=$5 role=$6 msp=$7 rcodes=$8
  local uid rid code
  uid=$(psql_q "select id from users where username='$user'" | last_number)
  if [ -z "$uid" ]; then
    uid=$(psql_q "insert into users (username,email,name,role,password_hash,active,created_at,updated_at,tenant_id,msp_role,is_bootstrap_admin)
            values ('$user','$email','$name','$role',crypt('$pass', gen_salt('bf',10)),true,now(),now(),$tid,'$msp',false)
            returning id" | last_number)
    echo "{\"username\":\"$user\",\"id\":$uid,\"via\":\"sql\"}" >&2
  else
    echo "{\"username\":\"$user\",\"id\":$uid,\"via\":\"exists\"}" >&2
  fi
  [ -n "$uid" ] || { echo "用户 $user 创建失败" >&2; exit 1; }
  # 角色边（幂等补齐；RBAC 权限按 M2M 并集解析）
  local IFS=','
  for code in $rcodes; do
    rid=$(psql_q "select id from roles where tenant_id=$tid and code='$code'" | last_number)
    if [ -n "$rid" ]; then
      psql_q "insert into user_roles (user_id, role_id) values ($uid, $rid)
              on conflict (user_id, role_id) do nothing" >/dev/null
    fi
  done
  printf '%s' "$uid"
}

say "6. 创建各租户首个用户（SQL/pgcrypto）"
MSP_ADMIN_ID=$(first_user "$PROVIDER_ID" mspadmin "MSP Admin" mspadmin@msp.local "$MSP_STAFF_PASSWORD" admin provider_admin admin,msp_manager)
CUSTA_ADMIN_ID=$(first_user "$CUSTA_ID" custa_admin "Customer A Admin" custa-admin@cust-a.local "$CUSTOMER_PASSWORD" admin customer_user admin)
CUSTB_USER_ID=$(first_user "$CUSTB_ID" custb_user "Customer B User" custb-user@cust-b.local "$CUSTOMER_PASSWORD" end_user customer_user end_user)

say "6b. 创建 MSP 技术员 mspagent"
# 实测：mspadmin 登录后有效角色被解析为 msp_manager（roleRank=0，仅 10 条 msp:* 权限），
# 既无 user:write 权限，也过不了 handler 的“不得分配高于自身角色”校验，故同租户 API
# 建号在当前版本不可行（产品缺口，见部署文档）；此处与首个用户同样走 SQL。
MSP_AGENT_ID=$(first_user "$PROVIDER_ID" mspagent "MSP Agent" mspagent@msp.local "$MSP_STAFF_PASSWORD" agent provider_agent msp_tech)

# mspadmin 的 MSP token 供第 7/8 节调用 MSP 接口（其权限含 msp_allocation:*）。
ADMIN_TOKEN="$TOKEN"
MSP_ADMIN_TOKEN=$(login mspadmin "$MSP_STAFF_PASSWORD" /tmp/msp_admin.jar)
TOKEN="$ADMIN_TOKEN"
echo "mspadmin=$MSP_ADMIN_ID mspagent=$MSP_AGENT_ID custa_admin=$CUSTA_ADMIN_ID custb_user=$CUSTB_USER_ID"

# ---------------------------------------------------------------------------
# 7. MSP 分配（多客户覆盖；mspagent 只分到客户 A，用于验证分配范围）
# ---------------------------------------------------------------------------
say "7. 建立 msp_allocations（由 mspadmin 调用 MSP 接口）"
create_allocation() { # msp_user_id customer_tenant_id role
  local payload exists
  exists=$(psql_q "select count(*) from msp_allocations
                   where msp_user_id=$1 and customer_tenant_id=$2 and deassigned_at is null" | last_number)
  if [ "${exists:-0}" -gt 0 ]; then
    echo "{\"allocation\":\"exists\",\"mspUserId\":$1,\"customerTenantId\":$2}"
    return 0
  fi
  payload=$(jq -nc --argjson u "$1" --argjson c "$2" --arg r "$3" \
    '{mspUserId:$u,customerTenantId:$c,role:$r}')
  api POST /api/v1/msp/allocations "$payload" | jq -c '{code,message,id:.data.id}'
}
TOKEN="$MSP_ADMIN_TOKEN"
create_allocation "$MSP_ADMIN_ID" "$CUSTA_ID" primary
create_allocation "$MSP_ADMIN_ID" "$CUSTB_ID" primary
create_allocation "$MSP_AGENT_ID" "$CUSTA_ID" primary
TOKEN="$ADMIN_TOKEN"
psql_c "select a.id, u.username, a.customer_tenant_id, t.code, a.role, a.assigned_at
        from msp_allocations a join users u on u.id = a.msp_user_id
        join tenants t on t.id = a.customer_tenant_id
        where a.deassigned_at is null order by a.id"

# ---------------------------------------------------------------------------
# 8. 验证（隔离性 / MSP 能力）
# ---------------------------------------------------------------------------
say "8. 验证"
probe() { # jar_token method path
  curl -s -o /tmp/msp_probe_body.json -w '%{http_code}' -X "$2" \
    -H "Authorization: Bearer $1" "$BASE$3"
}

MSP_ADMIN_TOKEN=$(login mspadmin "$MSP_STAFF_PASSWORD" /tmp/msp_admin.jar)
MSP_AGENT_TOKEN=$(login mspagent "$MSP_STAFF_PASSWORD" /tmp/msp_agent.jar)
CUSTA_TOKEN=$(login custa_admin "$CUSTOMER_PASSWORD" /tmp/msp_custa.jar)

printf 'mspadmin /msp/status      -> %s\n' "$(probe "$MSP_ADMIN_TOKEN" GET /api/v1/msp/status)"
printf 'mspadmin /msp/context     -> %s\n' "$(probe "$MSP_ADMIN_TOKEN" GET /api/v1/msp/context)"
printf 'mspadmin /msp/customers   -> %s\n' "$(probe "$MSP_ADMIN_TOKEN" GET /api/v1/msp/customers)"
printf 'mspagent /msp/allocations -> %s\n' "$(probe "$MSP_AGENT_TOKEN" GET /api/v1/msp/allocations)"
printf 'custa_admin /msp/allocations (expect 403) -> %s\n' "$(probe "$CUSTA_TOKEN" GET /api/v1/msp/allocations)"
# 隔离回归（06 文档 §2 用例 2/3）：
#   用例 2 实测为 200——JWT 已解析出租户实体时 X-Tenant-Code 被忽略（tenant.go:60-83 仅作补充来源，
#   因此不会进入 146-156 行的“租户不匹配”拒绝分支），数据仍限客户 A；冲突头不报错属缺口 G9。
printf 'custa_admin + X-Tenant-Code: MSPCUSTB (实测 200，Header 被忽略) -> %s\n' \
  "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $CUSTA_TOKEN" \
     -H 'X-Tenant-Code: MSPCUSTB' "$BASE/api/v1/users")"
printf 'mspagent + X-Customer-Tenant-ID: 5 (expect 403) -> %s\n' \
  "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $MSP_AGENT_TOKEN" \
     -H 'X-Customer-Tenant-ID: 5' "$BASE/api/v1/tickets")"

echo "--- mspagent 可见客户（应为客户 A） ---"
curl -s -H "Authorization: Bearer $MSP_AGENT_TOKEN" "$BASE/api/v1/msp/allocations" | jq -c '.data'
echo "--- mspadmin 可见客户（应为 A + B） ---"
curl -s -H "Authorization: Bearer $MSP_ADMIN_TOKEN" "$BASE/api/v1/msp/customers" | jq -c '.data'
echo "--- 客户 A admin 用户列表（应仅本租户） ---"
curl -s -H "Authorization: Bearer $CUSTA_TOKEN" "$BASE/api/v1/users?page=1&pageSize=20" | jq -c '{total:.data.pagination.total,usernames:[.data.users[].username]}'

say "完成"
