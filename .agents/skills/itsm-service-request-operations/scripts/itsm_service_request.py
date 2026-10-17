#!/usr/bin/env python3
"""ITSM 服务请求 + 服务目录 REST API CLI（零依赖，Python 3.8+，仅标准库）。

覆盖 /service-requests（CRUD/审批/开通任务）与 /service-catalogs（目录 CRUD/搜索/统计）。
多级审批状态机：submitted → manager_approved → it_approved → security_approved → provisioning → delivered；
rejected/cancelled 终态；审批动作独立权限 service_request:approve。
公共层复用 ../_lib/itsm_common.py，勿在本文件复制。

用法（demo 环境）：
  python scripts/itsm_service_request.py login --base http://172.18.3.238:8089/api/v1 \\
      --username demo.mfg.admin --password 'Demo@2026!' --save
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


# ------------------------- service-requests -------------------------

def cmd_login(client, args):
    result = client.login(args.username, args.password, args.tenant_code)
    if args.save:
        save_login_token(result, args.save, tool_name="itsm_service_request")
    result["ok"] = True
    return result


def cmd_list(client, args):
    return _get(client, "/service-requests", {
        "page": args.page, "size": args.size, "status": args.status, "scope": args.scope or None,
    })


def cmd_pending(client, args):
    return _get(client, "/service-requests/approvals/pending", {"page": args.page, "size": args.size})


def cmd_get(client, args):
    return client.request("GET", "/service-requests/%s" % args.id)


def cmd_create(client, args):
    body = _kv(args.set)
    if not body.get("catalogId"):
        # 实测（2026-10-17）：服务端要求 catalogId 必填——自由请求也必须挂目录条目
        raise ApiError("创建服务请求必填 --set catalogId=N（服务端校验 catalogId is required；title/reason 可选）")
    return client.request("POST", "/service-requests", body)


def cmd_update(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("更新服务请求至少一个 --set key=value")
    return client.request("PUT", "/service-requests/%s" % args.id, body)


def cmd_delete(client, args):
    return client.request("DELETE", "/service-requests/%s" % args.id)


def cmd_status(client, args):
    if not args.status:
        raise ApiError("status 需要 --status（submitted/manager_approved/it_approved/security_approved/provisioning/delivered/failed/rejected/cancelled）")
    return client.request("PUT", "/service-requests/%s/status" % args.id, {"status": args.status})


def cmd_approvals(client, args):
    return client.request("GET", "/service-requests/%s/approvals" % args.id)


def cmd_approval(client, args):
    if args.action not in ("approve", "reject"):
        raise ApiError("approval 动作仅 approve|reject（service_request:approve 权限）")
    body = {"action": args.action}
    if args.comment:
        body["comment"] = args.comment
    return client.request("POST", "/service-requests/%s/approval" % args.id, body)


def cmd_provision(client, args):
    return client.request("POST", "/service-requests/%s/provision" % args.id, None)


def cmd_provisioning_tasks(client, args):
    return client.request("GET", "/service-requests/%s/provisioning-tasks" % args.id)


def cmd_task_execute(client, args):
    return client.request("POST", "/provisioning-tasks/%s/execute" % args.id, None)


# ------------------------- service-catalogs -------------------------

def cmd_catalog_list(client, args):
    return _get(client, "/service-catalogs", {"page": args.page, "size": args.size})


def cmd_catalog_get(client, args):
    return client.request("GET", "/service-catalogs/%s" % args.id)


def cmd_catalog_search(client, args):
    if not args.keyword:
        raise ApiError("catalog-search 需要 --keyword")
    return _get(client, "/service-catalogs/search", {"keyword": args.keyword})


def cmd_catalog_stats(client, _args):
    return client.request("GET", "/service-catalogs/stats")


def cmd_catalog_create(client, args):
    body = _kv(args.set)
    for key in ("name", "category"):
        if key not in body:
            raise ApiError("catalog-create 必填 --set name=... --set category=...（serviceType 可选 vm|rds|oss|network|storage|security|custom）")
    return client.request("POST", "/service-catalogs", body)


def cmd_catalog_update(client, args):
    body = _kv(args.set)
    if not body:
        raise ApiError("catalog-update 至少一个 --set")
    return client.request("PUT", "/service-catalogs/%s" % args.id, body)


def cmd_catalog_delete(client, args):
    return client.request("DELETE", "/service-catalogs/%s" % args.id)


def cmd_catalog_services(client, _args):
    return client.request("GET", "/service-catalog-services")


COMMANDS = {
    "login": (cmd_login, False),
    "list": (cmd_list, True),
    "pending": (cmd_pending, True),
    "get": (cmd_get, True),
    "create": (cmd_create, True),
    "update": (cmd_update, True),
    "delete": (cmd_delete, True),
    "status": (cmd_status, True),
    "approvals": (cmd_approvals, True),
    "approval": (cmd_approval, True),
    "provision": (cmd_provision, True),
    "provisioning-tasks": (cmd_provisioning_tasks, True),
    "task-execute": (cmd_task_execute, True),
    "catalog-list": (cmd_catalog_list, True),
    "catalog-get": (cmd_catalog_get, True),
    "catalog-search": (cmd_catalog_search, True),
    "catalog-stats": (cmd_catalog_stats, True),
    "catalog-create": (cmd_catalog_create, True),
    "catalog-update": (cmd_catalog_update, True),
    "catalog-delete": (cmd_catalog_delete, True),
    "catalog-services": (cmd_catalog_services, True),
}


def build_parser():
    parser = argparse.ArgumentParser(prog="itsm_service_request.py", description="ITSM 服务请求+服务目录 REST API CLI（零依赖）")
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

    ls = add("list", "GET /service-requests（page/size/status）")
    ls.add_argument("--page", type=int, default=1)
    ls.add_argument("--size", type=int, default=20)
    ls.add_argument("--status", default="")
    ls.add_argument("--scope", default="", help="me（/service-requests/me 别名）")
    pd = add("pending", "GET /service-requests/approvals/pending")
    pd.add_argument("--page", type=int, default=1)
    pd.add_argument("--size", type=int, default=20)

    g = add("get", "GET /service-requests/:id")
    g.add_argument("id")
    cr = add("create", "POST /service-requests（--set title 或 catalogId；reason/formData/costCenter/dataClassification 可选）")
    cr.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    up = add("update", "PUT /service-requests/:id")
    up.add_argument("id")
    up.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    de = add("delete", "DELETE /service-requests/:id")
    de.add_argument("id")

    st = add("status", "PUT /service-requests/:id/status（运营状态机校验）")
    st.add_argument("id")
    st.add_argument("--status", required=True)
    ap = add("approvals", "GET /service-requests/:id/approvals")
    ap.add_argument("id")
    apr = add("approval", "POST /service-requests/:id/approval（approve|reject，service_request:approve）")
    apr.add_argument("id")
    apr.add_argument("--action", required=True, choices=["approve", "reject"])
    apr.add_argument("--comment", default="")

    pv = add("provision", "POST /service-requests/:id/provision（触发开通）")
    pv.add_argument("id")
    pt = add("provisioning-tasks", "GET /service-requests/:id/provisioning-tasks")
    pt.add_argument("id")
    te = add("task-execute", "POST /provisioning-tasks/:id/execute")
    te.add_argument("id")

    cl = add("catalog-list", "GET /service-catalogs")
    cl.add_argument("--page", type=int, default=1)
    cl.add_argument("--size", type=int, default=20)
    cg = add("catalog-get", "GET /service-catalogs/:id")
    cg.add_argument("id")
    csx = add("catalog-search", "GET /service-catalogs/search?keyword=")
    csx.add_argument("--keyword", required=True)
    add("catalog-stats", "GET /service-catalogs/stats")
    cc = add("catalog-create", "POST /service-catalogs（--set name/category 必填）")
    cc.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    cu = add("catalog-update", "PUT /service-catalogs/:id")
    cu.add_argument("id")
    cu.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    cd = add("catalog-delete", "DELETE /service-catalogs/:id")
    cd.add_argument("id")
    add("catalog-services", "GET /service-catalog-services")

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
