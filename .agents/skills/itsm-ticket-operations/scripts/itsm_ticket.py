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
import json
import mimetypes
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
from http.cookiejar import CookieJar
from pathlib import Path

DEFAULT_BASE = os.environ.get("ITSM_BASE_URL", "http://localhost:8090/api/v1")
DEFAULT_TIMEOUT = 30


class ApiError(Exception):
    """HTTP/业务错误：携带 http、code、message、reasonCode 与可选 data。"""

    def __init__(self, message, http=0, code=0, reason_code="", data=None, hint=""):
        super().__init__(message)
        self.http = http
        self.code = code
        self.reason_code = reason_code
        self.data = data
        self.hint = hint

    def as_dict(self):
        out = {"ok": False, "http": self.http, "code": self.code, "message": str(self)}
        if self.reason_code:
            out["reasonCode"] = self.reason_code
        if self.data is not None:
            out["data"] = self.data
        if self.hint:
            out["hint"] = self.hint
        return out


def build_multipart(fields, file_path=None, file_field="file"):
    """构建 multipart/form-data（field 值为字符串，文件单发）。"""
    boundary = "----itsm" + uuid.uuid4().hex
    chunks = []
    for key, value in (fields or {}).items():
        if value is None:
            continue
        chunks.append(
            (
                '--%s\r\nContent-Disposition: form-data; name="%s"\r\n\r\n%s\r\n'
                % (boundary, key, value)
            ).encode("utf-8")
        )
    if file_path:
        path = Path(file_path)
        mime = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
        chunks.append(
            (
                '--%s\r\nContent-Disposition: form-data; name="%s"; filename="%s"\r\n'
                "Content-Type: %s\r\n\r\n" % (boundary, file_field, path.name, mime)
            ).encode("utf-8")
            + path.read_bytes()
            + b"\r\n"
        )
    chunks.append(("--%s--\r\n" % boundary).encode("ascii"))
    return b"".join(chunks), "multipart/form-data; boundary=%s" % boundary


class Client:
    def __init__(self, base, token=None, timeout=DEFAULT_TIMEOUT):
        self.base = base.rstrip("/")
        self.token = token or ""
        self.timeout = timeout
        self.jar = CookieJar()

    def _opener(self):
        return urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))

    def request(self, method, path, body=None, content_type="application/json", raw=None):
        url = self.base + path
        data = None
        headers = {"Accept": "application/json"}
        if self.token:
            headers["Authorization"] = "Bearer %s" % self.token
        if raw is not None:
            data = raw
            headers["Content-Type"] = content_type
        elif body is not None:
            data = json.dumps(body, ensure_ascii=False).encode("utf-8")
            headers["Content-Type"] = content_type + "; charset=utf-8"
        req = urllib.request.Request(url, data=data, headers=headers, method=method.upper())
        try:
            with self._opener().open(req, timeout=self.timeout) as resp:
                payload = resp.read()
                status = resp.status
        except urllib.error.HTTPError as exc:
            payload = exc.read()
            status = exc.code
        except urllib.error.URLError as exc:
            raise ApiError("网络错误: %s" % exc.reason)
        try:
            parsed = json.loads(payload.decode("utf-8"))
        except ValueError:
            if 200 <= status < 300:
                return {"raw": payload.decode("utf-8", "replace")}
            raise ApiError("非 JSON 响应", http=status)
        if isinstance(parsed, dict) and parsed.get("code") == 0 and 200 <= status < 300:
            return parsed.get("data")
        if isinstance(parsed, dict):
            raise ApiError(
                parsed.get("message", "请求失败"),
                http=status,
                code=parsed.get("code", 0),
                reason_code=parsed.get("reasonCode", ""),
                data=parsed.get("data"),
            )
        raise ApiError("非预期响应", http=status, data=parsed)

    # ---------- 高层封装 ----------

    def login(self, username, password, tenant_code=None):
        body = {"username": username, "password": password}
        if tenant_code:
            body["tenantCode"] = tenant_code
        data = self.request("POST", "/auth/login", body)
        token = ""
        for cookie in self.jar:
            if cookie.name == "access_token":
                token = cookie.value
        if not token:
            raise ApiError("登录成功但未在 Set-Cookie 中找到 access_token")
        return {
            "token": token,
            "user": (data or {}).get("user"),
            "tenant": (data or {}).get("tenant"),
        }

    def upload_attachment(self, ticket_id, file_path, usage=None):
        fields = {}
        if usage:
            fields["usage"] = usage
        raw, ctype = build_multipart(fields, file_path)
        return self.request("POST", "/tickets/%s/attachments" % ticket_id, raw=raw, content_type=ctype)

    def upload_comment_attachment(self, ticket_id, file_path):
        raw, ctype = build_multipart(
            {"bizType": "ticket", "bizId": str(ticket_id), "usage": "comment_attachment"},
            file_path,
        )
        return self.request("POST", "/attachments", raw=raw, content_type=ctype)


# ------------------------- 输出 / 参数辅助 -------------------------

def emit(obj, pretty=True):
    text = json.dumps(obj, ensure_ascii=False, indent=2 if pretty else None, sort_keys=False)
    sys.stdout.write(text + "\n")


def fail(err, pretty=True):
    emit(err.as_dict() if isinstance(err, ApiError) else {"ok": False, "message": str(err)}, pretty)
    sys.exit(1)


def resolve_token(args):
    token = args.token or os.environ.get("ITSM_TOKEN") or os.environ.get("ITSM_PAT")
    if not token and getattr(args, "token_file", None):
        token = Path(args.token_file).read_text(encoding="utf-8").strip()
    return token or ""


def authed(args):
    token = resolve_token(args)
    if not token:
        fail(ApiError("缺少令牌：先 login 获取 JWT，或用 --token/--token-file/ITSM_PAT 提供"))
    return Client(args.base, token, args.timeout)


def parse_kv(pairs):
    out = {}
    for item in pairs or []:
        if "=" not in item:
            fail(ApiError("--form-field 需要 key=value 形式: %s" % item))
        key, value = item.split("=", 1)
        out[key.strip()] = value
    return out# ------------------------- 子命令实现 -------------------------

def cmd_login(client, args):
    result = client.login(args.username, args.password, args.tenant_code)
    if args.save:
        path = Path(args.save).expanduser()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(result["token"], encoding="utf-8")
        if os.name != "nt":
            try:
                path.chmod(0o600)
            except OSError:
                pass
        print(
            "[itsm_ticket] token 已写入 %s（含明文凭据，勿提交版本库；后续用 --token-file 引用）" % path,
            file=sys.stderr,
        )
        masked = result["token"][:12] + "****"
        result["savedTo"] = str(path)
        result["maskedToken"] = masked
        result["token"] = masked
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
    form = parse_kv(args.form_field)
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
    return client.upload_attachment(args.id, args.file, args.usage)


def cmd_comment_attachment_upload(client, args):
    return client.upload_comment_attachment(args.id, args.file)


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
    """公共参数。子命令侧用 SUPPRESS 默认值：Python 3.13 的 argparse 会让
    subparser 默认值覆盖主 parser 已解析的全局参数（`--token X me` 丢成 ''），
    SUPPRESS 表示"未显式给出则不动命名空间"，全局参数因此前后位置都生效。"""

    def default(value):
        return argparse.SUPPRESS if suppressed else value

    parser.add_argument("--base", default=default(DEFAULT_BASE), help="API base，默认 %s" % DEFAULT_BASE)
    parser.add_argument("--token", default=default(""), help="JWT 或 PAT（优先于环境变量）")
    parser.add_argument("--token-file", default=default(""), help="从文件读取 token（login --save 产物）")
    parser.add_argument("--timeout", type=float, default=default(DEFAULT_TIMEOUT), help="请求超时秒数")
    parser.add_argument("--compact", action="store_true", default=default(False), help="紧凑 JSON 输出")


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
        const=str(Path.home() / ".itsm" / "itsm.token"),
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