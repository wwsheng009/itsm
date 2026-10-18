#!/usr/bin/env bash
# BE-8 验证：未绑定调用方走租户 provider（工单总结/分诊/影响解释）+ 项目 AI 回归
set -u
cd /home/n9e/itsm/repo || exit 1
BASE="http://127.0.0.1:8089/api/v1"
JAR=/tmp/be8_verify_cookies.txt
rm -f "$JAR"

echo "===== [0] version ====="
curl -s --max-time 10 "$BASE/version"; echo

PW=$(grep -E '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)
curl -s -o /dev/null -c "$JAR" -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}"
csrf() {
  curl -s -b "$JAR" -c "$JAR" "$BASE/csrf-token" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("csrf_token",""))'
}
post() {
  local path="$1" body="$2"
  local t; t=$(csrf)
  curl -s --max-time 90 -b "$JAR" -H "X-CSRF-Token: $t" -H 'Content-Type: application/json' \
    -X POST "$BASE$path" -d "$body"
}

TID=$(docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -P pager=off -t -A -c "SELECT id FROM tickets ORDER BY id DESC LIMIT 1" | tr -d '\r')
echo "===== [1] ticket summary (ticket=$TID) ====="
curl -s --max-time 90 -b "$JAR" "$BASE/ai/tickets/$TID/summary" | head -c 900; echo

echo "===== [2] triage ====="
post "/ai/triage" '{"title":"VPN 无法连接","description":"用户反馈 VPN 连接超时，影响华东区 30 人办公"}' | head -c 900; echo

echo "===== [3] project AI summary（回归） ====="
post "/projects/1/ai/summary" '{}' | python3 -c '
import sys,json
d=json.load(sys.stdin).get("data",{})
print("generatedBy=",d.get("generatedBy"),"degraded=",d.get("degraded"),"model=",d.get("model"),"chars=",len(d.get("summary") or ""))
'

CIID=$(docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -P pager=off -t -A -c "SELECT id FROM configuration_items ORDER BY id LIMIT 1" | tr -d '\r')
echo "===== [4] tool get_ci_impact (ci=$CIID, raw) ====="
post "/agent/tools/execute" "{\"name\":\"get_ci_impact\",\"args\":{\"ci_id\":$CIID,\"hops\":3}}" | head -c 1400; echo

echo "===== [5] ai_llm_calls last 10 ====="
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -P pager=off -c \
"SELECT id, provider, COALESCE(provider_key,'<static>') AS pkey, model, success, latency_ms, to_char(created_at,'MM-DD HH24:MI:SS') AS ts FROM ai_llm_calls ORDER BY id DESC LIMIT 10"

echo "===== [6] degraded/fallback logs since 10m ====="
docker logs itsm-backend-demo --since 10m 2>&1 | grep -aE "degraded|falling back|LLM failed|AI 摘要失败|AI分诊失败" | tail -12 || echo "(none)"
echo "BE8-VERIFY-DONE"
