#!/usr/bin/env python3
"""ITSM 问题管理 REST API CLI（零依赖，Python 3.8+，仅标准库）。

覆盖 /problems：CRUD、调查/根因/解决方案/关闭、关联管理（ticket/incident/change）、评论、统计/趋势/热点。
公共层复用 ../_lib/itsm_common.py，勿在本文件复制。

用法（demo 环境）：
  python scripts/itsm_problem.py login --base http://172.18.3.238:8089/api/v1 \\
      --username demo.mfg.admin --password 'Demo@2026!' --save
  python scripts/itsm_problem.py --base http://172.18.3.238:8089/api/v1 --token-file "$HOME/.itsm/itsm.token" stats
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
        save_login_token(result, args.save, tool_name="itsm_problem")
    result["ok"] = True
    return result


def cmd_list(client, args):
    return _get(client, "/problems", {
        "page": args.page, "pageSize": args.page_size, "status": args.status,
        "priority": args.priority, "category": args.category, "keyword": args.keyword,
        "sortBy": args.sort_by, "sortOrder": args.sort_order,
    })


def cmd_get(client, args):
    return client.request("GET", "/problems/%s" % args.id)


def cmd_create(client, args):
    body = _kv(args.set)
    for key in ("title", "description", "priority"):
        if key not in body:
            raise ApiError("创建问题必填 --set title=... --set description=(≥10字符) --set priority=low|medium|high|critical")
    return client.request("POST", "/problems", body)


def cmd_update(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("更新问题至少一个 --set key=value")
    return client.request("PUT", "/problems/%s" % args.id, body)


def cmd_delete(client, args):
    return client.request("DELETE", "/problems/%s" % args.id)


def cmd_stats(client, _args):
    return client.request("GET", "/problems/stats")


def cmd_trend(client, _args):
    return client.request("GET", "/problems/trend")


def cmd_hotspots(client, _args):
    return client.request("GET", "/problems/hotspots")


def cmd_investigate(client, args):
    return client.request("POST", "/problems/%s/investigate" % args.id, None)


def cmd_root_cause(client, args):
    if not args.root_cause:
        raise ApiError("root-cause 需要 --root-cause 文本（required）")
    return client.request("PUT", "/problems/%s/root-cause" % args.id, {"rootCause": args.root_cause})


def cmd_solution(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("solution 至少一个 --set（solutionType/solutionDescription/estimatedEffortHours…）")
    return client.request("PUT", "/problems/%s/solution" % args.id, body)


def cmd_close(client, args):
    body = {}
    if args.resolution:
        body["resolution"] = args.resolution
    return client.request("POST", "/problems/%s/close" % args.id, body)


def cmd_sla(client, args):
    return client.request("GET", "/problems/%s/sla" % args.id)


def cmd_comments(client, args):
    return client.request("GET", "/problems/%s/comments" % args.id)


def cmd_comment_add(client, args):
    if not args.content:
        raise ApiError("comment-add 需要 --content")
    return client.request("POST", "/problems/%s/comments" % args.id, {"content": args.content})


def cmd_associations(client, args):
    return client.request("GET", "/problems/%s/associations" % args.id)


def cmd_association_add(client, args):
    if args.related_type not in ("ticket", "incident", "change"):
        raise ApiError("related-type 仅 ticket|incident|change")
    try:
        ids = [int(x) for x in args.related_ids.split(",") if x.strip()]
    except ValueError:
        raise ApiError("--related-ids 需为逗号分隔数字")
    if not ids:
        raise ApiError("--related-ids 至少一个")
    return client.request("POST", "/problems/%s/associations" % args.id,
                          {"relatedType": args.related_type, "relatedIds": ids})


def cmd_association_remove(client, args):
    if args.related_type not in ("ticket", "incident", "change"):
        raise ApiError("related-type 仅 ticket|incident|change")
    return client.request("DELETE", "/problems/%s/associations" % args.id,
                          {"relatedType": args.related_type, "relatedId": args.related_id})


COMMANDS = {
    "login": (cmd_login, False),
    "list": (cmd_list, True),
    "get": (cmd_get, True),
    "create": (cmd_create, True),
    "update": (cmd_update, True),
    "delete": (cmd_delete, True),
    "stats": (cmd_stats, True),
    "trend": (cmd_trend, True),
    "hotspots": (cmd_hotspots, True),
    "investigate": (cmd_investigate, True),
    "root-cause": (cmd_root_cause, True),
    "solution": (cmd_solution, True),
    "close": (cmd_close, True),
    "sla": (cmd_sla, True),
    "comments": (cmd_comments, True),
    "comment-add": (cmd_comment_add, True),
    "associations": (cmd_associations, True),
    "association-add": (cmd_association_add, True),
    "association-remove": (cmd_association_remove, True),
}


def build_parser():
    parser = argparse.ArgumentParser(prog="itsm_problem.py", description="ITSM 问题管理 REST API CLI（零依赖）")
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

    ls = add("list", "GET /problems（分页+过滤）")
    ls.add_argument("--page", type=int, default=1)
    ls.add_argument("--page-size", type=int, default=20)
    ls.add_argument("--status", default="")
    ls.add_argument("--priority", default="")
    ls.add_argument("--category", default="")
    ls.add_argument("--keyword", default="")
    ls.add_argument("--sort-by", default="")
    ls.add_argument("--sort-order", default="", choices=["", "asc", "desc"])

    g = add("get", "GET /problems/:id")
    g.add_argument("id")

    cr = add("create", "POST /problems（--set title/description/priority 必填）")
    cr.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")

    up = add("update", "PUT /problems/:id（指针字段；status 走状态机校验）")
    up.add_argument("id")
    up.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")

    de = add("delete", "DELETE /problems/:id")
    de.add_argument("id")

    add("stats", "GET /problems/stats")
    add("trend", "GET /problems/trend")
    add("hotspots", "GET /problems/hotspots")

    iv = add("investigate", "POST /problems/:id/investigate（open→investigating，无 body）")
    iv.add_argument("id")

    rc = add("root-cause", "PUT /problems/:id/root-cause（rootCause 必填）")
    rc.add_argument("id")
    rc.add_argument("--root-cause", required=True)

    so = add("solution", "PUT /problems/:id/solution（--set key=value）")
    so.add_argument("id")
    so.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")

    cl = add("close", "POST /problems/:id/close（--resolution 可选；resolved 才可 close）")
    cl.add_argument("id")
    cl.add_argument("--resolution", default="")

    sl = add("sla", "GET /problems/:id/sla")
    sl.add_argument("id")

    cm = add("comments", "GET /problems/:id/comments")
    cm.add_argument("id")
    ca = add("comment-add", "POST /problems/:id/comments")
    ca.add_argument("id")
    ca.add_argument("--content", required=True)

    asl = add("associations", "GET /problems/:id/associations")
    asl.add_argument("id")
    asa = add("association-add", "POST /problems/:id/associations（relatedType+relatedIds）")
    asa.add_argument("id")
    asa.add_argument("--related-type", required=True, choices=["ticket", "incident", "change"])
    asa.add_argument("--related-ids", required=True, help="逗号分隔数字")
    asr = add("association-remove", "DELETE /problems/:id/associations（body）")
    asr.add_argument("id")
    asr.add_argument("--related-type", required=True, choices=["ticket", "incident", "change"])
    asr.add_argument("--related-id", type=int, required=True)

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
