#!/usr/bin/env python3
"""ITSM 变更管理 REST API CLI（零依赖，Python 3.8+，仅标准库）。

覆盖 /changes 读面 + 状态流转 + 审批 + 风险 + PIR + 日历；CAB 成员管理。
公共层复用 ../_lib/itsm_common.py（认证/输出/错误/token 落盘），勿在本文件复制。

用法（demo 环境）：
  python scripts/itsm_change.py login --base http://172.18.3.238:8089/api/v1 \\
      --username demo.mfg.admin --password 'Demo@2026!' --save
  python scripts/itsm_change.py --base http://172.18.3.238:8089/api/v1 --token-file "$HOME/.itsm/itsm.token" list
"""
import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "_lib"))  # 分发态：单技能包内嵌 _lib
if not (Path(sys.path[0]) / "itsm_common.py").exists():
    sys.path[0] = str(Path(__file__).resolve().parents[2] / "_lib")  # 开发态：仓库 skills 根 _lib
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
    """key=value → dict；value 尝试 JSON 解析（数字/布尔/数组/对象），失败按字符串。"""
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


# ------------------------- 子命令实现 -------------------------

def cmd_login(client, args):
    result = client.login(args.username, args.password, args.tenant_code)
    if args.save:
        save_login_token(result, args.save, tool_name="itsm_change")
    result["ok"] = True
    return result


def _get_list(client, path, params):
    clean = {k: v for k, v in params.items() if v not in (None, "", [])}
    if clean:
        from urllib.parse import urlencode
        path = path + "?" + urlencode(clean)
    return client.request("GET", path)


def cmd_list(client, args):
    return _get_list(client, "/changes", {
        "page": args.page, "pageSize": args.page_size, "status": args.status,
        "search": args.search, "riskLevel": args.risk_level,
    })


def cmd_get(client, args):
    return client.request("GET", "/changes/%s" % args.id)


def cmd_stats(client, _args):
    return client.request("GET", "/changes/stats")


def cmd_create(client, args):
    body = _kv(args.set)
    if "title" not in body:
        raise ApiError("创建变更必填 --set title=...")
    return client.request("POST", "/changes", body)


def cmd_update(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("更新变更至少一个 --set key=value")
    return client.request("PUT", "/changes/%s" % args.id, body)


def cmd_delete(client, args):
    return client.request("DELETE", "/changes/%s" % args.id)


def cmd_submit(client, args):
    body = {}
    if args.approver_ids:
        try:
            body["approverIds"] = [int(x) for x in args.approver_ids.split(",") if x.strip()]
        except ValueError:
            raise ApiError("--approver-ids 需为逗号分隔数字")
    if args.comment:
        body["comment"] = args.comment
    return client.request("POST", "/changes/%s/submit" % args.id, body)


def cmd_assign(client, args):
    if not args.assignee_id:
        raise ApiError("assign 需要 --assignee-id N")
    return client.request("POST", "/changes/%s/assign" % args.id, {"assigneeId": args.assignee_id})


def _transition(client, args, action):
    body = {}
    if args.comment:
        body["comment"] = args.comment
    if args.reason:
        body["reason"] = args.reason
    return client.request("POST", "/changes/%s/%s" % (args.id, action), body)


def cmd_approve(client, args):
    return _transition(client, args, "approve")


def cmd_reject(client, args):
    return _transition(client, args, "reject")


def cmd_schedule(client, args):
    return _transition(client, args, "schedule")


def cmd_start(client, args):
    return _transition(client, args, "start")


def cmd_complete(client, args):
    return _transition(client, args, "complete")


def cmd_close(client, args):
    return _transition(client, args, "close")


def cmd_rollback(client, args):
    return _transition(client, args, "rollback")


def cmd_cancel(client, args):
    return _transition(client, args, "cancel")


def cmd_approvals(client, args):
    return client.request("GET", "/changes/%s/approvals" % args.id)


def cmd_approval_submit(client, args):
    body = {"changeId": args.id, "approverId": args.approver_id}
    if args.comment:
        body["comment"] = args.comment
    return client.request("POST", "/changes/%s/approvals" % args.id, body)


def cmd_approval_summary(client, args):
    return client.request("GET", "/changes/%s/approval-summary" % args.id)


def cmd_risk_get(client, args):
    return client.request("GET", "/changes/%s/risk" % args.id)


def cmd_risk_update(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("risk-update 至少一个 --set key=value（riskLevel/riskDescription/impactAnalysis/mitigationMeasures/contingencyPlan/riskOwner/riskReviewDate）")
    return client.request("PUT", "/changes/%s/risk" % args.id, body)


def cmd_cmdb_impact(client, args):
    return client.request("GET", "/changes/%s/cmdb-impact" % args.id)


def cmd_calendar(client, args):
    if not args.start_date or not args.end_date:
        raise ApiError("calendar 需要 --start-date YYYY-MM-DD --end-date YYYY-MM-DD")
    return _get_list(client, "/changes/calendar", {
        "startDate": args.start_date, "endDate": args.end_date, "status": args.status,
    })


def cmd_pirs(client, _args):
    return client.request("GET", "/changes/pirs")


def cmd_pir_get(client, args):
    return client.request("GET", "/changes/%s/pir" % args.id)


def cmd_pir_create(client, args):
    body = _kv(args.set)
    body["changeId"] = int(args.id)  # DTO 要求 int；字符串会 400
    if "overallResult" not in body:
        raise ApiError("pir-create 必填 --set overallResult=successful|partially_successful|failed")
    return client.request("POST", "/changes/%s/pir" % args.id, body)


def cmd_pir_update(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("pir-update 至少一个 --set key=value")
    return client.request("PUT", "/changes/pir/%s" % args.id, body)


def cmd_pir_delete(client, args):
    return client.request("DELETE", "/changes/pir/%s" % args.id)


def cmd_cab_members(client, _args):
    return client.request("GET", "/cab/members")


def cmd_cab_add(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("cab-add 至少一个 --set key=value")
    return client.request("POST", "/cab/members", body)


def cmd_cab_update(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("cab-update 至少一个 --set key=value")
    return client.request("PUT", "/cab/members/%s" % args.id, body)


def cmd_cab_remove(client, args):
    return client.request("DELETE", "/cab/members/%s" % args.id)


COMMANDS = {
    "login": (cmd_login, False),
    "list": (cmd_list, True),
    "get": (cmd_get, True),
    "stats": (cmd_stats, True),
    "create": (cmd_create, True),
    "update": (cmd_update, True),
    "delete": (cmd_delete, True),
    "submit": (cmd_submit, True),
    "assign": (cmd_assign, True),
    "approve": (cmd_approve, True),
    "reject": (cmd_reject, True),
    "schedule": (cmd_schedule, True),
    "start": (cmd_start, True),
    "complete": (cmd_complete, True),
    "close": (cmd_close, True),
    "rollback": (cmd_rollback, True),
    "cancel": (cmd_cancel, True),
    "approvals": (cmd_approvals, True),
    "approval-submit": (cmd_approval_submit, True),
    "approval-summary": (cmd_approval_summary, True),
    "risk-get": (cmd_risk_get, True),
    "risk-update": (cmd_risk_update, True),
    "cmdb-impact": (cmd_cmdb_impact, True),
    "calendar": (cmd_calendar, True),
    "pirs": (cmd_pirs, True),
    "pir-get": (cmd_pir_get, True),
    "pir-create": (cmd_pir_create, True),
    "pir-update": (cmd_pir_update, True),
    "pir-delete": (cmd_pir_delete, True),
    "cab-members": (cmd_cab_members, True),
    "cab-add": (cmd_cab_add, True),
    "cab-update": (cmd_cab_update, True),
    "cab-remove": (cmd_cab_remove, True),
}


def build_parser():
    parser = argparse.ArgumentParser(prog="itsm_change.py", description="ITSM 变更管理 REST API CLI（零依赖）")
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
    lg.add_argument(
        "--save",
        nargs="?",
        const=str(Path.home() / ".itsm" / "itsm.token"),
        default="",
        help="token 写入文件；省略路径默认 ~/.itsm/itsm.token",
    )

    ls = add("list", "GET /changes（page/pageSize/status/search/riskLevel）")
    ls.add_argument("--page", type=int, default=1)
    ls.add_argument("--page-size", type=int, default=10)
    ls.add_argument("--status", default="")
    ls.add_argument("--search", default="")
    ls.add_argument("--risk-level", default="")

    g = add("get", "GET /changes/:id")
    g.add_argument("id")
    add("stats", "GET /changes/stats")

    cr = add("create", "POST /changes（--set key=value；value 支持 JSON 字面量）")
    cr.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")

    up = add("update", "PUT /changes/:id（指针字段部分更新）")
    up.add_argument("id")
    up.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")

    de = add("delete", "DELETE /changes/:id")
    de.add_argument("id")

    su = add("submit", "POST /changes/:id/submit（提交审批：approverIds 可选）")
    su.add_argument("id")
    su.add_argument("--approver-ids", default="", help="逗号分隔审批人 id")
    su.add_argument("--comment", default="")

    asg = add("assign", "POST /changes/:id/assign")
    asg.add_argument("id")
    asg.add_argument("--assignee-id", type=int, required=True)

    def transition(name, help_text):
        t = add(name, "POST /changes/:id/%s" % name)
        t.add_argument("id")
        t.add_argument("--comment", default="")
        t.add_argument("--reason", default="")

    transition("approve", "批准（change:approve 权限，须为链上审批人）")
    transition("reject", "拒绝（change:approve 权限）")
    transition("schedule", "排期")
    transition("start", "开始实施")
    transition("complete", "完成")
    transition("close", "关闭")
    transition("rollback", "回滚（change:rollback 权限）")
    transition("cancel", "取消")

    ap = add("approvals", "GET /changes/:id/approvals")
    ap.add_argument("id")
    aps = add("approval-submit", "POST /changes/:id/approvals（登记审批记录）")
    aps.add_argument("id")
    aps.add_argument("--approver-id", type=int, required=True)
    aps.add_argument("--comment", default="")
    apsum = add("approval-summary", "GET /changes/:id/approval-summary")
    apsum.add_argument("id")

    rg = add("risk-get", "GET /changes/:id/risk")
    rg.add_argument("id")
    ru = add("risk-update", "PUT /changes/:id/risk")
    ru.add_argument("id")
    ru.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")

    ci = add("cmdb-impact", "GET /changes/:id/cmdb-impact（CMDB 影响面汇总）")
    ci.add_argument("id")

    ca = add("calendar", "GET /changes/calendar（变更日历）")
    ca.add_argument("--start-date", required=True)
    ca.add_argument("--end-date", required=True)
    ca.add_argument("--status", default="")

    add("pirs", "GET /changes/pirs（PIR 列表）")
    pg = add("pir-get", "GET /changes/:id/pir")
    pg.add_argument("id")
    pc = add("pir-create", "POST /changes/:id/pir（实施后评审）")
    pc.add_argument("id")
    pc.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    pu = add("pir-update", "PUT /changes/pir/:id")
    pu.add_argument("id")
    pu.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    pd = add("pir-delete", "DELETE /changes/pir/:id")
    pd.add_argument("id")

    add("cab-members", "GET /cab/members")
    cb_add = add("cab-add", "POST /cab/members")
    cb_add.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    cb_up = add("cab-update", "PUT /cab/members/:id")
    cb_up.add_argument("id")
    cb_up.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    cb_rm = add("cab-remove", "DELETE /cab/members/:id")
    cb_rm.add_argument("id")

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
