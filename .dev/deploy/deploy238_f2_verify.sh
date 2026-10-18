#!/usr/bin/env bash
# 238 demo 运行验证：F2 应用内 LLM 配置请求级下发 guidance sidecar（c05cc49e）
# 断言（租户 1 = admin 账号，唯一有 llm_provider_configs 的租户）：
#   A) sidecar 日志出现 "request-scoped LLM target"（tenant=1 protocol/model/url 来自 DB）
#   B) /ai/triage 返回真实 LLM reasoning（非 "keyword heuristic"）
#   C) ai_llm_calls delta=0（sidecar 直连上游成功，未回落网关）——与 F1 的行为差异
#   D) sidecar 自身 LLM_API_KEY 仍为空（结果来自请求级下发，而非 sidecar env）
set -u
cd /home/n9e/itsm/repo || exit 1
BASE=http://127.0.0.1:8089/api/v1
JAR=/tmp/f2_verify_cookies.txt
rm -f "$JAR"
PSQL() { docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -P pager=off "$@"; }
PY='import sys,json;d=json.load(sys.stdin);print(json.dumps(d,ensure_ascii=True))'

echo "===== [V0] 版本 / 账号租户 / 租户 1 默认实例 ====="
curl -s --max-time 10 "$BASE/version"; echo
PSQL -c "select id, username, tenant_id from users where username = 'admin'"
PSQL -c "select id, tenant_id, name, protocol, variant, model, endpoint, endpoint_full_path, enabled, is_default, status from llm_provider_configs where tenant_id = 1 and is_default and enabled and deleted_at is null"

echo "===== [V1] sidecar 自身 env（证明 LLM_API_KEY 未配置） ====="
docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' itsm-ai-service-demo \
  | grep -E '^(LLM_API_KEY|LLM_BASE_URL|LLM_MODEL|LLM_PROVIDER)=' \
  | sed 's/^\(LLM_API_KEY=.\{0,6\}\).*/\1***/'

PW=$(grep -E '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)
curl -s -o /dev/null -c "$JAR" -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}"
csrf() { curl -s -b "$JAR" -c "$JAR" "$BASE/csrf-token" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("csrf_token",""))'; }
post() { local t; t=$(csrf); curl -s --max-time 120 -b "$JAR" -H "X-CSRF-Token: $t" -H 'Content-Type: application/json' -X POST "$BASE$1" -d "$2"; }

BEFORE=$(PSQL -t -A -c "SELECT COALESCE(MAX(id),0) FROM ai_llm_calls" | tr -d '\r')
echo "===== [V2] max_ai_llm_calls_id_before=$BEFORE ====="

echo "===== [V3] POST /ai/triage #1（VPN，tenant 1） ====="
post "/ai/triage" '{"title":"VPN 无法连接","description":"用户反馈 VPN 连接超时，影响华东区 30 人办公"}' | tee /tmp/f2_triage1.json | python3 -c "$PY"

echo "===== [V4] POST /ai/triage #2（Outlook，tenant 1） ====="
post "/ai/triage" '{"title":"邮箱无法收发邮件","description":"Outlook 报错 0x8004010F，多个用户反馈"}' | tee /tmp/f2_triage2.json | python3 -c "$PY"

echo "===== [V5] ai_llm_calls（F2 期望 delta=0：sidecar 直连成功，未走网关） ====="
PSQL -c "SELECT id, provider, COALESCE(provider_key,'<static>') AS pkey, model, success, latency_ms, to_char(created_at,'MM-DD HH24:MI:SS') AS ts FROM ai_llm_calls WHERE id > $BEFORE ORDER BY id"
echo -n "new_calls_total="; PSQL -t -A -c "SELECT count(*) FROM ai_llm_calls WHERE id > $BEFORE" | tr -d '\r'

echo "===== [V6] sidecar 日志：请求级目标 ====="
docker logs --since 10m itsm-ai-service-demo 2>&1 | grep -aE "request-scoped LLM target" | tail -4
echo -n "override_log_lines="; docker logs --since 10m itsm-ai-service-demo 2>&1 | grep -ac "request-scoped LLM target"
echo -n "sidecar_llm_unavailable_lines="; docker logs --since 10m itsm-ai-service-demo 2>&1 | grep -ac "using keyword fallback" || true
echo -n "sidecar_post_triage_lines="; docker logs --since 10m itsm-ai-service-demo 2>&1 | grep -ac "POST /triage" || true

echo "===== [V7] backend 日志：侧车成功（Guidance-constrained）而非降级/回落 ====="
docker logs --since 10m itsm-backend-demo 2>&1 | grep -aE "Guidance-constrained classification|degraded to keyword fallback|no sidecar-compatible|resolve sidecar LLM target failed|LLM classification failed" | tail -8
echo -n "guidance_success_lines="; docker logs --since 10m itsm-backend-demo 2>&1 | grep -ac "Guidance-constrained classification" || true
echo -n "degraded_lines="; docker logs --since 10m itsm-backend-demo 2>&1 | grep -ac "degraded to keyword fallback" || true

echo "===== [V8] 响应 reasoning 断言 ====="
echo -n "triage1_reasoning="; python3 -c 'import json;d=json.load(open("/tmp/f2_triage1.json"));print((d.get("data",{}).get("suggestions",{}) or {}).get("reasoning","<none>"))' 2>/dev/null || echo "<parse-failed>"
echo -n "triage2_reasoning="; python3 -c 'import json;d=json.load(open("/tmp/f2_triage2.json"));print((d.get("data",{}).get("suggestions",{}) or {}).get("reasoning","<none>"))' 2>/dev/null || echo "<parse-failed>"
echo "F2-VERIFY-DONE"