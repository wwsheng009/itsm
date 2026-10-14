#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""探针：打印 demo 用户的角色/权限与 MSP 上下文（只读）。"""
import json
import sys

sys.path.insert(0, "scripts/demo")
from seed_demo_dataset import Client, ApiError  # noqa: E402

BASE = "http://172.18.3.238:8089"
PW = "Demo@2026!"
PW_ADMIN = "Demo@2026!Admin"


def probe(user, pw):
    c = Client(BASE)
    c.login(user, pw)
    me = c.get("/api/v1/auth/me") or {}
    perms = sorted(str(p.get("code")) for p in (me.get("permissions") or []) if isinstance(p, dict))
    print("== %s ==" % user)
    print("  role=%s msp_role=%s tenant=%s" % (me.get("role"), me.get("mspRole") or me.get("msp_role"), me.get("tenantId")))
    print("  perms=%d: %s" % (len(perms), ",".join(perms)))
    for path in ("/api/v1/msp/context", "/api/v1/msp/status", "/api/v1/sla/definitions?page=1&pageSize=3"):
        try:
            d = c.get(path)
            s = json.dumps(d, ensure_ascii=False)
            print("  GET %-46s -> 200 %s" % (path, s[:140]))
        except ApiError as e:
            print("  GET %-46s -> %s %s" % (path, e.status, str(e.body)[:140]))
    print()


probe("demo.msp.manager", PW)
probe("demo.msp.agent1", PW)
probe("admin", PW_ADMIN)
