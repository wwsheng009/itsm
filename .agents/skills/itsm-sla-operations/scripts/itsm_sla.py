#!/usr/bin/env python3
"""ITSM SLA REST API CLI（零依赖，Python 3.8+，仅标准库）。

覆盖 /sla 读面（定义/监控/指标/违约/绩效/合规报告/告警历史/告警规则）+ 违约处置 + 写面核心（定义 CRUD/告警规则 CRUD）。
MSP 专属子面（grants/contracts/settlement-report，需 MSP 部署模式）提供直通子命令。
公共层复用 ../_lib/itsm_common.py，勿在本文件复制。

用法（demo 环境）：
  python scripts/itsm_sla.py login --base http://172.18.3.238:8089/api/v1 \\
      --username demo.mfg.admin --password 'Demo@2026!' --save
  python scripts/itsm_sla.py --base http://172.18.3.238:8089/api/v1 --token-file "$HOME/.itsm/itsm.token" stats
"""
import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "_lib"))
if not (Path(sys.path[0]) / "itsm_common.py").exists():
    sys.path[0] = str(Path(__file__).resolve().parents[2] / "_lib")
from itsm_common import (  # noqa: E402
    add_common_args,
    ApiError,
    authed,
    Client,
    emit,
    fail,
    save_login_token,
)


def _kv(pairs, flag="--set"):
    out = {}
    for item in pairs or []:
        if "=" not in item:
            raise ApiError("%s 需要 key=value 形式: %s" % (flag, item))
        key, value = item.split("=", 1)
        key = key.strip()
        try:
            out[key] = json.loads(value)
        except ValueError:
            out[key] = value
    return out


def _get(client, path, params=None):
    if params:
        from urllib.parse import urlencode
        clean = {k: v for k, v in params.items() if v not in (None, "", [])}
        if clean:
            path = path + "?" + urlencode(clean)
    return client.request("GET", path)


# ------------------------- 子命令实现 -------------------------

def cmd_login(client, args):
    result = client.login(args.username, args.password, args.tenant_code)
    if args.save:
        save_login_token(result, args.save, tool_name="itsm_sla")
    result["ok"] = True
    return result


def cmd_stats(client, _args):
    return client.request("GET", "/sla/stats")


def cmd_definitions(client, args):
    return _get(client, "/sla/definitions", {"page": args.page, "pageSize": args.page_size})


def cmd_definition_get(client, args):
    return client.request("GET", "/sla/definitions/%s" % args.id)


def cmd_definition_create(client, args):
    body = _kv(args.set)
    for key in ("name", "responseTime", "resolutionTime"):
        if key not in body:
            raise ApiError("创建 SLA 定义必填 --set name=... --set responseTime=分钟 --set resolutionTime=分钟")
    return client.request("POST", "/sla/definitions", body)


def cmd_definition_update(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("更新 SLA 定义至少一个 --set key=value")
    return client.request("PUT", "/sla/definitions/%s" % args.id, body)


def cmd_definition_delete(client, args):
    return client.request("DELETE", "/sla/definitions/%s" % args.id)


def cmd_provided(client, _args):
    """customer 面只读：服务商下发的定义（D-SLA-1）。"""
    return client.request("GET", "/sla/definitions/provided")


def cmd_alert_rules(client, _args):
    return client.request("GET", "/sla/alert-rules")


def cmd_alert_rule_get(client, args):
    return client.request("GET", "/sla/alert-rules/%s" % args.id)


def cmd_alert_rule_create(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("创建告警规则至少一个 --set key=value")
    return client.request("POST", "/sla/alert-rules", body)


def cmd_alert_rule_update(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("更新告警规则至少一个 --set key=value")
    return client.request("PUT", "/sla/alert-rules/%s" % args.id, body)


def cmd_alert_rule_delete(client, args):
    return client.request("DELETE", "/sla/alert-rules/%s" % args.id)


def cmd_metrics(client, args):
    return _get(client, "/sla/metrics", {"slaDefinitionId": args.sla_id, "metricType": args.metric_type})


def cmd_violations(client, args):
    return _get(client, "/sla/violations", {
        "isResolved": args.resolved, "severity": args.severity,
        "violationType": args.violation_type, "slaDefinitionId": args.sla_id,
        "page": args.page, "pageSize": args.page_size,
    })


def cmd_violation_update(client, args):
    body = {}
    if args.resolved is not None:
        body["isResolved"] = args.resolved.lower() in ("1", "true", "yes")
    if args.notes:
        body["notes"] = args.notes
    if args.state:
        if args.state != "disputed":
            raise ApiError("本端点仅支持 resolutionState=disputed；accepted/waived 走 violation-settle（需 sla:violation:settle 权限）")
        body["resolutionState"] = "disputed"
        if not args.dispute_reason:
            raise ApiError("disputed 需要 --dispute-reason")
        body["disputeReason"] = args.dispute_reason
    if not body:
        raise ApiError("violation-update 至少一个参数（--resolved/--notes/--state disputed）")
    return client.request("PUT", "/sla/violations/%s" % args.id, body)


def cmd_violation_settle(client, args):
    if args.decision not in ("accepted", "waived"):
        raise ApiError("settle 决策仅 accepted|waived")
    body = {"decision": args.decision}
    if args.reason:
        body["reason"] = args.reason
    return client.request("PUT", "/sla/violations/%s/settle" % args.id, body)


def cmd_monitoring(client, args):
    body = {}
    if args.start:
        body["startTime"] = args.start
    if args.end:
        body["endTime"] = args.end
    return client.request("POST", "/sla/monitoring", body or None)


def cmd_performance(client, args):
    if args.dimension not in ("serviceType", "priority"):
        raise ApiError("dimension 仅 serviceType|priority")
    return _get(client, "/sla/performance", {
        "dimension": args.dimension, "serviceType": args.service_type, "priority": args.priority,
    })


def cmd_check_compliance(client, args):
    return client.request("POST", "/sla/check-compliance/%s" % args.ticket_id, None)


def cmd_alert_history(client, _args):
    return client.request("GET", "/sla/alert-history")


def cmd_compliance_report(client, _args):
    return client.request("GET", "/sla/compliance-report")


def cmd_grants(client, _args):
    return client.request("GET", "/sla/grants")


def cmd_contracts(client, _args):
    return client.request("GET", "/sla/contracts")


def cmd_settlement(client, args):
    return _get(client, "/sla/settlement-report", {
        "customerId": args.customer_id, "periodStart": args.period_start, "periodEnd": args.period_end,
    })


COMMANDS = {
    "login": (cmd_login, False),
    "stats": (cmd_stats, True),
    "definitions": (cmd_definitions, True),
    "definition-get": (cmd_definition_get, True),
    "definition-create": (cmd_definition_create, True),
    "definition-update": (cmd_definition_update, True),
    "definition-delete": (cmd_definition_delete, True),
    "provided": (cmd_provided, True),
    "alert-rules": (cmd_alert_rules, True),
    "alert-rule-get": (cmd_alert_rule_get, True),
    "alert-rule-create": (cmd_alert_rule_create, True),
    "alert-rule-update": (cmd_alert_rule_update, True),
    "alert-rule-delete": (cmd_alert_rule_delete, True),
    "metrics": (cmd_metrics, True),
    "violations": (cmd_violations, True),
    "violation-update": (cmd_violation_update, True),
    "violation-settle": (cmd_violation_settle, True),
    "monitoring": (cmd_monitoring, True),
    "performance": (cmd_performance, True),
    "check-compliance": (cmd_check_compliance, True),
    "alert-history": (cmd_alert_history, True),
    "compliance-report": (cmd_compliance_report, True),
    "grants": (cmd_grants, True),
    "contracts": (cmd_contracts, True),
    "settlement": (cmd_settlement, True),
}


def build_parser():
    parser = argparse.ArgumentParser(prog="itsm_sla.py", description="ITSM SLA REST API CLI（零依赖）")
    add_common_args(parser)
    sub = parser.add_subparsers(dest="command", required=True)
    common_sub = argparse.ArgumentParser(add_help=False)
    add_common_args(common_sub, suppressed=True)

    def add(name, help_text):
        return sub.add_parser(name, help=help_text, parents=[common_sub])

    lg = add("login", "登录换 JWT（从 Set-Cookie 提取 access_token）")
    lg.add_argument("--username", default="demo.mfg.admin")
    lg.add_argument("--password", default="")
    lg.add_argument("--tenant-code", default="")
    lg.add_argument("--save", nargs="?", const=str(Path.home() / ".itsm" / "itsm.token"), default="",
                    help="token 写入文件；省略路径默认 ~/.itsm/itsm.token")

    add("stats", "GET /sla/stats")

    dl = add("definitions", "GET /sla/definitions（定义列表）")
    dl.add_argument("--page", type=int, default=1)
    dl.add_argument("--page-size", type=int, default=20)
    dg = add("definition-get", "GET /sla/definitions/:id")
    dg.add_argument("id")
    dc = add("definition-create", "POST /sla/definitions（--set name/responseTime/resolutionTime 必填）")
    dc.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    du = add("definition-update", "PUT /sla/definitions/:id")
    du.add_argument("id")
    du.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    dd = add("definition-delete", "DELETE /sla/definitions/:id")
    dd.add_argument("id")
    add("provided", "GET /sla/definitions/provided（customer 面：服务商下发定义，D-SLA-1）")

    add("alert-rules", "GET /sla/alert-rules")
    ag = add("alert-rule-get", "GET /sla/alert-rules/:id")
    ag.add_argument("id")
    ac = add("alert-rule-create", "POST /sla/alert-rules")
    ac.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    au = add("alert-rule-update", "PUT /sla/alert-rules/:id")
    au.add_argument("id")
    au.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    ad = add("alert-rule-delete", "DELETE /sla/alert-rules/:id")
    ad.add_argument("id")

    mt = add("metrics", "GET /sla/metrics")
    mt.add_argument("--sla-id", type=int, default=0)
    mt.add_argument("--metric-type", default="")

    vl = add("violations", "GET /sla/violations（分页+过滤）")
    vl.add_argument("--resolved", default="", help="true/false")
    vl.add_argument("--severity", default="")
    vl.add_argument("--violation-type", default="")
    vl.add_argument("--sla-id", type=int, default=0)
    vl.add_argument("--page", type=int, default=1)
    vl.add_argument("--page-size", type=int, default=20)

    vu = add("violation-update", "PUT /sla/violations/:id（isResolved/notes/仅 disputed）")
    vu.add_argument("id")
    vu.add_argument("--resolved", default=None, help="true/false")
    vu.add_argument("--notes", default="")
    vu.add_argument("--state", default="", choices=["", "disputed"])
    vu.add_argument("--dispute-reason", default="")

    vs = add("violation-settle", "PUT /sla/violations/:id/settle（accepted|waived，sla:violation:settle）")
    vs.add_argument("id")
    vs.add_argument("--decision", required=True, choices=["accepted", "waived"])
    vs.add_argument("--reason", default="")

    mo = add("monitoring", "POST /sla/monitoring（startTime/endTime RFC3339 可选）")
    mo.add_argument("--start", default="")
    mo.add_argument("--end", default="")

    pf = add("performance", "GET /sla/performance（维度聚合：serviceType|priority）")
    pf.add_argument("--dimension", required=True)
    pf.add_argument("--service-type", default="")
    pf.add_argument("--priority", default="")

    cc = add("check-compliance", "POST /sla/check-compliance/:ticketId")
    cc.add_argument("ticket_id")

    add("alert-history", "GET /sla/alert-history")
    add("compliance-report", "GET /sla/compliance-report")

    # MSP 专属子面（非 MSP 部署 404，直通）
    add("grants", "GET /sla/grants（MSP 部署模式专用）")
    add("contracts", "GET /sla/contracts（MSP 部署模式专用）")
    st = add("settlement", "GET /sla/settlement-report（MSP 部署模式专用）")
    st.add_argument("--customer-id", type=int, default=0)
    st.add_argument("--period-start", default="")
    st.add_argument("--period-end", default="")

    return parser


def main(argv=None):
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")
    args = build_parser().parse_args(argv)
    pretty = not args.compact
    handler, needs_auth = COMMANDS[args.command]
    try:
        client = authed(args) if needs_auth else Client(args.base, "", args.timeout)
        result = handler(client, args)
        emit(result if result is not None else {"ok": True}, pretty)
        return 0
    except ApiError as err:
        fail(err, pretty)
    except KeyboardInterrupt:
        return 130


if __name__ == "__main__":
    sys.exit(main())
