#!/usr/bin/env python3
"""ITSM MSP（托管服务商）跨租户 REST API CLI（零依赖，Python 3.8+，仅标准库）。

覆盖 /msp：状态/上下文、分配管理、客户管理、跨租户工作台（工单/任务/变更/问题/服务请求）、
工单类型下发、报表与审计聚合。需要 MSP 身份（MSPMiddleware）；非 MSP 用户 403。
公共层复用 ../_lib/itsm_common.py，勿在本文件复制。

用法（demo 环境，MSP 员工账号）：
  python scripts/itsm_msp.py login --base http://172.18.3.238:8089/api/v1 --username <msp-user> --password '...' --save
  python scripts/itsm_msp.py --base http://172.18.3.238:8089/api/v1 --token-file "$HOME/.itsm/itsm.token" status
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
        save_login_token(result, args.save, tool_name="itsm_msp")
    result["ok"] = True
    return result


def cmd_status(client, _args):
    return client.request("GET", "/msp/status")


def cmd_context(client, _args):
    return client.request("GET", "/msp/context")


# ---- 分配管理（msp_allocation:read/write）----

def cmd_allocations(client, args):
    return _get(client, "/msp/allocations", {"page": args.page, "pageSize": args.page_size})


def cmd_allocation_history(client, _args):
    return client.request("GET", "/msp/allocations/history")


def cmd_allocation_create(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("allocation-create 至少一个 --set key=value")
    return client.request("POST", "/msp/allocations", body)


def cmd_deallocate(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("deallocate 至少一个 --set key=value")
    return client.request("POST", "/msp/allocations/deallocate", body)


# ---- 客户管理（msp_customer / msp_ticket:read）----

def cmd_customers(client, _args):
    return client.request("GET", "/msp/customers")


def cmd_customer_tickets(client, args):
    return _get(client, "/msp/customers/%s/tickets" % args.customer_tenant_id,
                {"page": args.page, "pageSize": args.page_size})


def cmd_customer_roles(client, args):
    return client.request("GET", "/msp/customers/%s/roles" % args.customer_tenant_id)


def cmd_workflow_templates(client, args):
    return client.request("GET", "/msp/customers/%s/workflow-templates" % args.customer_tenant_id)


def cmd_workflow_distribute(client, args):
    return client.request("POST", "/msp/customers/%s/workflow-templates/%s/distribute" % (args.customer_tenant_id, args.key), None)


def cmd_tt_distributions(client, args):
    return client.request("GET", "/msp/customers/%s/ticket-types/distributions" % args.customer_tenant_id)


def cmd_tt_distribute(client, args):
    return client.request("POST", "/msp/customers/%s/ticket-types/%s/distribute" % (args.customer_tenant_id, args.id), None)


def cmd_tt_preview(client, args):
    return client.request("GET", "/msp/customers/%s/ticket-types/distributions/%s/preview" % (args.customer_tenant_id, args.dist_id))


def cmd_tt_publish(client, args):
    return client.request("POST", "/msp/customers/%s/ticket-types/distributions/%s/publish" % (args.customer_tenant_id, args.dist_id), None)


def cmd_tt_unbind(client, args):
    return client.request("POST", "/msp/customers/%s/ticket-types/distributions/%s/unbind" % (args.customer_tenant_id, args.dist_id), None)


# ---- 工作台（msp_ticket:read/write）----

def cmd_workbench_tickets(client, args):
    return _get(client, "/msp/workbench/tickets", {
        "page": args.page, "pageSize": args.page_size, "status": args.status,
        "customerTenantId": args.customer_id, "priority": args.priority, "search": args.search,
    })


def cmd_workbench_summary(client, _args):
    return client.request("GET", "/msp/workbench/summary")


def cmd_workbench_ticket_get(client, args):
    return client.request("GET", "/msp/tickets/%s" % args.id)


def cmd_workbench_ticket_comments(client, args):
    return client.request("GET", "/msp/tickets/%s/comments" % args.id)


def cmd_ticket_assign(client, args):
    if not args.technician_id:
        raise ApiError("ticket-assign 需要 --technician-id N")
    return client.request("POST", "/msp/tickets/%s/assign" % args.id, {"technicianId": args.technician_id})


def cmd_ticket_reply(client, args):
    if not args.content:
        raise ApiError("ticket-reply 需要 --content")
    body = {"content": args.content}
    if args.internal is not None:
        body["isInternal"] = args.internal.lower() in ("1", "true", "yes")
    return client.request("POST", "/msp/tickets/%s/reply" % args.id, body)


def cmd_ticket_status(client, args):
    if not args.status:
        raise ApiError("ticket-status 需要 --status")
    return client.request("POST", "/msp/tickets/%s/status" % args.id, {"status": args.status})


def cmd_workbench_batch(client, args):
    body = _kv(args.set)
    if "ticketIds" not in body or "action" not in body:
        raise ApiError("batch 需要 --set ticketIds=[1,2,...] --set action=<低危白名单动作>（≤100 条）")
    return client.request("POST", "/msp/workbench/batch", body)


def cmd_workbench_views(client, _args):
    return client.request("GET", "/msp/workbench/views")


def cmd_workbench_view_create(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("view-create 至少一个 --set key=value")
    return client.request("POST", "/msp/workbench/views", body)


def cmd_workbench_view_update(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("view-update 至少一个 --set key=value")
    return client.request("PUT", "/msp/workbench/views/%s" % args.id, body)


def cmd_workbench_view_delete(client, args):
    return client.request("DELETE", "/msp/workbench/views/%s" % args.id)


def cmd_workbench_view_default(client, args):
    return client.request("POST", "/msp/workbench/views/%s/default" % args.id, None)


# ---- 任务投影 ----

def cmd_workbench_tasks(client, args):
    return _get(client, "/msp/workbench/tasks", {"page": args.page, "pageSize": args.page_size})


def cmd_task_claim(client, args):
    return client.request("POST", "/msp/workbench/tasks/%s/claim" % args.task_id, None)


def cmd_task_complete(client, args):
    body = _kv(args.set)
    return client.request("POST", "/msp/workbench/tasks/%s/complete" % args.task_id, body or None)


# ---- 变更/问题/服务请求接入 ----

def cmd_workbench_changes(client, args):
    return _get(client, "/msp/workbench/changes", {"page": args.page, "pageSize": args.page_size})


def cmd_workbench_problems(client, args):
    return _get(client, "/msp/workbench/problems", {"page": args.page, "pageSize": args.page_size})


def cmd_workbench_service_requests(client, args):
    return _get(client, "/msp/workbench/service-requests", {"page": args.page, "pageSize": args.page_size})


def cmd_change_get(client, args):
    return client.request("GET", "/msp/changes/%s" % args.id)


def cmd_problem_get(client, args):
    return client.request("GET", "/msp/problems/%s" % args.id)


def cmd_service_request_get(client, args):
    return client.request("GET", "/msp/service-requests/%s" % args.id)


def cmd_problem_status(client, args):
    if not args.status:
        raise ApiError("problem-status 需要 --status")
    return client.request("POST", "/msp/problems/%s/status" % args.id, {"status": args.status})


def cmd_change_status(client, args):
    if not args.status:
        raise ApiError("change-status 需要 --status")
    return client.request("POST", "/msp/changes/%s/status" % args.id, {"status": args.status})


def cmd_service_request_status(client, args):
    if not args.status:
        raise ApiError("service-request-status 需要 --status")
    return client.request("POST", "/msp/service-requests/%s/status" % args.id, {"status": args.status})


# ---- 报表 / 审计 ----

def cmd_reports_customers(client, args):
    if not args.start_date or not args.end_date:
        raise ApiError("reports-customers 需要 --start-date YYYY-MM-DD --end-date YYYY-MM-DD（服务端必填）")
    return _get(client, "/msp/reports/customers", {
        "startDate": args.start_date, "endDate": args.end_date,
        "customerTenantId": args.customer_id,
    })


def cmd_reports_performance(client, args):
    if not args.start_date or not args.end_date:
        raise ApiError("reports-performance 需要 --start-date YYYY-MM-DD --end-date YYYY-MM-DD（服务端必填）")
    return _get(client, "/msp/reports/performance", {
        "startDate": args.start_date, "endDate": args.end_date,
        "customerTenantId": args.customer_id,
    })


def cmd_audit_summary(client, _args):
    return client.request("GET", "/msp/audit/summary")


COMMANDS = {
    "login": (cmd_login, False),
    "status": (cmd_status, True),
    "context": (cmd_context, True),
    "allocations": (cmd_allocations, True),
    "allocation-history": (cmd_allocation_history, True),
    "allocation-create": (cmd_allocation_create, True),
    "deallocate": (cmd_deallocate, True),
    "customers": (cmd_customers, True),
    "customer-tickets": (cmd_customer_tickets, True),
    "customer-roles": (cmd_customer_roles, True),
    "workflow-templates": (cmd_workflow_templates, True),
    "workflow-distribute": (cmd_workflow_distribute, True),
    "tt-distributions": (cmd_tt_distributions, True),
    "tt-distribute": (cmd_tt_distribute, True),
    "tt-preview": (cmd_tt_preview, True),
    "tt-publish": (cmd_tt_publish, True),
    "tt-unbind": (cmd_tt_unbind, True),
    "workbench-tickets": (cmd_workbench_tickets, True),
    "workbench-summary": (cmd_workbench_summary, True),
    "workbench-ticket-get": (cmd_workbench_ticket_get, True),
    "workbench-ticket-comments": (cmd_workbench_ticket_comments, True),
    "ticket-assign": (cmd_ticket_assign, True),
    "ticket-reply": (cmd_ticket_reply, True),
    "ticket-status": (cmd_ticket_status, True),
    "workbench-batch": (cmd_workbench_batch, True),
    "workbench-views": (cmd_workbench_views, True),
    "workbench-view-create": (cmd_workbench_view_create, True),
    "workbench-view-update": (cmd_workbench_view_update, True),
    "workbench-view-delete": (cmd_workbench_view_delete, True),
    "workbench-view-default": (cmd_workbench_view_default, True),
    "workbench-tasks": (cmd_workbench_tasks, True),
    "task-claim": (cmd_task_claim, True),
    "task-complete": (cmd_task_complete, True),
    "workbench-changes": (cmd_workbench_changes, True),
    "workbench-problems": (cmd_workbench_problems, True),
    "workbench-service-requests": (cmd_workbench_service_requests, True),
    "change-get": (cmd_change_get, True),
    "problem-get": (cmd_problem_get, True),
    "service-request-get": (cmd_service_request_get, True),
    "problem-status": (cmd_problem_status, True),
    "change-status": (cmd_change_status, True),
    "service-request-status": (cmd_service_request_status, True),
    "reports-customers": (cmd_reports_customers, True),
    "reports-performance": (cmd_reports_performance, True),
    "audit-summary": (cmd_audit_summary, True),
}


def build_parser():
    parser = argparse.ArgumentParser(prog="itsm_msp.py", description="ITSM MSP 跨租户 REST API CLI（零依赖）")
    add_common_args(parser)
    sub = parser.add_subparsers(dest="command", required=True)
    common_sub = argparse.ArgumentParser(add_help=False)
    add_common_args(common_sub, suppressed=True)

    def add(name, help_text):
        return sub.add_parser(name, help=help_text, parents=[common_sub])

    lg = add("login", "登录换 JWT（从 Set-Cookie 提取 access_token）")
    lg.add_argument("--username", default="")
    lg.add_argument("--password", default="")
    lg.add_argument("--tenant-code", default="")
    lg.add_argument("--save", nargs="?", const=str(Path.home() / ".itsm" / "itsm.token"), default="",
                    help="token 写入文件；省略路径默认 ~/.itsm/itsm.token")

    add("status", "GET /msp/status（MSP 员工与基础信息）")
    add("context", "GET /msp/context（msp:read）")

    al = add("allocations", "GET /msp/allocations")
    al.add_argument("--page", type=int, default=1)
    al.add_argument("--page-size", type=int, default=20)
    add("allocation-history", "GET /msp/allocations/history（含已解除）")
    ac = add("allocation-create", "POST /msp/allocations")
    ac.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    da = add("deallocate", "POST /msp/allocations/deallocate")
    da.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")

    add("customers", "GET /msp/customers")
    ct = add("customer-tickets", "GET /msp/customers/:ctid/tickets")
    ct.add_argument("customer_tenant_id")
    ct.add_argument("--page", type=int, default=1)
    ct.add_argument("--page-size", type=int, default=20)
    cr = add("customer-roles", "GET /msp/customers/:ctid/roles（建号表单角色候选）")
    cr.add_argument("customer_tenant_id")

    wt = add("workflow-templates", "GET /msp/customers/:ctid/workflow-templates")
    wt.add_argument("customer_tenant_id")
    wd = add("workflow-distribute", "POST /msp/customers/:ctid/workflow-templates/:key/distribute")
    wd.add_argument("customer_tenant_id")
    wd.add_argument("key")

    td = add("tt-distributions", "GET /msp/customers/:ctid/ticket-types/distributions")
    td.add_argument("customer_tenant_id")
    tdist = add("tt-distribute", "POST /msp/customers/:ctid/ticket-types/:id/distribute（D-TT-1）")
    tdist.add_argument("customer_tenant_id")
    tdist.add_argument("id")
    tpre = add("tt-preview", "GET /msp/customers/:ctid/ticket-types/distributions/:distId/preview")
    tpre.add_argument("customer_tenant_id")
    tpre.add_argument("dist_id")
    tpub = add("tt-publish", "POST /msp/customers/:ctid/ticket-types/distributions/:distId/publish")
    tpub.add_argument("customer_tenant_id")
    tpub.add_argument("dist_id")
    tun = add("tt-unbind", "POST /msp/customers/:ctid/ticket-types/distributions/:distId/unbind")
    tun.add_argument("customer_tenant_id")
    tun.add_argument("dist_id")

    wbt = add("workbench-tickets", "GET /msp/workbench/tickets（跨客户工单）")
    wbt.add_argument("--page", type=int, default=1)
    wbt.add_argument("--page-size", type=int, default=20)
    wbt.add_argument("--status", default="")
    wbt.add_argument("--customer-id", type=int, default=0)
    wbt.add_argument("--priority", default="")
    wbt.add_argument("--search", default="")
    add("workbench-summary", "GET /msp/workbench/summary")
    wtg = add("workbench-ticket-get", "GET /msp/tickets/:id（跨租户详情，双重校验）")
    wtg.add_argument("id")
    wtc = add("workbench-ticket-comments", "GET /msp/tickets/:id/comments（客户内部备注不下发）")
    wtc.add_argument("id")

    ta = add("ticket-assign", "POST /msp/tickets/:id/assign")
    ta.add_argument("id")
    ta.add_argument("--technician-id", type=int, required=True)
    tr = add("ticket-reply", "POST /msp/tickets/:id/reply")
    tr.add_argument("id")
    tr.add_argument("--content", required=True)
    tr.add_argument("--internal", default=None, help="true/false（isInternal）")
    ts = add("ticket-status", "POST /msp/tickets/:id/status")
    ts.add_argument("id")
    ts.add_argument("--status", required=True)

    wb = add("workbench-batch", "POST /msp/workbench/batch（≤100 条，低危动作白名单）")
    wb.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")

    add("workbench-views", "GET /msp/workbench/views（灰度 WORKBENCH_VIEWS_ENABLED）")
    wvc = add("workbench-view-create", "POST /msp/workbench/views")
    wvc.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    wvu = add("workbench-view-update", "PUT /msp/workbench/views/:id")
    wvu.add_argument("id")
    wvu.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    wvd = add("workbench-view-delete", "DELETE /msp/workbench/views/:id")
    wvd.add_argument("id")
    wvdf = add("workbench-view-default", "POST /msp/workbench/views/:id/default")
    wvdf.add_argument("id")

    tk = add("workbench-tasks", "GET /msp/workbench/tasks（跨租户任务投影）")
    tk.add_argument("--page", type=int, default=1)
    tk.add_argument("--page-size", type=int, default=20)
    tc = add("task-claim", "POST /msp/workbench/tasks/:taskId/claim")
    tc.add_argument("task_id")
    tcp = add("task-complete", "POST /msp/workbench/tasks/:taskId/complete")
    tcp.add_argument("task_id")
    tcp.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")

    wch = add("workbench-changes", "GET /msp/workbench/changes")
    wch.add_argument("--page", type=int, default=1)
    wch.add_argument("--page-size", type=int, default=20)
    wpb = add("workbench-problems", "GET /msp/workbench/problems")
    wpb.add_argument("--page", type=int, default=1)
    wpb.add_argument("--page-size", type=int, default=20)
    wsr = add("workbench-service-requests", "GET /msp/workbench/service-requests")
    wsr.add_argument("--page", type=int, default=1)
    wsr.add_argument("--page-size", type=int, default=20)

    cg = add("change-get", "GET /msp/changes/:id")
    cg.add_argument("id")
    pg = add("problem-get", "GET /msp/problems/:id")
    pg.add_argument("id")
    sg = add("service-request-get", "GET /msp/service-requests/:id")
    sg.add_argument("id")

    ps = add("problem-status", "POST /msp/problems/:id/status（处理类动作）")
    ps.add_argument("id")
    ps.add_argument("--status", required=True)
    cs = add("change-status", "POST /msp/changes/:id/status")
    cs.add_argument("id")
    cs.add_argument("--status", required=True)
    ss = add("service-request-status", "POST /msp/service-requests/:id/status")
    ss.add_argument("id")
    ss.add_argument("--status", required=True)

    rc = add("reports-customers", "GET /msp/reports/customers（startDate/endDate 必填）")
    rc.add_argument("--start-date", required=True)
    rc.add_argument("--end-date", required=True)
    rc.add_argument("--customer-id", type=int, default=0)
    rp = add("reports-performance", "GET /msp/reports/performance（startDate/endDate 必填）")
    rp.add_argument("--start-date", required=True)
    rp.add_argument("--end-date", required=True)
    rp.add_argument("--customer-id", type=int, default=0)
    add("audit-summary", "GET /msp/audit/summary（越权尝试/冲突告警面板）")

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
