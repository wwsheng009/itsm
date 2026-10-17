"""ITSM skills 共享底座（零依赖，Python 3.8+，仅标准库）。

被各业务域 CLI（itsm_ticket.py / itsm_kb.py / itsm_cmdb.py …）引用，收敛以下公共层，
禁止在各脚本内复制漂移（治理见 .agents/skills/README.md §4.3）：
- ApiError：HTTP/业务错误（信封 {code,message,data}，含 http/code/reasonCode/hint）
- Client：Bearer + httpOnly cookie 登录提取 access_token（免 CSRF）
- multipart 构造（附件上传）
- 输出/失败：emit / fail（错误 JSON 化，退出码 1）
- 令牌：resolve_token（--token > --token-file > ITSM_TOKEN/ITSM_PAT）、
  save_login_token（默认 ~/.itsm/itsm.token，自动建父目录、POSIX chmod 600、stdout 掩码回显）
- 公共 argparse：add_common_args（--base/--token/--token-file/--timeout/--compact，
  子命令侧 SUPPRESS 语义：Python 3.13 下 subparser 默认值不覆盖主 parser）

用法（各域脚本内）：
    import sys
    from pathlib import Path
    sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "_lib"))
    from itsm_common import (...)
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
DEFAULT_TOKEN_PATH = Path.home() / ".itsm" / "itsm.token"


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
    boundary = "----itsmcli" + uuid.uuid4().hex
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
    token = getattr(args, "token", "") or os.environ.get("ITSM_TOKEN") or os.environ.get("ITSM_PAT")
    if not token and getattr(args, "token_file", None):
        token = Path(args.token_file).read_text(encoding="utf-8").strip()
    return token or ""


def authed(args):
    token = resolve_token(args)
    if not token:
        fail(ApiError("缺少令牌：先 login 获取 JWT，或用 --token/--token-file/ITSM_PAT 提供"))
    return Client(args.base, token, args.timeout)


def save_login_token(result, save_path, tool_name="itsm_cli"):
    """登录结果 token 落盘（含安全卫生），并就地掩码 result["token"]。

    - 路径 expanduser，默认 ~/.itsm/itsm.token；自动创建父目录
    - POSIX 下 chmod 600；stderr 提醒勿提交版本库
    - stdout 仅回显掩码（result["maskedToken"]）
    """
    path = Path(save_path).expanduser()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(result["token"], encoding="utf-8")
    if os.name != "nt":
        try:
            path.chmod(0o600)
        except OSError:
            pass
    print(
        "[%s] token 已写入 %s（含明文凭据，勿提交版本库；后续用 --token-file 引用）" % (tool_name, path),
        file=sys.stderr,
    )
    masked = result["token"][:12] + "****"
    result["savedTo"] = str(path)
    result["maskedToken"] = masked
    result["token"] = masked
    return result


def add_common_args(parser, suppressed=False):
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


def parse_kv(pairs, flag="--filter"):
    """key=value 列表转 dict；缺 '=' 直接报参数错误（退出码 2 语义由调用方处理）。"""
    out = {}
    for item in pairs or []:
        if "=" not in item:
            raise ApiError("%s 需要 key=value 形式: %s" % (flag, item))
        key, value = item.split("=", 1)
        out[key.strip()] = value
    return out


def add_login_parser(sub, add_parser, tool_name="itsm_cli"):
    """统一 login 子命令：--save 省略路径默认 ~/.itsm/itsm.token。"""
    lg = add_parser("login", "登录换 JWT（从 Set-Cookie 提取 access_token）")
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
    return lg
