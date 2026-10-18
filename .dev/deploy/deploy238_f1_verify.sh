#!/usr/bin/env bash
# F1 验证：/ai/triage 不再吃 sidecar 静默降级 → 回落网关（真实 LLM + ai_llm_calls）
set -u
cd /home/n9e/itsm/repo || exit 1
BASE="http://127.0.0.1:8089/api/v1"
JAR=/tmp/f1_verify_cookies.txt
rm -f "$JAR"
PY='import sys,json;d=json.load(sys.stdin);print(json.dumps(d,ensure_ascii=True))'

echo "===== [0] version ====="
curl -s --max-time 10 "$BASE/version"; echo

PW=$(grep -E '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)
curl -s -o /dev/null -c "$JAR" -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}"
csrf() {
  curl -s -b "$JAR" -c "$JAR" "$BASE/csrf-token" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("csrf_token",""))'
}
post() {
  local t; t=$(csrf)
  curl -s --max-time 90 -b "$JAR" -H "X-CSRF-Token: $t" -H 'Content-Type: application/json' \
    -X POST "$BASE$1" -d "$2"
}

BEFORE=$(docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -P pager=off -t -A -c "SELECT COALESCE(MAX(id),0) FROM ai_llm_calls" | tr -d '\r')
echo "===== [1] max_ai_llm_calls_id_before=$BEFORE ====="

echo "===== [2] POST /ai/triage #1（VPN） ====="
post "/ai/triage" '{"title":"VPN 无法连接","description":"用户反馈 VPN 连接超时，影响华东区 30 人办公"}' | python3 -c "$PY"

echo "===== [3] ai_llm_calls 新行 ====="
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -P pager=off -c \
"SELECT id, provider, COALESCE(provider_key,'<static>') AS pkey, model, success, latency_ms, to_char(created_at,'MM-DD HH24:MI:SS') AS ts FROM ai_llm_calls WHERE id > $BEFORE ORDER BY id DESC LIMIT 10"

echo "===== [4] backend 日志：降级回落标记 ====="
docker logs itsm-backend-demo --since 3m 2>&1 | grep -aE "degraded to keyword fallback|Guidance-constrained classification|LLM classification failed" | tail -6 || echo "(none)"

echo "===== [5] POST /ai/triage #2（Outlook） ====="
post "/ai/triage" '{"title":"邮箱无法收发邮件","description":"Outlook 报错 0x8004010F，多个用户反馈"}' | python3 -c "$PY"

echo -n "new_calls_total="
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -P pager=off -t -A -c "SELECT count(*) FROM ai_llm_calls WHERE id > $BEFORE" | tr -d '\r'
echo "F1-VERIFY-DONE"
