#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""探针：原始 /auth/me 与 /auth/menus 结构（只读）。"""
import json
import sys

sys.path.insert(0, "scripts/demo")
from seed_demo_dataset import Client  # noqa: E402

BASE = "http://172.18.3.238:8089"

for user in ("demo.msp.manager", "admin"):
    c = Client(BASE)
    c.login(user, "Demo@2026!Admin" if user == "admin" else "Demo@2026!")
    me = c.get("/api/v1/auth/me")
    print("== %s /auth/me ==" % user)
    if isinstance(me, dict):
        print("  keys: %s" % sorted(me.keys()))
        print("  raw[:500]: %s" % json.dumps(me, ensure_ascii=True)[:500])
        print("  perms(%d): %s" % (len(me.get("permissions") or []), json.dumps(me.get("permissions"), ensure_ascii=True)))
    try:
        menus = c.get("/api/v1/auth/menus")
        print("  menus type=%s len=%s sample=%s" % (
            type(menus).__name__,
            len(menus) if isinstance(menus, list) else (len(menus.get("menus") or []) if isinstance(menus, dict) else "?"),
            json.dumps((menus or [{}])[0] if isinstance(menus, list) else menus, ensure_ascii=True)[:200]))
    except Exception as e:  # noqa: BLE001
        print("  menus err: %r" % e)
    print()
