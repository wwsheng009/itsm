#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""复核 IM-40：customer 域外 DELETE 定义 → 404（只读语义：删除域外 id 为 no-op 拦截）。"""
import sys

sys.path.insert(0, "scripts/demo")
from seed_demo_dataset import Client, ApiError  # noqa: E402

BASE = "http://172.18.3.238:8089"
PW = "Demo@2026!"

c = Client(BASE)
c.login("demo.mfg.admin", PW)
for def_id in (9, 10):
    try:
        c.write("DELETE", f"/api/v1/sla/definitions/{def_id}")
        print(f"DELETE def {def_id} -> 200 (unexpected)")
    except ApiError as e:
        print(f"DELETE def {def_id} -> {e.status} {str(e.body)[:80]}")

# 复核定义仍在（provider 视角）
m = Client(BASE)
m.login("demo.msp.manager", PW)
import json  # noqa: E402

defs = m.get("/api/v1/sla/definitions?page=1&pageSize=50")
ids = sorted(d.get("id") for d in (defs.get("items") or []) if isinstance(d, dict))
print("provider defs intact:", [i for i in ids if i in (9, 10)])
