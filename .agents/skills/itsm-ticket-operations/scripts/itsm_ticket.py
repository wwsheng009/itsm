#!/usr/bin/env python3
"""ITSM 工单 REST API CLI（零依赖，Python 3.8+，仅标准库）。

封装 references/ticket-api.md 中所有“每次都要重写”的调用细节：
- 登录换 JWT：从 httpOnly Set-Cookie 提取 access_token，后续统一走 Bearer（免 CSRF）
- 乐观锁：update 默认读-改-写，409 自动重取版本重试一次
- 附件：multipart 上传（工单域 / 通用面 usage=comment_attachment）
- 自定义类型：create 失败时提示必填 formFields key

用法（demo 环境；token 建议存用户主目录，勿放版本库；bash 与 pwsh 等价）：
  python scripts/itsm_ticket.py login --base http://172.18.3.238:8089/api/v1 \\
      --username demo.mfg.admin --password 'Demo@2026!' --save "$HOME/.itsm/itsm.token"
  python scripts/itsm_ticket.py --base http://172.18.3.238:8089/api/v1 --token-file "$HOME/.itsm/itsm.token" me

login --save：省略路径默认 ~/.itsm/itsm.token；自动创建父目录（POSIX chmod 600），stdout 仅回显掩码。
Token 来源优先级：--token > --token-file > 环境变量 ITSM_TOKEN / ITSM_PAT。
输出：JSON 到 stdout；API 错误 JSON 到 stderr，退出码 1（参数错误 2）。
"""
import argparse
import os
import sys
import urllib.parse
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "_lib"))
from itsm_common import (  # noqa: E402
    add_common_args,
    DEFAULT_BASE,
    DEFAULT_TIMEOUT,
    DEFAULT_TOKEN_PATH,
    ApiError,
    authed,
    build_multipart,
    Client,
    emit,
    fail,
    parse_kv,
    save_login_token,
)




# ------------------------- 子命令实现 -------------------------

def upload_attachment(client, ticket_id, file_path, usage=None):
    fields = {}
    if usage:
        fields["usage"] = usage
    raw, ctype = build_multipart(fields, file_path)
    return client.request("POST", "/tickets/%s/attachments" % ticket_id, raw=raw, content_type=ctype)


def upload_comment_attachment(client, ticket_id, file_path):
    raw, ctype = build_multipart(
        {"bizType": "ticket", "bizId": str(ticket_id), "usage": "comment_attachment"},
        file_path,
    )
    return client.request("POST", "/attachments", raw=raw, content_type=ctype)


def cmd_login(client, args):
    result = client.login(args.username, args.password, args.tenant_code)
    if args.save:
        save_login_token(result, args.save, tool_name="itsm_ticket")
    result["ok"] = True
    return result


def cmd_me(client, _args):
    return client.request("GET", "/auth/me")


def cmd_types(client, args):
    data = client.request(
        "GET", "/ticket-types?page=%d&pageSize=%d" % (args.page, args.page_size)
    )
    if isinstance(data, dict):
        items = data.get("items") or data.get("types") or []
        if args.show_fields:
            for item in items:
                required = []
                for field in item.get("customFields") or []:
                    if field.get("required"):
                        required.append(
                            {
                                "key": field.get("id") or field.get("name"),
                                "label": field.get("label"),
                                "type": field.get("type"),
                            }
                        )
                item["requiredFormFields"] = required
        if args.brief:
            keys = ["id", "code", "name", "status", "defaultPriority"]
            if args.show_fields:
                keys.append("requiredFormFields")
            items = [{key: item.get(key) for key in keys} for item in items]
        # items 与 types 为同一列表的两份别名下发：只保留一份，缩小输出
        data.pop("types", None)
        data["items"] = items
    return data


def cmd_create(client, args):
    body = {"title": args.title, "priority": args.priority}
    if args.description:
        body["description"] = args.description
    if args.type:
        body["type"] = args.type
    if args.ticket_type_id:
        body["ticketTypeId"] = args.ticket_type_id
    if args.category_id:
        body["categoryId"] = args.category_id
    elif args.category:
        body["category"] = args.category
    if args.tag:
        body["tags"] = args.tag
    if args.template_id:
        body["templateId"] = args.template_id
    if args.parent_ticket_id:
        body["parentTicketId"] = args.parent_ticket_id
    if args.assignee_id:
        body["assigneeId"] = args.assignee_id
    form = parse_kv(args.form_field, flag="--form-field")
    if form:
        body["formFields"] = form
    if args.dry_run:
        return {"dryRun": True, "method": "POST", "path": "/tickets", "body": body}
    try:
        return client.request("POST", "/tickets", body)
    except ApiError as err:
        if err.code == 1001:
            err.hint = (
                "自定义类型必填/未知字段：先执行 `types --show-fields` 查看 requiredFormFields，"
                "再用 --form-field key=value 传入（key 取 customFields[].id，如 symptom）"
            )
        raise


def _query(params):
    parts = []
    for key, value in params.items():
        if value is None or value == "":
            continue
        parts.append("%s=%s" % (key, urllib.parse.quote(str(value), safe="")))
    return ("?" + "&".join(parts)) if parts else ""


def cmd_list(client, args):
    return client.request(
        "GET",
        "/tickets"
        + _query(
            {
                "page": args.page,
                "pageSize": args.page_size,
                "status": args.status,
                "priority": args.priority,
                "assigneeId": args.assignee_id,
                "requesterId": args.requester_id,
                "keyword": args.keyword,
                "type": args.type,
                "sortBy": args.sort_by,
                "sortOrder": args.sort_order,
            }
        ),
    )


def cmd_get(client, args):
    return client.request("GET", "/tickets/%s" % args.id)


def cmd_search(client, args):
    return client.request("GET", "/tickets/search" + _query({"q": args.q}))


def cmd_stats(client, _args):
    return client.request("GET", "/tickets/stats")


def cmd_comment(client, args):
    body = {"content": args.content, "isInternal": bool(args.internal)}
    if args.attachment:
        body["attachments"] = args.attachment
    if args.mention:
        body["mentions"] = args.mention
    return client.request("POST", "/tickets/%s/comments" % args.id, body)


def cmd_status(client, args):
    return client.request("PUT", "/tickets/%s/status" % args.id, {"status": args.status})


def cmd_update(client, args):
    current = client.request("GET", "/tickets/%s" % args.id)
    explicit_version = args.version is not None
    body = {"version": args.version if explicit_version else current.get("version")}
    if args.title:
        body["title"] = args.title
    if args.description:
        body["description"] = args.description
    if args.priority:
        body["priority"] = args.priority
    try:
        return client.request("PUT", "/tickets/%s" % args.id, body)
    except ApiError as err:
        # 仅自动取版本的场景做读-改-写重试；显式 --version 时保留 409 语义交给调用方。
        if err.http == 409 and not explicit_version:
            latest = client.request("GET", "/tickets/%s" % args.id)
            body["version"] = latest.get("version")
            updated = client.request("PUT", "/tickets/%s" % args.id, body)
            if isinstance(updated, dict):
                updated["retriedAfterConflict"] = True
            return updated
        raise


def cmd_assign(client, args):
    return client.request("POST", "/tickets/%s/assign" % args.id, {"assigneeId": args.assignee_id})


def cmd_resolve(client, args):
    body = {}
    if args.resolution:
        body["resolution"] = args.resolution
    return client.request("POST", "/tickets/%s/resolve" % args.id, body)


def cmd_close(client, args):
    body = {}
    if args.reason:
        body["closeReason"] = args.reason
    return client.request("POST", "/tickets/%s/close" % args.id, body)


def cmd_escalate(client, args):
    return client.request("POST", "/tickets/%s/escalate" % args.id, {"reason": args.reason})


def cmd_history(client, args):
    return client.request("GET", "/tickets/%s/history" % args.id)


def cmd_sla(client, args):
    return client.request("GET", "/tickets/%s/sla" % args.id)


def cmd_attachment_list(client, args):
    return client.request("GET", "/tickets/%s/attachments" % args.id)


def cmd_attachment_upload(client, args):
    return upload_attachment(client, args.id, args.file, args.usage)


def cmd_comment_attachment_upload(client, args):
    return upload_comment_attachment(client, args.id, args.file)


COMMANDS = {
    "login": (cmd_login, False),
    "me": (cmd_me, True),
    "types": (cmd_types, True),
    "create": (cmd_create, True),
    "get": (cmd_get, True),
    "list": (cmd_list, True),
    "search": (cmd_search, True),
    "stats": (cmd_stats, True),
    "comment": (cmd_comment, True),
    "status": (cmd_status, True),
    "update": (cmd_update, True),
    "assign": (cmd_assign, True),
    "resolve": (cmd_resolve, True),
    "close": (cmd_close, True),
    "escalate": (cmd_escalate, True),
    "history": (cmd_history, True),
    "sla": (cmd_sla, True),
    "attachment-list": (cmd_attachment_list, True),
    "attachment-upload": (cmd_attachment_upload, True),
    "comment-attachment-upload": (cmd_comment_attachment_upload, True),
}

# ------------------------- CLI -------------------------

def _add_common(parser, suppressed=False):
    # 公共参数由共享底座提供（治理：.agents/skills/README.md §4.3）
    add_common_args(parser, suppressed=suppressed)


def build_parser():
    parser = argparse.ArgumentParser(prog="itsm_ticket.py", description="ITSM 工单 REST API CLI（零依赖）")
    _add_common(parser)
    sub = parser.add_subparsers(dest="command", required=True)
    common_sub = argparse.ArgumentParser(add_help=False)
    _add_common(common_sub, suppressed=True)

    def add(name, help_text):
        return sub.add_parser(name, help=help_text, parents=[common_sub])

    lg = add("login", "登录换 JWT（从 Set-Cookie 提取 access_token）")
    lg.add_argument("--username", default=os.environ.get("ITSM_USERNAME", ""))
    lg.add_argument("--password", default=os.environ.get("ITSM_PASSWORD", ""))
    lg.add_argument("--tenant-code", default="")
    lg.add_argument(
        "--save",
        nargs="?",
        const=str(DEFAULT_TOKEN_PATH),
        default="",
        help="token 写入文件；省略路径默认 ~/.itsm/itsm.token（供 --token-file 复用）",
    )

    add("me", "GET /auth/me（核对身份/角色/权限码）")

    tp = add("types", "GET /ticket-types（平台工单类型）")
    tp.add_argument("--page", type=int, default=1)
    tp.add_argument("--page-size", type=int, default=50)
    tp.add_argument("--show-fields", action="store_true", help="附带 requiredFormFields（必填自定义字段 key）")
    tp.add_argument("--brief", action="store_true", help="精简输出（id/code/name/status/defaultPriority，配 --show-fields 附必填字段）")

    cr = add("create", "POST /tickets（创建工单）")
    cr.add_argument("--title", required=True)
    cr.add_argument(
        "--priority", default="medium", choices=["low", "medium", "high", "critical", "urgent"]
    )
    cr.add_argument("--description", default="")
    cr.add_argument("--type", default="", help="incident/service_request/change/ticket/problem/improvement")
    cr.add_argument("--ticket-type-id", type=int, default=0)
    cr.add_argument("--assignee-id", type=int, default=0)
    cr.add_argument("--form-field", action="append", default=[], help="key=value，可重复")
    cr.add_argument("--category-id", type=int, default=0)
    cr.add_argument("--category", default="", help="分类名（categoryId 优先；未传 --category-id 时使用）")
    cr.add_argument("--tag", action="append", default=[], help="标签名，可重复")
    cr.add_argument("--template-id", type=int, default=0)
    cr.add_argument("--parent-ticket-id", type=int, default=0)
    cr.add_argument("--dry-run", action="store_true", help="只打印请求体，不发送请求")

    gt = add("get", "GET /tickets/:id（数字 ID 或工单号）")
    gt.add_argument("id")

    ls = add("list", "GET /tickets（分页/过滤）")
    ls.add_argument("--page", type=int, default=1)
    ls.add_argument("--page-size", type=int, default=20)
    ls.add_argument("--status", default="")
    ls.add_argument("--priority", default="")
    ls.add_argument("--type", default="")
    ls.add_argument("--assignee-id", type=int, default=0)
    ls.add_argument("--requester-id", type=int, default=0)
    ls.add_argument("--keyword", default="")
    ls.add_argument("--sort-by", default="", choices=["", "created_at", "updated_at", "title", "status", "priority", "ticket_number"])
    ls.add_argument("--sort-order", default="", choices=["", "asc", "desc"])

    se = add("search", "GET /tickets/search?q=")
    se.add_argument("q")

    add("stats", "GET /tickets/stats")

    cm = add("comment", "POST /tickets/:id/comments（回复/内部备注）")
    cm.add_argument("id")
    cm.add_argument("--content", required=True)
    cm.add_argument("--internal", action="store_true", help="内部备注 isInternal=true")
    cm.add_argument("--attachment", action="append", type=int, default=[], help="附件 ID（通用附件面），可重复")
    cm.add_argument("--mention", action="append", type=int, default=[], help="用户 ID，可重复")

    st = add("status", "PUT /tickets/:id/status（body 仅 status）")
    st.add_argument("id")
    st.add_argument("status")

    up = add("update", "PUT /tickets/:id（默认读-改-写；409 自动重试一次）")
    up.add_argument("id")
    up.add_argument("--title", default="")
    up.add_argument("--description", default="")
    up.add_argument("--priority", default="", choices=["", "low", "medium", "high", "critical"])
    up.add_argument("--version", type=int, default=None, help="显式版本号（传了就保留 409 语义，不自动重试）")

    asg = add("assign", "POST /tickets/:id/assign")
    asg.add_argument("id")
    asg.add_argument("--assignee-id", type=int, required=True)

    rv = add("resolve", "POST /tickets/:id/resolve")
    rv.add_argument("id")
    rv.add_argument("--resolution", default="")

    cl = add("close", "POST /tickets/:id/close（仅自 resolved）")
    cl.add_argument("id")
    cl.add_argument("--reason", default="")

    es = add("escalate", "POST /tickets/:id/escalate")
    es.add_argument("id")
    es.add_argument("--reason", required=True)

    hi = add("history", "GET /tickets/:id/history")
    hi.add_argument("id")
    sl = add("sla", "GET /tickets/:id/sla")
    sl.add_argument("id")

    al = add("attachment-list", "GET /tickets/:id/attachments")
    al.add_argument("id")

    au = add("attachment-upload", "POST /tickets/:id/attachments（multipart file）")
    au.add_argument("id")
    au.add_argument("--file", required=True)
    au.add_argument("--usage", default="")

    cu = add("comment-attachment-upload", "POST /attachments（usage=comment_attachment，先上传后绑定）")
    cu.add_argument("id")
    cu.add_argument("--file", required=True)

    return parser


def main(argv=None):
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")
    args = build_parser().parse_args(argv)
    pretty = not args.compact
    handler, needs_auth = COMMANDS[args.command]
    try:
        if args.command == "login":
            if not args.username or not args.password:
                fail(ApiError("login 需要 --username/--password（或环境变量 ITSM_USERNAME/ITSM_PASSWORD）"), pretty)
            client = Client(args.base, "", args.timeout)
        elif args.command == "create" and getattr(args, "dry_run", False):
            # dry-run 只回显请求体、不发送请求，无需令牌
            client = Client(args.base, "", args.timeout)
        else:
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