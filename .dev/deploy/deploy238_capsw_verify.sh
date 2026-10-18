#!/usr/bin/env bash
# 238 demo API 验收：Bot 能力平台级开关「请求期生效、免重启」（读端保留 / 写端 403 / 文案指引）
set -u
R=/home/n9e/itsm/repo
cd "$R" || { echo "NO_REPO"; exit 1; }
BASE="http://127.0.0.1:8089/api/v1"
JAR=/tmp/capsw_cookies.txt
rm -f "$JAR"
PW=$(grep -E '^DEMO_ADMIN_PASSWORD=' .env.demo | cut -d= -f2-)
[ -n "$PW" ] || { echo "NO_ADMIN_PW"; exit 9; }
csrf() { curl -s --max-time 20 -b "$JAR" -c "$JAR" "$BASE/csrf-token" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("csrf_token",""))' 2>/dev/null; }
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin).get("data",{});print(d.get("'"$1"'"))'; }

echo "===== [V0] 容器启动时间（证明全程未重启） ====="
echo "backend-start-before=$(docker inspect -f '{{.State.StartedAt}}' itsm-backend-demo)"

echo "===== [V1] login ====="
curl -s -o /dev/null -w 'login-http=%{http_code}\n' --max-time 20 -c "$JAR" -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$PW\"}"

echo "===== [V2] GET ai-capabilities（初始态） ====="
INIT=$(curl -s --max-time 20 -b "$JAR" "$BASE/system-configs/ai-capabilities")
echo "$INIT" | head -c 500; echo
BOT_INIT=$(echo "$INIT" | jget botEnabled)

echo "===== [V3] 关闭 bot.enabled（PUT，运行期生效） ====="
T=$(csrf)
curl -s --max-time 20 -b "$JAR" -H "X-CSRF-Token: $T" -H 'Content-Type: application/json' \
  -X PUT "$BASE/system-configs/ai-capabilities" -d '{"botEnabled":false}' | head -c 400; echo

echo "===== [V4] 关闭态：读端保留（GET /admin/bots 应非 403） ====="
curl -s -o /tmp/capsw_read.json -w 'read-http=%{http_code}\n' --max-time 20 -b "$JAR" "$BASE/admin/bots"
head -c 200 /tmp/capsw_read.json; echo

echo "===== [V5] 关闭态：写端 403 + 文案指引 ====="
T=$(csrf)
curl -s -o /tmp/capsw_write.json -w 'write-http=%{http_code}\n' --max-time 20 -b "$JAR" -H "X-CSRF-Token: $T" -H 'Content-Type: application/json' \
  -X POST "$BASE/admin/bots" -d '{"name":"capsw-probe","key":"capsw_probe"}'
head -c 400 /tmp/capsw_write.json; echo
grep -q '系统配置 → AI 配置' /tmp/capsw_write.json && echo 'MSG-OK' || echo 'MSG-MISSING'

echo "===== [V6] 打开 bot.enabled（免重启恢复） ====="
T=$(csrf)
curl -s --max-time 20 -b "$JAR" -H "X-CSRF-Token: $T" -H 'Content-Type: application/json' \
  -X PUT "$BASE/system-configs/ai-capabilities" -d '{"botEnabled":true}' | head -c 400; echo

echo "===== [V7] 开启态：写端放行（空 body 应为 400 校验错误而非 403） ====="
T=$(csrf)
curl -s -o /tmp/capsw_write2.json -w 'write-http=%{http_code}\n' --max-time 20 -b "$JAR" -H "X-CSRF-Token: $T" -H 'Content-Type: application/json' \
  -X POST "$BASE/admin/bots" -d '{}'
head -c 300 /tmp/capsw_write2.json; echo

echo "===== [V8] 恢复初始开关状态 + 无重启复核 ====="
if [ "$BOT_INIT" = "False" ]; then
  T=$(csrf)
  curl -s --max-time 20 -b "$JAR" -H "X-CSRF-Token: $T" -H 'Content-Type: application/json' \
    -X PUT "$BASE/system-configs/ai-capabilities" -d '{"botEnabled":false}' >/dev/null
  echo "restored botEnabled=false"
else
  echo "botEnabled kept true (initial=$BOT_INIT)"
fi
echo "backend-start-after=$(docker inspect -f '{{.State.StartedAt}}' itsm-backend-demo)"
echo "V-STATUS: DONE"
