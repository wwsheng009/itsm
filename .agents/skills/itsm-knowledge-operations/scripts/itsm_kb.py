#!/usr/bin/env python3
"""ITSM 知识库 REST API CLI（零依赖，Python 3.8+，仅标准库）。

封装知识库特有细节：
- 登录换 JWT（httpOnly cookie 取 access_token）→ 统一 Bearer（免 CSRF）
- 分类白名单前置校验（10 值，服务端硬校验）
- 发布/下架/复核/版本对比/恢复（restore 后回草稿需重发）
- 搜索请求体与 snake_case 响应的直通输出
- 文章附件 multipart 上传

用法（demo 环境）：
  python scripts/itsm_kb.py login --base http://172.18.3.238:8089/api/v1 \\
      --username demo.mfg.admin --password 'Demo@2026!' --save ./itsm.token
  python scripts/itsm_kb.py --base http://172.18.3.238:8089/api/v1 --token-file ./itsm.token categories

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

# 服务端硬校验白名单（service/knowledge_service.go allowedKnowledgeCategories）
ALLOWED_CATEGORIES = [
    "accountManagement",
    "troubleshooting",
    "network",
    "processGuide",
    "systemConfig",
    "账号管理",
    "故障排除",
    "网络连接",
    "流程指南",
    "系统配置",
]


class ApiError(Exception):
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
    boundary = "----itsmkb" + uuid.uuid4().hex
    chunks = []
    for key, value in (fields or {}).items():
        if value is None:
            continue
        chunks.append(
            ('--%s\r\nContent-Disposition: form-data; name="%s"\r\n\r\n%s\r\n' % (boundary, key, value)).encode("utf-8")
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

    def request(self, method, path, body=None, raw=None, content_type="application/json"):
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
        return {"token": token, "user": (data or {}).get("user"), "tenant": (data or {}).get("tenant")}


# ------------------------- 输出 / 参数辅助 -------------------------

def emit(obj, pretty=True):
    sys.stdout.write(json.dumps(obj, ensure_ascii=False, indent=2 if pretty else None) + "\n")


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


def check_category(category):
    if category not in ALLOWED_CATEGORIES:
        fail(
            ApiError(
                "分类不在白名单: %s" % category,
                hint="允许值（10）：%s" % " / ".join(ALLOWED_CATEGORIES),
            )
        )# ------------------------- 子命令实现 -------------------------

def _query(params):
    parts = []
    for key, value in params.items():
        if value is None or value == "":
            continue
        parts.append("%s=%s" % (key, urllib.parse.quote(str(value), safe="")))
    return ("?" + "&".join(parts)) if parts else ""


def _read_content(args):
    if getattr(args, "content_file", ""):
        return Path(args.content_file).read_text(encoding="utf-8")
    if args.content is not None:
        return args.content
    fail(ApiError("需要 --content 或 --content-file"))


def cmd_login(client, args):
    if not args.username or not args.password:
        fail(ApiError("login 需要 --username/--password（或环境变量 ITSM_USERNAME/ITSM_PASSWORD）"))
    result = client.login(args.username, args.password, args.tenant_code)
    if args.save:
        Path(args.save).write_text(result["token"], encoding="utf-8")
        result["savedTo"] = args.save
    result["ok"] = True
    return result


def cmd_me(client, _args):
    return client.request("GET", "/auth/me")


def cmd_categories(client, _args):
    return client.request("GET", "/knowledge/categories")


def cmd_list(client, args):
    return client.request(
        "GET",
        "/knowledge/articles"
        + _query(
            {
                "page": args.page,
                "pageSize": args.page_size,
                "category": args.category,
                "status": args.status,
                "search": args.search,
            }
        ),
    )


def cmd_get(client, args):
    return client.request("GET", "/knowledge/articles/%s" % args.id)


def cmd_create(client, args):
    check_category(args.category)
    body = {"title": args.title, "content": _read_content(args), "category": args.category}
    if args.tags:
        body["tags"] = [t for t in args.tags.split(",") if t]
    if args.content_type:
        body["contentType"] = args.content_type
    if args.authority_level is not None:
        body["authorityLevel"] = args.authority_level
    if args.valid_until:
        body["validUntil"] = args.valid_until
    if args.review_interval_days is not None:
        body["reviewIntervalDays"] = args.review_interval_days
    return client.request("POST", "/knowledge/articles", body)


def cmd_update(client, args):
    body = {}
    if args.title:
        body["title"] = args.title
    if args.content_file:
        body["content"] = Path(args.content_file).read_text(encoding="utf-8")
    elif args.content is not None:
        body["content"] = args.content
    if args.category:
        check_category(args.category)
        body["category"] = args.category
    if args.tags is not None:
        body["tags"] = [t for t in args.tags.split(",") if t]
    if args.content_type:
        body["contentType"] = args.content_type
    if args.authority_level is not None:
        body["authorityLevel"] = args.authority_level
    if not body:
        fail(ApiError("update 至少需要一个要修改的字段"))
    return client.request("PUT", "/knowledge/articles/%s" % args.id, body)


def cmd_publish(client, args):
    body = {}
    if args.change_log:
        body["changeLog"] = args.change_log
    return client.request("POST", "/knowledge/articles/%s/publish" % args.id, body)


def cmd_unpublish(client, args):
    return client.request("POST", "/knowledge/articles/%s/unpublish" % args.id, {})


def cmd_review(client, args):
    return client.request("POST", "/knowledge/articles/%s/review" % args.id, {})


def cmd_versions(client, args):
    return client.request("GET", "/knowledge/articles/%s/versions" % args.id)


def cmd_versions_compare(client, args):
    if args.from_version == args.to_version:
        fail(ApiError("--from 与 --to 必须是不同版本"))
    return client.request(
        "GET",
        "/knowledge/articles/%s/versions/compare?from=%d&to=%d" % (args.id, args.from_version, args.to_version),
    )


def cmd_restore(client, args):
    return client.request("POST", "/knowledge/articles/%s/versions/%d/restore" % (args.id, args.version), {})


def cmd_search(client, args):
    body = {"query": args.query, "limit": args.limit}
    if args.category:
        body["category"] = args.category
    return client.request("POST", "/knowledge/search", body)


def cmd_stats(client, _args):
    return client.request("GET", "/knowledge/stats")


def cmd_recent(client, _args):
    return client.request("GET", "/knowledge/recent")


def cmd_recommendations(client, _args):
    return client.request("GET", "/knowledge/recommendations")


def cmd_restrict_list(client, _args):
    return client.request("GET", "/knowledge/categories/restricted")


def cmd_restrict_set(client, args):
    return client.request(
        "POST",
        "/knowledge/categories/restricted",
        {"category": args.category, "restricted": not args.unrestricted},
    )


def cmd_delete(client, args):
    return client.request("DELETE", "/knowledge/articles/%s" % args.id)


def cmd_attachment_list(client, args):
    return client.request("GET", "/knowledge/articles/%s/attachments" % args.id)


def cmd_attachment_upload(client, args):
    raw, ctype = build_multipart({}, args.file)
    return client.request("POST", "/knowledge/articles/%s/attachments" % args.id, raw=raw, content_type=ctype)


COMMANDS = {
    "login": (cmd_login, False),
    "me": (cmd_me, True),
    "categories": (cmd_categories, True),
    "list": (cmd_list, True),
    "get": (cmd_get, True),
    "create": (cmd_create, True),
    "update": (cmd_update, True),
    "publish": (cmd_publish, True),
    "unpublish": (cmd_unpublish, True),
    "review": (cmd_review, True),
    "versions": (cmd_versions, True),
    "versions-compare": (cmd_versions_compare, True),
    "restore": (cmd_restore, True),
    "search": (cmd_search, True),
    "stats": (cmd_stats, True),
    "recent": (cmd_recent, True),
    "recommendations": (cmd_recommendations, True),
    "restrict-list": (cmd_restrict_list, True),
    "restrict-set": (cmd_restrict_set, True),
    "delete": (cmd_delete, True),
    "attachment-list": (cmd_attachment_list, True),
    "attachment-upload": (cmd_attachment_upload, True),
}

# ------------------------- CLI -------------------------

def _add_common(parser, suppressed=False):
    """公共参数。子命令侧用 SUPPRESS：Python 3.13 argparse 的 subparser 默认值
    会覆盖主 parser 已解析的全局参数，SUPPRESS 表示"未显式给出则不动命名空间"。"""

    def default(value):
        return argparse.SUPPRESS if suppressed else value

    parser.add_argument("--base", default=default(DEFAULT_BASE), help="API base，默认 %s" % DEFAULT_BASE)
    parser.add_argument("--token", default=default(""), help="JWT 或 PAT（优先于环境变量）")
    parser.add_argument("--token-file", default=default(""), help="从文件读取 token（login --save 产物）")
    parser.add_argument("--timeout", type=float, default=default(DEFAULT_TIMEOUT), help="请求超时秒数")
    parser.add_argument("--compact", action="store_true", default=default(False), help="紧凑 JSON 输出")


def build_parser():
    parser = argparse.ArgumentParser(prog="itsm_kb.py", description="ITSM 知识库 REST API CLI（零依赖）")
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
    lg.add_argument("--save", default="", help="把 token 写入文件")

    add("me", "GET /auth/me")

    add("categories", "GET /knowledge/categories（租户已用分类）")

    ls = add("list", "GET /knowledge/articles")
    ls.add_argument("--page", type=int, default=1)
    ls.add_argument("--page-size", type=int, default=10)
    ls.add_argument("--category", default="")
    ls.add_argument("--status", default="", choices=["", "published", "draft"])
    ls.add_argument("--search", default="")

    gt = add("get", "GET /knowledge/articles/:id")
    gt.add_argument("id")

    cr = add("create", "POST /knowledge/articles（分类必须在 10 值白名单内）")
    cr.add_argument("--title", required=True)
    cr.add_argument("--content", default=None)
    cr.add_argument("--content-file", default="", help="从文件读取正文（与 --content 二选一）")
    cr.add_argument("--category", required=True)
    cr.add_argument("--tags", default="", help="逗号分隔")
    cr.add_argument("--content-type", default="", choices=["", "text", "markdown", "html", "rich_text"])
    cr.add_argument("--authority-level", type=int, default=None, help="0/10/20/30")
    cr.add_argument("--valid-until", default="", help="RFC3339 时效截止")
    cr.add_argument("--review-interval-days", type=int, default=None)

    up = add("update", "PUT /knowledge/articles/:id（部分更新；status 被忽略）")
    up.add_argument("id")
    up.add_argument("--title", default="")
    up.add_argument("--content", default=None)
    up.add_argument("--content-file", default="")
    up.add_argument("--category", default="")
    up.add_argument("--tags", default=None, help="逗号分隔（传空串=清空）")
    up.add_argument("--content-type", default="", choices=["", "text", "markdown", "html", "rich_text"])
    up.add_argument("--authority-level", type=int, default=None)

    pb = add("publish", "POST /knowledge/articles/:id/publish（发布=版本唯一入口）")
    pb.add_argument("id")
    pb.add_argument("--change-log", default="")

    ub = add("unpublish", "POST /knowledge/articles/:id/unpublish（幂等，无版本）")
    ub.add_argument("id")

    rv = add("review", "POST /knowledge/articles/:id/review（刷新复核时间）")
    rv.add_argument("id")

    vs = add("versions", "GET /knowledge/articles/:id/versions")
    vs.add_argument("id")

    vc = add("versions-compare", "GET /knowledge/articles/:id/versions/compare?from=&to=")
    vc.add_argument("id")
    vc.add_argument("--from", dest="from_version", type=int, required=True)
    vc.add_argument("--to", dest="to_version", type=int, required=True)

    rs = add("restore", "POST /knowledge/articles/:id/versions/:version/restore（恢复后回草稿）")
    rs.add_argument("id")
    rs.add_argument("--version", type=int, required=True)

    se = add("search", "POST /knowledge/search（只返回已发布+时效合格）")
    se.add_argument("--query", required=True)
    se.add_argument("--category", default="")
    se.add_argument("--limit", type=int, default=20, help="默认20，上限50")

    add("stats", "GET /knowledge/stats")
    add("recent", "GET /knowledge/recent")
    add("recommendations", "GET /knowledge/recommendations")

    add("restrict-list", "GET /knowledge/categories/restricted")
    rt = add("restrict-set", "POST /knowledge/categories/restricted")
    rt.add_argument("--category", required=True)
    rt.add_argument("--unrestricted", action="store_true", help="解除纳管（缺省为纳管）")

    dl = add("delete", "DELETE /knowledge/articles/:id（软删）")
    dl.add_argument("id")

    al = add("attachment-list", "GET /knowledge/articles/:id/attachments")
    al.add_argument("id")

    au = add("attachment-upload", "POST /knowledge/articles/:id/attachments（multipart file）")
    au.add_argument("id")
    au.add_argument("--file", required=True)

    return parser


def main(argv=None):
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")
    args = build_parser().parse_args(argv)
    pretty = not args.compact
    handler, needs_auth = COMMANDS[args.command]
    try:
        client = Client(args.base, "", args.timeout) if args.command == "login" else (authed(args) if needs_auth else Client(args.base, "", args.timeout))
        result = handler(client, args)
        emit(result if result is not None else {"ok": True}, pretty)
        return 0
    except ApiError as err:
        fail(err, pretty)
    except KeyboardInterrupt:
        return 130


if __name__ == "__main__":
    sys.exit(main())