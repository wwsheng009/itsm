#!/usr/bin/env python3
"""P0（SCD1–SCD4）演示环境验收脚本（8089）。

验收点：
  SCA1 按租户邮件路由：租户 A/B 各自 SMTP（mock-a/mock-b），邮件互不影响；
  SCA2 未配置回退：租户 C（edu）未配置且部署级无回退 → 后端告警跳过（不静默、不崩溃）；
  SCA3 保存即生效：保存后无需重启即可发信；
  SCA4 凭据治理：smtpPassword 入库密文（v1: 前缀）、API 回显掩码 ******。

用法（仓库根目录）：python scripts/demo/verify_scd_p0.py
环境变量：BASE（默认 http://172.18.3.238:8089）、DEMO_USER_PASSWORD（默认 Demo@2026!）
"""
import http.cookiejar
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = os.environ.get("BASE", "http://172.18.3.238:8089")
PASSWORD = os.environ.get("DEMO_USER_PASSWORD", "Demo@2026!")
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

TENANTS = {
    "mfg": {"username": "demo.mfg.admin", "tenant_id": 3, "mock": "itsm-smtp-mock-a", "from": "ops-a@tenant-a.example", "user_email": "demo.mfg.admin@demo.example.com"},
    "med": {"username": "demo.med.admin", "tenant_id": 4, "mock": "itsm-smtp-mock-b", "from": "ops-b@tenant-b.example", "user_email": "demo.med.admin@demo.example.com"},
    "edu": {"username": "demo.edu.admin", "tenant_id": 5, "mock": None, "from": "", "user_email": "demo.edu.admin@demo.example.com"},
}

RESULTS = []


def record(name, ok, detail=""):
    RESULTS.append((name, ok, detail))
    print("[%s] %s %s" % ("PASS" if ok else "FAIL", name, ("| " + detail) if detail else ""))


class Client:
    def __init__(self, base):
        self.base = base
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))

    def _request(self, method, path, body=None, timeout=30):
        data = json.dumps(body).encode("utf-8") if body is not None else None
        headers = {"Accept": "application/json"}
        if data is not None:
            headers["Content-Type"] = "application/json"
        if method != "GET":
            token = self._csrf()
            if token:
                headers["X-CSRF-Token"] = token
        req = urllib.request.Request(self.base + path, data=data, method=method, headers=headers)
        try:
            with self.opener.open(req, timeout=timeout) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            payload = exc.read().decode("utf-8", "replace")
            raise RuntimeError("HTTP %s %s -> %s: %s" % (method, path, exc.code, payload[:300]))

    def get(self, path, timeout=30):
        return self._request("GET", path, None, timeout)

    def write(self, method, path, body, timeout=60):
        return self._request(method, path, body, timeout)

    def _csrf(self):
        try:
            data = self.get("/api/v1/csrf-token", timeout=15)
            return (data.get("data") or {}).get("csrf_token", "")
        except Exception:
            return ""


def remote(cmd, sudo=True, timeout=180):
    args = [sys.executable, os.path.join(ROOT, ".dev", "deploy", "remote.py")]
    if sudo:
        args.append("--sudo")
    args.append(cmd)
    env = dict(os.environ)
    env.setdefault("ITSM_DEPLOY_PASSWORD", "passw0rd")
    env.setdefault("PYTHONIOENCODING", "utf-8")
    proc = subprocess.run(args, cwd=ROOT, env=env, capture_output=True, text=True, timeout=timeout)
    return (proc.stdout or "") + (proc.stderr or "")


def login(client, username):
    resp = client.write("POST", "/api/v1/auth/login", {"username": username, "password": PASSWORD})
    if resp.get("code") != 0:
        raise RuntimeError("login failed for %s: %s" % (username, resp))
    return resp


def get_config_map(client):
    resp = client.get("/api/v1/system-configs?page=1&pageSize=200")
    items = (resp.get("data") or {}).get("items") or []
    return {item.get("key"): item for item in items}


def batch_update(client, entries):
    return client.write("PUT", "/api/v1/system-configs/batch", entries)


def first_ticket(client):
    resp = client.get("/api/v1/tickets?page=1&pageSize=1")
    data = resp.get("data") or {}
    rows = data.get("items") or data.get("tickets") or []
    return rows[0] if rows else None


def me_id(client):
    resp = client.get("/api/v1/auth/me")
    data = resp.get("data") or {}
    return data.get("id") or data.get("userId")


def main():
    print("=== P0 SCD 验收 @ %s ===" % BASE)
    # 清空 mock 日志，保证本轮断言只针对本次触发（幂等重跑）。
    for mock in ("itsm-smtp-mock-a", "itsm-smtp-mock-b"):
        remote("docker exec %s sh -c '> /tmp/mock_%s.log' || true" % (mock, mock[-1]))

    clients = {}
    for key, meta in TENANTS.items():
        client = Client(BASE)
        login(client, meta["username"])
        clients[key] = client
        print("[info] login ok: %s (tenant %s)" % (meta["username"], meta["tenant_id"]))

    # ---------- SCA3+SCA4: 保存租户 A/B SMTP（含密码）并立即回读 ----------
    for key in ("mfg", "med"):
        meta = TENANTS[key]
        client = clients[key]
        cfg_map = get_config_map(client)
        secret = "Scd-%s-Secret-2026" % key
        entries = []
        for ckey, value in (
            ("smtpHost", meta["mock"]),
            ("smtpPort", "2525"),
            ("smtpUsername", ""),
            ("smtpPassword", secret),
            ("smtpEnableSSL", "false"),
            ("emailFrom", meta["from"]),
        ):
            base = cfg_map.get(ckey, {})
            entries.append({
                "key": ckey,
                "value": value,
                "valueType": base.get("valueType") or ("boolean" if ckey == "smtpEnableSSL" else "string"),
                "description": base.get("description") or "",
            })
        resp = batch_update(client, entries)
        ok_save = resp.get("code") == 0
        record("SCA3 save configs [%s]" % key, ok_save, "code=%s" % resp.get("code"))

        after = get_config_map(client)
        host = (after.get("smtpHost") or {}).get("value")
        pwd = (after.get("smtpPassword") or {}).get("value")
        record("SCA3 save-then-read host [%s]" % key, host == meta["mock"], "host=%r" % host)
        record("SCA4 password masked [%s]" % key, pwd == "******", "value=%r" % pwd)

    # ---------- SCA1+SCA3: 触发邮件（找回密码 = 直连 EmailService 的租户解析路径） ----------
    # 说明：工单通知 API 走 outbox → connector 投递（另一条链路），不经过 EmailService；
    # 找回密码为直连发信路径，能验证「保存即生效 + 租户 SMTP 路由」。
    for key in ("mfg", "med"):
        meta = TENANTS[key]
        resp = clients[key].write("POST", "/api/v1/auth/forgot-password", {"email": meta["user_email"]})
        record("SCA1 trigger mail [%s]" % key, resp.get("code") == 0, "code=%s" % resp.get("code"))

    # ---------- SCA2: 租户 C 未配置（清空 smtpHost）→ 发信应告警跳过 ----------
    edu = clients["edu"]
    edu_map = get_config_map(edu)
    edu_entries = []
    for ckey in ("smtpHost", "smtpUsername", "smtpPassword", "emailFrom"):
        base = edu_map.get(ckey, {})
        edu_entries.append({"key": ckey, "value": "", "valueType": base.get("valueType") or "string",
                            "description": base.get("description") or ""})
    resp = batch_update(edu, edu_entries)
    record("SCA2 clear edu smtp", resp.get("code") == 0, "code=%s" % resp.get("code"))
    resp = edu.write("POST", "/api/v1/auth/forgot-password", {"email": TENANTS["edu"]["user_email"]})
    record("SCA2 edu trigger accepted", resp.get("code") == 0, "code=%s" % resp.get("code"))

    print("[info] waiting 6s for mail delivery ...")
    time.sleep(6)

    # ---------- SCA1 校验：mock 日志隔离 ----------
    logs = {}
    for key, mock in (("mfg", "itsm-smtp-mock-a"), ("med", "itsm-smtp-mock-b")):
        out = remote("docker exec %s sh -c 'cat /tmp/mock_%s.log 2>/dev/null || echo NO_MAIL_YET'" % (mock, mock[-1]))
        logs[key] = out
    a_from, b_from = TENANTS["mfg"]["from"], TENANTS["med"]["from"]
    a_to, b_to = TENANTS["mfg"]["user_email"], TENANTS["med"]["user_email"]
    edu_to = TENANTS["edu"]["user_email"]
    record("SCA1 mock-a got tenant A mail", a_from in logs["mfg"], "marker=%s" % a_from)
    record("SCA1 mock-a To header [mfg]", a_to in logs["mfg"], "to=%s" % a_to)
    record("SCA1 mock-a NOT got tenant B mail", b_from not in logs["mfg"] and b_to not in logs["mfg"])
    record("SCA1 mock-b got tenant B mail", b_from in logs["med"], "marker=%s" % b_from)
    record("SCA1 mock-b To header [med]", b_to in logs["med"], "to=%s" % b_to)
    record("SCA1 mock-b NOT got tenant A mail", a_from not in logs["med"] and a_to not in logs["med"])
    record("SCA2 no mail for unconfigured tenant", edu_to not in logs["mfg"] and edu_to not in logs["med"])

    # ---------- SCA4 校验：数据库密文前缀 ----------
    sql = ("docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -t -A -F'|' -c "
           "\"select tenant_id, left(value,3), length(value) from system_configs "
           "where key='smtpPassword' and tenant_id in (3,4,5) and deleted_at is null order by tenant_id;\"")
    out = remote(sql)
    rows = {}
    for line in out.splitlines():
        parts = [p.strip() for p in line.split("|")]
        if len(parts) == 3 and parts[0].isdigit():
            rows[int(parts[0])] = (parts[1], parts[2])
    record("SCA4 ciphertext v1: [mfg]", rows.get(3, ("", ""))[0].startswith("v1:"), "row=%s" % (rows.get(3),))
    record("SCA4 ciphertext v1: [med]", rows.get(4, ("", ""))[0].startswith("v1:"), "row=%s" % (rows.get(4),))

    # ---------- SCA4b: 掩码回写不覆盖原值（前端回显 ****** 后原样提交的场景） ----------
    mfg_map = get_config_map(clients["mfg"])
    resp = batch_update(clients["mfg"], [{
        "key": "smtpPassword",
        "value": "******",
        "valueType": "string",
        "description": (mfg_map.get("smtpPassword") or {}).get("description") or "",
    }])
    record("SCA4b mask write accepted", resp.get("code") == 0, "code=%s" % resp.get("code"))
    out2 = remote(sql)
    rows2 = {}
    for line in out2.splitlines():
        parts = [p.strip() for p in line.split("|")]
        if len(parts) == 3 and parts[0].isdigit():
            rows2[int(parts[0])] = (parts[1], parts[2])
    record("SCA4b ciphertext preserved after mask",
           rows2.get(3, ("", ""))[1] == rows.get(3, ("", ""))[1] and rows2.get(3, ("", ""))[0].startswith("v1:"),
           "before=%s after=%s" % (rows.get(3), rows2.get(3)))

    # ---------- D6: Redis 失效订阅启动 ----------
    sub_out = remote("docker exec itsm-backend-demo sh -c \"grep -h 'invalidation subscriber started' /app/logs/*.log 2>/dev/null | tail -2\" || true")
    record("D6 redis invalidation subscriber started", "subscriber started" in sub_out, sub_out.strip()[:160])

    # ---------- SCA2 校验：后端日志出现告警跳过 ----------
    log_out = remote("docker exec itsm-backend-demo sh -c \"grep -h 'password reset email skipped' /app/logs/*.log 2>/dev/null | tail -4\" || true")
    if "mailer unavailable" not in log_out:
        log_out = remote("docker logs --since 20m itsm-backend-demo 2>&1 | grep -E 'password reset email skipped' | tail -4 || true")
    record("SCA2 backend warn on missing smtp", "mailer unavailable" in log_out, log_out.strip()[:200])

    # ---------- 汇总 ----------
    failed = [r for r in RESULTS if not r[1]]
    print("\n=== SUMMARY: %d passed, %d failed ===" % (len(RESULTS) - len(failed), len(failed)))
    for name, ok, detail in RESULTS:
        if not ok:
            print("FAIL: %s | %s" % (name, detail))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
