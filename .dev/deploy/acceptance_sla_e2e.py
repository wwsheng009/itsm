#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""238 demo SLA 端到端验收执行器（SLA-A1…A17，部署 c700c060）。

运行（本机直连演示入口）：
    python .dev/deploy/acceptance_sla_e2e.py --mode run        # 场景构造 + API 断言
    python .dev/deploy/acceptance_sla_e2e.py --mode discover   # 仅侦察（只读）

产出：以 "RESULT: {json}" 行输出每步结果；供人工汇总为验收记录。
安全：仅演示库（8090:itsm_demo）；所有实体以 [ACC] 前缀标记；幂等（按 contractNo/标题 复用）。
"""
from __future__ import annotations

import argparse
import json
import sys
import time

sys.path.insert(0, "scripts/demo")
from seed_demo_dataset import Client, ApiError  # noqa: E402

BASE = "http://172.18.3.238:8089"
PW = "Demo@2026!"
PW_ADMIN = "Demo@2026!Admin"

T_MSP = 2      # demo-provider
T_MFG = 3      # demo-mfg
T_MED = 4      # demo-med
T_EDU = 5      # demo-edu

RESULTS = []


def rec(step: str, status: str, **detail):
    item = {"step": step, "status": status, **detail}
    RESULTS.append(item)
    print("RESULT: " + json.dumps(item, ensure_ascii=False), flush=True)


def clients() -> dict:
    out = {}
    for key, user, pw in [
        ("msp", "demo.msp.manager", PW),
        ("mfg_admin", "demo.mfg.admin", PW),
        ("mfg_agent", "demo.mfg.agent", PW),
        ("med_agent", "demo.med.agent", PW),
        ("edu_agent", "demo.edu.agent", PW),
        ("admin", "admin", PW_ADMIN),
    ]:
        c = Client(BASE)
        c.login(user, pw)
        out[key] = c
    return out


def as_list(data, keys=("items", "list", "rows", "types", "definitions", "contracts")) -> list:
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        for k in keys:
            v = data.get(k)
            if isinstance(v, list):
                return v
    return []


def try_call(fn, *a, **kw):
    """返回 (ok, data_or_error_payload)。"""
    try:
        return True, fn(*a, **kw)
    except ApiError as e:
        return False, {"__http__": e.status, "body": e.body}
    except Exception as e:  # noqa: BLE001
        return False, {"__err__": repr(e)}


def idd(d):
    """从响应中提取实体 id（兼容顶层 id / 嵌套 {xxx:{id}} 形态）。"""
    if not isinstance(d, dict):
        return None
    if d.get("id") is not None:
        return d.get("id")
    for v in d.values():
        if isinstance(v, dict) and v.get("id") is not None:
            return v.get("id")
    return None


def post_incident(c: Client, title: str, priority: str = "high"):
    return c.write("POST", "/api/v1/incidents", {
        "title": title, "type": "incident", "priority": priority, "source": "manual",
    })


def main() -> int:
    global BASE
    ap = argparse.ArgumentParser()
    ap.add_argument("--mode", choices=["discover", "run"], default="discover")
    ap.add_argument("--base-url", default=BASE)
    args = ap.parse_args()
    BASE = args.base_url

    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")  # type: ignore[attr-defined]
    except Exception:  # noqa: BLE001
        pass

    cs = clients()

    # ---------------- 侦察（两种模式都跑） ----------------
    ok, msp_defs = try_call(cs["msp"].get, "/api/v1/sla/definitions?page=1&pageSize=100")
    defs = as_list(msp_defs) if ok else []
    provider_defs = [d for d in defs if isinstance(d, dict) and int(d.get("tenantId") or 0) == T_MSP]
    rec("DISC-msp-defs", "OK" if ok else "FAIL",
        total=len(defs), provider_defs=len(provider_defs),
        sample=[{k: d.get(k) for k in ("id", "name", "serviceType", "priority", "responseTime", "resolutionTime", "isActive", "ownerProviderTenantId")} for d in provider_defs[:8]],
        err=None if ok else msp_defs)

    ok, msp_types = try_call(cs["msp"].get, "/api/v1/ticket-types?page=1&pageSize=100")
    mtypes = as_list(msp_types) if ok else []
    rec("DISC-msp-types", "OK" if ok else "FAIL", total=len(mtypes),
        sample=[{k: t.get(k) for k in ("id", "code", "name", "slaEnabled", "slaExempt", "defaultSlaId", "origin", "tenantId")} for t in mtypes[:10]],
        err=None if ok else msp_types)

    ok, mfg_types = try_call(cs["mfg_admin"].get, "/api/v1/ticket-types?page=1&pageSize=100")
    ftypes = as_list(mfg_types) if ok else []
    rec("DISC-mfg-types", "OK" if ok else "FAIL", total=len(ftypes),
        sample=[{k: t.get(k) for k in ("id", "code", "name", "slaEnabled", "slaExempt", "defaultSlaId", "origin", "tenantId")} for t in ftypes[:12]],
        err=None if ok else mfg_types)

    ok, contracts = try_call(cs["msp"].get, "/api/v1/sla/contracts")
    cts = as_list(contracts) if ok else []
    rec("DISC-contracts", "OK" if ok else "FAIL", total=len(cts),
        sample=[{k: c.get(k) for k in ("id", "contractNo", "customerTenantId", "status", "effectiveFrom", "effectiveUntil")} for c in cts[:10]],
        err=None if ok else contracts)

    ok, provided = try_call(cs["mfg_admin"].get, "/api/v1/sla/definitions/provided")
    prov = as_list(provided) if ok else []
    rec("DISC-provided", "OK" if ok else "FAIL", total_=len(prov),
        raw_keys=sorted(provided.keys()) if isinstance(provided, dict) else type(provided).__name__,
        sample=[{k: p.get(k) for k in ("slaDefinitionId", "contractId", "ticketTypeId", "providerTenantId")} for p in prov[:5]],
        err=None if ok else provided)

    if args.mode == "discover":
        print("SUMMARY: " + json.dumps({"results": len(RESULTS), "mode": "discover"}, ensure_ascii=False))
        return 0

    # ==================== run：场景构造与断言 ====================
    ts = time.strftime("%m%d%H%M")
    step_fail = 0

    # ---- 选取：provider 定义（优先 isActive + responseTime>0）、源类型 ----
    def pick_def():
        for d in provider_defs:
            if d.get("isActive") and int(d.get("responseTime") or 0) > 0:
                return d
        return provider_defs[0] if provider_defs else None

    sel_def = pick_def()
    if not sel_def:
        rec("A1", "BLOCKED", reason="no provider defs on tenant 2")
        step_fail += 1
    rec("SEL-def", "OK" if sel_def else "FAIL",
        **({"def": {k: sel_def.get(k) for k in ("id", "name", "responseTime", "resolutionTime", "serviceType", "priority")}} if sel_def else {}))

    # ---- A2 基线（合同创建前建单；若合同已存在则仅记录） ----
    c1_no = "ACC-A1-MFG"
    existing_c1 = next((c for c in cts if c.get("contractNo") == c1_no), None)
    if not existing_c1:
        ok, inc0 = try_call(post_incident, cs["mfg_agent"], "[ACC] A2 baseline before contract")
        rec("A2-baseline", "OK" if ok else "FAIL",
            incident=(inc0 or {}).get("id") if ok else None, err=None if ok else inc0)
    else:
        rec("A2-baseline", "SKIP", reason="contract already existed; baseline unavailable")

    # ---- A1：创建合同（reuse-or-create）→ 建单 → 期望 L1 ----
    if sel_def:
        if existing_c1:
            c1 = existing_c1
            rec("A1-contract", "REUSED", contractId=c1.get("id"), contractNo=c1_no)
        else:
            ok, c1 = try_call(cs["msp"].write, "POST", "/api/v1/sla/contracts", {
                "customerTenantId": T_MFG,
                "contractNo": c1_no,
                "name": "[ACC] A1 contract (mfg)",
                "effectiveFrom": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - 86400)),
                "status": "active",
                "settlementCycle": "monthly",
                "currency": "CNY",
                "bindings": [{"slaDefinitionId": int(sel_def["id"]), "ticketTypeId": 0}],
            })
            if ok:
                rec("A1-contract", "OK", contractId=c1.get("id"), contractNo=c1_no)
            else:
                rec("A1-contract", "FAIL", err=c1)
                step_fail += 1

        ok, inc1 = try_call(post_incident, cs["mfg_agent"], f"[ACC] A1+settlement probe {ts}")
        rec("A1-incident", "OK" if ok else "FAIL",
            incident=(inc1 or {}).get("id") if ok else None,
            number=(inc1 or {}).get("incidentNumber") if ok else None,
            err=None if ok else inc1)
        if not ok:
            step_fail += 1

    # ---- A4：customer 写保护 / 非授权 provided 读取 ----
    if sel_def:
        ok, r = try_call(cs["mfg_admin"].write, "PUT", f"/api/v1/sla/definitions/{int(sel_def['id'])}", {"name": "HACK-BY-CUSTOMER"})
        status = r.get("__http__") if not ok else 200
        rec("A4-put-provided-def", "PASS" if status in (403, 404) else "FAIL", http=status, body=(r.get("body") if not ok else None))
        if status not in (403, 404):
            step_fail += 1
        ok, r = try_call(cs["mfg_admin"].write, "DELETE", f"/api/v1/sla/definitions/{int(sel_def['id'])}")
        status = r.get("__http__") if not ok else 200
        rec("A4-del-provided-def", "PASS" if status in (403, 404) else "WARN",
            http=status, note="=200 时为租户域外 no-op 语义，须 DB 复核目标定义未被变更")
        # 非授权定义：选另一个 provider 定义（不在 bindings 中）
        others = [d for d in provider_defs if int(d["id"]) != int(sel_def["id"])]
        if others:
            ok, r = try_call(cs["mfg_admin"].get, f"/api/v1/sla/definitions/{int(others[0]['id'])}")
            status = 200 if ok else r.get("__http__")
            rec("A4-read-foreign-def", "PASS" if status in (403, 404) else "FAIL", http=status, defId=int(others[0]["id"]))

    # ---- A10：provided 白名单字段（合同建立后重取） ----
    ok, provided2 = try_call(cs["mfg_admin"].get, "/api/v1/sla/definitions/provided")
    if ok:
        prov = as_list(provided2)
    rec("A10-provided-count", "OK" if ok else "FAIL", total=len(prov))
    if isinstance(provided, dict) or isinstance(provided, list):
        keyset = set()
        for p in prov:
            if isinstance(p, dict):
                keyset |= set(p.keys())
        allowed = {"contractId", "contractNo", "slaDefinitionId", "name", "description", "serviceType",
                   "priority", "responseTime", "resolutionTime", "ticketTypeId", "providerTenantId", "validUntil"}
        leaked = sorted(keyset - allowed)
        rec("A10-provided-keys", "PASS" if not leaked else "FAIL", keys=sorted(keyset), leaked=leaked)
        if leaked:
            step_fail += 1

    # ---- A5：过期合同（med）→ 新建单不得 L1 ----
    med_no = "ACC-A5-MED-EXPIRED"
    existing_c5 = next((c for c in cts if c.get("contractNo") == med_no), None)
    if sel_def and not existing_c5:
        ok, c5 = try_call(cs["msp"].write, "POST", "/api/v1/sla/contracts", {
            "customerTenantId": T_MED,
            "contractNo": med_no,
            "name": "[ACC] A5 expired-period contract (med)",
            "effectiveFrom": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - 30 * 86400)),
            "effectiveUntil": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - 86400)),
            "status": "active",
            "bindings": [{"slaDefinitionId": int(sel_def["id"]), "ticketTypeId": 0}],
        })
        rec("A5-contract", "OK" if ok else "FAIL", contractId=(c5 or {}).get("id") if ok else None, err=None if ok else c5)
        if not ok:
            step_fail += 1
    elif existing_c5:
        rec("A5-contract", "REUSED", contractId=existing_c5.get("id"))
    if sel_def:
        ok, inc5 = try_call(post_incident, cs["med_agent"], f"[ACC] A5 expired-period probe {ts}")
        rec("A5-incident", "OK" if ok else "FAIL", incident=(inc5 or {}).get("id") if ok else None, err=None if ok else inc5)

    # ---- tickets 数据面（SLA 证据载体：incidents 无 SLA 列；结算读 tickets） ----
    mfg_type_id = 13
    okx, ft2 = try_call(cs["mfg_admin"].get, "/api/v1/ticket-types?page=1&pageSize=100")
    for t in (as_list(ft2) if okx else []):
        if str(t.get("code")) == "general_incident":
            mfg_type_id = int(t.get("id"))

    def mk_ticket(c, title, type_id=None, form_fields=None):
        payload = {"title": title, "priority": "high", "type": "incident"}
        if type_id:
            payload["ticketTypeId"] = int(type_id)
        if form_fields:
            payload["formFields"] = form_fields
        return c.write("POST", "/api/v1/tickets", payload)

    # 确保合同含 mfg 类型绑定（结算覆盖类型工单；幂等，保留既有/分发产生的绑定）
    try:
        c1id = int(c1["id"])
    except Exception:  # noqa: BLE001
        c1id = None
    if c1id:
        okc, cd = try_call(cs["msp"].get, f"/api/v1/sla/contracts/{c1id}")
        if okc and isinstance(cd, dict):
            bs = cd.get("bindings") or []
            if not any(int(b.get("ticketTypeId") or 0) == mfg_type_id for b in bs):
                agn = next((b for b in bs if int(b.get("ticketTypeId") or 0) == 0), None)
                agn_def = int(agn.get("slaDefinitionId")) if agn else 9
                newb = [{"slaDefinitionId": int(b.get("slaDefinitionId")), "ticketTypeId": int(b.get("ticketTypeId") or 0)} for b in bs]
                newb.append({"slaDefinitionId": agn_def, "ticketTypeId": mfg_type_id})
                okb, rb = try_call(cs["msp"].write, "PUT", f"/api/v1/sla/contracts/{c1id}", {"bindings": newb})
                rec("A11-binding", "OK" if okb else "FAIL", err=None if okb else rb)
            else:
                rec("A11-binding", "REUSED")

    ok, tk1 = try_call(mk_ticket, cs["mfg_agent"], f"[ACC] A1 ticket L1 probe {ts}", mfg_type_id,
                       {"symptom": "[ACC] probe symptom"})
    rec("A1-ticket", "OK" if ok else "FAIL", ticket=idd(tk1), err=None if ok else tk1)
    ok, tk5 = try_call(mk_ticket, cs["med_agent"], f"[ACC] A5 ticket expired probe {ts}")
    rec("A5-ticket", "OK" if ok else "FAIL", ticket=idd(tk5), err=None if ok else tk5)

    # ---- A9：解绑级联（edu：contract active→incident；terminate→incident） ----
    edu_no = "ACC-A9-EDU"
    existing_c9 = next((c for c in cts if c.get("contractNo") == edu_no), None)
    c9 = existing_c9
    if sel_def and not existing_c9:
        ok, c9 = try_call(cs["msp"].write, "POST", "/api/v1/sla/contracts", {
            "customerTenantId": T_EDU,
            "contractNo": edu_no,
            "name": "[ACC] A9 revoke-cascade contract (edu)",
            "effectiveFrom": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - 86400)),
            "status": "active",
            "bindings": [{"slaDefinitionId": int(sel_def["id"]), "ticketTypeId": 0}],
        })
        rec("A9-contract", "OK" if ok else "FAIL", contractId=(c9 or {}).get("id") if ok else None, err=None if ok else c9)
        if not ok:
            step_fail += 1
    if sel_def:
        if c9:
            # 幂等归一化：历史运行可能已终止，先恢复 active 再验证级联
            okst, r = try_call(cs["msp"].write, "PUT", f"/api/v1/sla/contracts/{int(c9['id'])}", {"status": "active"})
            rec("A9-reactivate", "OK" if okst else "FAIL", err=None if okst else r)
        ok, inc9a = try_call(post_incident, cs["edu_agent"], f"[ACC] A9 before-revoke probe {ts}")
        rec("A9-incident-active", "OK" if ok else "FAIL", incident=(inc9a or {}).get("id") if ok else None, err=None if ok else inc9a)
        ok, tk9a = try_call(mk_ticket, cs["edu_agent"], f"[ACC] A9 ticket before-revoke {ts}")
        rec("A9-ticket-active", "OK" if ok else "FAIL", ticket=idd(tk9a), err=None if ok else tk9a)
        if c9:
            terminated = None
            for st in ("terminated", "inactive", "suspended", "expired", "cancelled"):
                ok, r = try_call(cs["msp"].write, "PUT", f"/api/v1/sla/contracts/{int(c9['id'])}", {"status": st})
                if ok:
                    terminated = st
                    break
                last_err = r
            rec("A9-terminate", "OK" if terminated else "FAIL", terminatedStatus=terminated, err=None if terminated else last_err)
            if not terminated:
                step_fail += 1
            ok, inc9b = try_call(post_incident, cs["edu_agent"], f"[ACC] A9 after-revoke probe {ts}")
            rec("A9-incident-revoked", "OK" if ok else "FAIL", incident=(inc9b or {}).get("id") if ok else None, err=None if ok else inc9b)
            ok, tk9b = try_call(mk_ticket, cs["edu_agent"], f"[ACC] A9 ticket after-revoke {ts}")
            rec("A9-ticket-revoked", "OK" if ok else "FAIL", ticket=idd(tk9b), err=None if ok else tk9b)

    # ---- A3：fail-loud（edu 已终止合同后建单 → 期望 L4 或非 L1；日志另查） ----
    ok, inc3 = try_call(post_incident, cs["edu_agent"], f"[ACC] A3 fail-loud probe {ts}")
    rec("A3-incident", "OK" if ok else "FAIL", incident=(inc3 or {}).get("id") if ok else None, err=None if ok else inc3)

    # ---- A6/A11/A13/A14：结算报表 ----
    frm = time.strftime("%Y-%m-%d", time.gmtime(time.time() - 30 * 86400))
    to = time.strftime("%Y-%m-%d", time.gmtime(time.time() + 86400))
    ok, rep = try_call(cs["msp"].get, f"/api/v1/sla/settlement-report?customerTenantId={T_MFG}&from={frm}&to={to}")
    if ok and isinstance(rep, dict):
        rows = rep.get("rows") or []
        rec("A11-settlement", "OK",
            covered=rep.get("coveredTickets"), exempt=rep.get("exemptTickets"), uncovered=rep.get("uncoveredTickets"),
            breached=rep.get("contractBreached"), settlementBreaches=rep.get("settlementBreaches"),
            excludedInternal=rep.get("excludedInternalViolations"), rows=len(rows),
            compliance=rep.get("complianceRate"))
        if rows:
            r0 = rows[0]
            has3 = all(k in r0 for k in ("contractVerdict", "internalVerdict", "driftFlag", "matchedLayer"))
            rec("A13-dual-verdict", "PASS" if has3 else "FAIL",
                row0={k: r0.get(k) for k in ("ticketId", "contractVerdict", "internalVerdict", "driftFlag", "matchedLayer", "violationState")})
            if not has3:
                step_fail += 1
            l1rows = [r for r in rows if r.get("matchedLayer") == "L1"]
            rec("A13-L1-in-rows", "PASS" if l1rows else "FAIL", l1Count=len(l1rows))
        else:
            rec("A13-dual-verdict", "WARN", reason="no rows in period")
    else:
        rec("A11-settlement", "FAIL", err=rep)
        step_fail += 1

    ok, rep_bad = try_call(cs["msp"].get, f"/api/v1/sla/settlement-report?customerTenantId=1&from={frm}&to={to}")
    if ok:
        rec("A6-foreign-tenant", "FAIL", reason="expected 403/404, got 200", body=str(rep_bad)[:200])
        step_fail += 1
    else:
        st = rep_bad.get("__http__")
        body = rep_bad.get("body")
        body_s = json.dumps(body, ensure_ascii=False) if body is not None else ""
        leaky = any(k in body_s for k in ("coveredTickets", "rows", "breached", "total"))
        rec("A6-foreign-tenant", "PASS" if st in (403, 404) and not leaky else "FAIL",
            http=st, no_counts=(not leaky), body=str(body)[:200])
        if st not in (403, 404) or leaky:
            step_fail += 1

    # ---- 并轨闸门：grant 写端点 410；grants 读可用 ----
    ok, r410 = try_call(cs["msp"].write, "POST", "/api/v1/sla/grants", {"slaDefinitionId": 1, "customerTenantId": T_MFG})
    st = r410.get("__http__") if not ok else 200
    rec("P1-GATE-grant-410", "PASS" if st == 410 else "FAIL", http=st)
    if st != 410:
        step_fail += 1
    ok, glist = try_call(cs["msp"].get, "/api/v1/sla/grants")
    rec("P1-GATE-grant-read", "OK" if ok else "FAIL", err=None if ok else glist)

    # ---- A15/A16/A17：类型受控下发（mfg）；确保 provider 源类型存在（t2 无类型行 → 幂等创建） ----
    src_code = "acc_pacs_incident"
    src = None
    ok, mt2 = try_call(cs["msp"].get, "/api/v1/ticket-types?page=1&pageSize=100")
    for t in (as_list(mt2) if ok else []):
        if str(t.get("code")) == src_code:
            src = t
            break
    if src is not None:
        rec("SEL-source-type", "REUSED", id=src.get("id"), code=src_code)
    elif sel_def:
        ok, created = try_call(cs["msp"].write, "POST", "/api/v1/ticket-types", {
            "code": src_code,
            "name": "[ACC] PACS incident (source)",
            "description": "[ACC] distribution source for SLA-A15/A16/A17",
            "defaultPriority": "high",
            "sortOrder": 99,
            "slaEnabled": True,
            "defaultSlaId": int(sel_def["id"]),
        })
        if ok and idd(created):
            src = {"id": idd(created), "code": src_code, "defaultSlaId": int(sel_def["id"])}
            rec("SEL-source-type", "CREATED", id=src["id"], defaultSlaId=src["defaultSlaId"])
        else:
            rec("SEL-source-type", "FAIL", err=created)
    else:
        rec("SEL-source-type", "FAIL", reason="no sel_def")

    dist_id = None
    if src:
        ok, d = try_call(cs["msp"].write, "POST", f"/api/v1/msp/customers/{T_MFG}/ticket-types/{int(src['id'])}/distribute")
        if ok and isinstance(d, dict):
            dd = d.get("distribution") or {}
            dist_id = dd.get("id")
            rec("A15-distribute", "OK", distId=dist_id, copyTypeId=dd.get("copyTypeId"), copyCode=dd.get("copyTypeCode"),
                syncStatus=dd.get("syncStatus"), slaBinding=d.get("slaBinding"))
            # A16：同源同客户重复下发 → 409（防重复物化守卫）
            ok2, dup = try_call(cs["msp"].write, "POST", f"/api/v1/msp/customers/{T_MFG}/ticket-types/{int(src['id'])}/distribute")
            st = 409 if not ok2 else 200
            rec("A16-dup-guard", "PASS" if st == 409 else "FAIL", http=st)
        else:
            st = d.get("__http__")
            rec("A15-distribute", "SKIP" if st == 409 else "FAIL", http=st, err=d)

    # 复用已存在分发（若 conflict）
    if src and dist_id is None:
        ok, dl = try_call(cs["msp"].get, f"/api/v1/msp/customers/{T_MFG}/ticket-types/distributions")
        items = as_list(dl) if ok else []
        active = [x for x in items if x.get("syncStatus") not in ("unbound", "orphaned") and int(x.get("sourceTypeId") or 0) == int(src["id"])]
        if active:
            dist_id = active[0].get("id")
            rec("A15-distribute", "REUSED", distId=dist_id, copyTypeId=active[0].get("copyTypeId"))

    if dist_id:
        ok, prev = try_call(cs["msp"].get, f"/api/v1/msp/customers/{T_MFG}/ticket-types/distributions/{dist_id}/preview")
        rec("A17-preview-initial", "OK" if ok else "FAIL", preview=(prev if ok else prev))
        # 客户侧：受控字段锁定 + 治理字段可改
        copy_id = None
        ok2, dl3 = try_call(cs["msp"].get, f"/api/v1/msp/customers/{T_MFG}/ticket-types/distributions")
        for x in as_list(dl3) if ok2 else []:
            if int(x.get("id") or 0) == int(dist_id):
                copy_id = x.get("copyTypeId")
        if copy_id:
            ok, r = try_call(cs["mfg_admin"].write, "PUT", f"/api/v1/ticket-types/{int(copy_id)}", {"name": "HACK-BY-CUSTOMER"})
            st = r.get("__http__") if not ok else 200
            rec("A15-lock-controlled", "PASS" if st in (400, 403) else "FAIL", http=st, body=str(r.get("body"))[:160] if not ok else None)
            if st not in (400, 403):
                step_fail += 1
            ok, r = try_call(cs["mfg_admin"].write, "PUT", f"/api/v1/ticket-types/{int(copy_id)}", {"color": "#123456", "sortOrder": 77})
            st = 200 if ok else r.get("__http__")
            rec("A15-edit-governance", "PASS" if st == 200 else "FAIL", http=st)
            if st != 200:
                step_fail += 1
            # 编辑源 → dirty → 预览 diff → 发布
            ok, r = try_call(cs["msp"].write, "PUT", f"/api/v1/ticket-types/{int(src['id'])}",
                             {"description": f"[ACC] publish probe {ts}"})
            rec("A17-edit-source", "OK" if ok else "FAIL", err=None if ok else r)
            ok, prev2 = try_call(cs["msp"].get, f"/api/v1/msp/customers/{T_MFG}/ticket-types/distributions/{dist_id}/preview")
            rec("A17-preview-diff", "OK" if ok else "FAIL", preview=(prev2 if ok else prev2))
            ok, pub = try_call(cs["msp"].write, "POST", f"/api/v1/msp/customers/{T_MFG}/ticket-types/distributions/{dist_id}/publish")
            rec("A17-publish", "OK" if ok else "FAIL", resp=(pub if ok else pub))
            # 解绑 → 转 native
            ok, unb = try_call(cs["msp"].write, "POST", f"/api/v1/msp/customers/{T_MFG}/ticket-types/distributions/{dist_id}/unbind")
            rec("A15-unbind", "OK" if ok else "FAIL", resp=(unb if ok else unb))

    print("SUMMARY: " + json.dumps({
        "mode": "run", "ts": ts, "steps": len(RESULTS), "fail_or_blocked": step_fail,
    }, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
