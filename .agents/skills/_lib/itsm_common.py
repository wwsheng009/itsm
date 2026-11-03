"""ITSM skills 共享底座（零依赖，Python 3.8+，仅标准库）。

被各业务域 CLI（itsm_ticket.py / itsm_kb.py / itsm_cmdb.py …）引用，收敛以下公共层，
禁止在各脚本内复制漂移（治理见 .agents/skills/README.md §4.3）：

- 地址解析：resolve_base —— 优先级 `--base` > 环境变量 ITSM_BASE_URL >
  `~/.itsm/config.json` 的 baseUrl > 内置 dev 兜底（http://localhost:8090/api/v1）。
  首次配置由 `setup` 持久化；`login` 成功也会回写 baseUrl。**不要假设部署地址是 localhost**。
- 令牌：resolve_token —— `--token` > ITSM_TOKEN/ITSM_PAT > `--token-file` >
  config.tokenFile > 默认 `~/.itsm/itsm.token`（存在才自动使用）；
  save_login_token —— 自动建父目录、POSIX chmod 600 / Windows 尽力 icacls 收紧、
  stdout 掩码回显、仓库目录告警。
- 安全输入：口令/PAT 走 `--password-stdin`/`--token-stdin` 管道或交互式隐藏输入（getpass），
  不鼓励把明文凭据写进 argv（会进入 shell 历史/进程列表/agent 记录）。
- 首次配置与自检：setup（地址 + 凭据 → 验证 → 落盘）/ config（来源 + 连通性 + 身份，不回显明文）。
- ApiError：HTTP/业务错误（信封 {code,message,data}，含 http/code/reasonCode/hint）
- Client：Bearer + httpOnly cookie 登录提取 access_token（免 CSRF）；401/网络错误附修复提示
- multipart 构造（附件上传）
- 输出/失败：emit / fail（错误 JSON 化，退出码 1）
- 公共 argparse：add_common_args / add_login_parser / add_setup_parser / add_config_parser
  （子命令侧 SUPPRESS 语义：Python 3.13 下 subparser 默认值不覆盖主 parser）

用法（各域脚本内，兼容两种布局自动定位，勿手写 parents[N]）：
    import sys
    from pathlib import Path
    sys.path.insert(0, _locate_lib())
    from itsm_common import (...)

其中 _locate_lib 逻辑：脚本位于 <root>/<skill>/scripts/x.py——
  开发态（仓库）：<root>/_lib           （parents[2]/_lib）
  分发态（单技能 zip）：<root>/<skill>/_lib（parents[1]/_lib）
取第一个存在 itsm_common.py 的路径，找不到抛出带布局提示的 RuntimeError。
"""
import argparse
import getpass
import json
import mimetypes
import os
import subprocess
import sys
import threading
import urllib.error
import urllib.parse
import urllib.request
import uuid
from datetime import datetime, timezone
from http.cookiejar import CookieJar
from pathlib import Path

DEFAULT_BASE = "http://localhost:8090/api/v1"   # 内置 dev 兜底，仅当 --base/env/config 都缺失时使用
DEFAULT_TIMEOUT = 30
DEFAULT_TOKEN_PATH = Path.home() / ".itsm" / "itsm.token"
CONFIG_PATH = Path(
    os.environ.get("ITSM_CONFIG_FILE") or (Path.home() / ".itsm" / "config.json")
)
ENV_BASE = "ITSM_BASE_URL"
SOURCE_DEFAULT = "default"


# ------------------------- 用户配置（~/.itsm/config.json） -------------------------

def load_config():
    """读取 ~/.itsm/config.json（容忍缺失/损坏，返回 dict；ITSM_CONFIG_FILE 可覆盖路径）。"""
    try:
        raw = CONFIG_PATH.read_text(encoding="utf-8")
    except OSError:
        return {}
    try:
        cfg = json.loads(raw)
    except ValueError:
        return {}
    return cfg if isinstance(cfg, dict) else {}


def save_config(**updates):
    """原子合并写入 ~/.itsm/config.json（键级合并，保留未知键）；失败仅告警不阻断。"""
    cfg = load_config()
    for key, value in updates.items():
        if value is not None:
            cfg[key] = value
    cfg["updatedAt"] = datetime.now(timezone.utc).replace(microsecond=0).isoformat()
    try:
        CONFIG_PATH.parent.mkdir(parents=True, exist_ok=True)
        tmp = CONFIG_PATH.with_name(CONFIG_PATH.name + ".tmp")
        tmp.write_text(json.dumps(cfg, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        os.replace(str(tmp), str(CONFIG_PATH))
        if os.name != "nt":
            try:
                CONFIG_PATH.chmod(0o600)
            except OSError:
                pass
    except OSError as exc:
        print("[itsm] 警告：配置文件写入失败 %s（%s）" % (CONFIG_PATH, exc), file=sys.stderr)
    return cfg


# ------------------------- 地址 / 令牌解析 -------------------------

def resolve_base(args=None):
    """解析 API base 与来源，返回 (base, source)。

    优先级：--base > ITSM_BASE_URL > ~/.itsm/config.json 的 baseUrl > 内置兜底。
    source ∈ flag|env|config|default；base 去掉尾部 '/'。
    """
    explicit = ""
    if args is not None:
        explicit = (getattr(args, "base", None) or "").strip()
    if explicit:
        return explicit.rstrip("/"), "flag"
    env = (os.environ.get(ENV_BASE) or "").strip()
    if env:
        return env.rstrip("/"), "env"
    cfg = load_config().get("baseUrl")
    if isinstance(cfg, str) and cfg.strip():
        return cfg.strip().rstrip("/"), "config"
    return DEFAULT_BASE, SOURCE_DEFAULT


def finalize_args(args):
    """回填解析后的地址（各脚本 main 在 parse_args 后调用一次）：args.base / args.base_source。"""
    if getattr(args, "_base_finalized", False):
        return args
    base, source = resolve_base(args)
    args.base = base
    args.base_source = source
    args._base_finalized = True
    return args


def _effective_base(args):
    """取 (base, source)：已 finalize 用回填值，否则现算（供 setup/config 直接调用时的兜底）。"""
    if getattr(args, "_base_finalized", False):
        return args.base, getattr(args, "base_source", "flag")
    return resolve_base(args)


def resolve_token_with_source(args):
    """令牌解析，返回 (token, source)。

    优先级：--token > ITSM_TOKEN/ITSM_PAT > --token-file > config.tokenFile >
    默认 ~/.itsm/itsm.token（文件存在才自动使用）。
    source ∈ flag|env|flag-file|config-file|default-file|none。
    """
    token = (getattr(args, "token", "") or "").strip()
    if token:
        return token, "flag"
    env = (os.environ.get("ITSM_TOKEN") or os.environ.get("ITSM_PAT") or "").strip()
    if env:
        return env, "env"
    path = (getattr(args, "token_file", None) or "").strip()
    source = "flag-file"
    if not path:
        cfg = load_config().get("tokenFile")
        if isinstance(cfg, str) and cfg.strip():
            path, source = cfg.strip(), "config-file"
    if not path and DEFAULT_TOKEN_PATH.exists():
        path, source = str(DEFAULT_TOKEN_PATH), "default-file"
    if not path:
        return "", "none"
    try:
        token = Path(path).expanduser().read_text(encoding="utf-8").strip()
    except OSError as exc:
        raise ApiError("无法读取 token 文件 %s：%s" % (path, exc))
    return (token, source) if token else ("", "none")


def resolve_token(args):
    """兼容旧签名：仅返回 token（来源见 resolve_token_with_source）。"""
    return resolve_token_with_source(args)[0]


# ------------------------- 布局定位 / 错误 / HTTP -------------------------

def locate_lib():
    """定位 _lib 目录（供各域脚本 sys.path 注入；兼容开发态/分发态两种布局）。"""
    here = Path(__file__).resolve().parent          # <root>/_lib（本文件所在处）
    script_dir = here.parent.parent                  # <skill>/scripts
    candidates = [
        script_dir.parent / "_lib",   # 分发态：<root>/<skill>/_lib
        script_dir.parent.parent / "_lib",  # 开发态：<root>/_lib
    ]
    for cand in candidates:
        if (cand / "itsm_common.py").exists():
            return str(cand)
    raise RuntimeError(
        "未找到 _lib/itsm_common.py；期望布局：<root>/<skill>/scripts/x.py 且 "
        "_lib 位于 <root>/<skill>/_lib（单技能包）或 <root>/_lib（仓库）"
    )


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

    def _connect_hint(self):
        """本地兜底地址的连接失败提示（引导一次性配置部署地址）。"""
        host = self.base.split("//", 1)[-1].split("/", 1)[0]
        if host.startswith("localhost") or host.startswith("127.0.0.1"):
            return (
                "当前地址指向本机（%s）。若 ITSM 实际部署在其它地址：运行 `setup` 一次性配置，"
                "或加 --base / 设置环境变量 ITSM_BASE_URL（解析优先级见 `config` 子命令）" % self.base
            )
        return ""

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
            raise ApiError("网络错误: %s" % exc.reason, hint=self._connect_hint())
        try:
            parsed = json.loads(payload.decode("utf-8"))
        except ValueError:
            if 200 <= status < 300:
                return {"raw": payload.decode("utf-8", "replace")}
            raise ApiError("非 JSON 响应", http=status)
        if isinstance(parsed, dict) and parsed.get("code") == 0 and 200 <= status < 300:
            return parsed.get("data")
        if isinstance(parsed, dict):
            hint = ""
            if status == 401:
                hint = (
                    "令牌无效或已过期（JWT 默认 15 分钟有效）：建议改用 PAT（个人中心→安全设置创建）"
                    "并运行 `setup` 落盘，或重新 login 更新 token"
                )
            raise ApiError(
                parsed.get("message", "请求失败"),
                http=status,
                code=parsed.get("code", 0),
                reason_code=parsed.get("reasonCode", ""),
                data=parsed.get("data"),
                hint=hint,
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


def authed(args):
    token = resolve_token(args)
    if not token:
        fail(ApiError(
            "缺少令牌：运行 `setup` 一次性配置部署地址与凭据（或 login 后 --save），"
            "也可用 --token/--token-file/ITSM_TOKEN/ITSM_PAT 提供",
            hint="安全口径：优先 PAT（个人中心→安全设置创建，有效期长）；勿把明文 token 写进命令行参数、脚本或仓库。",
        ))
    return Client(args.base, token, args.timeout)


def _harden_token_file(path):
    """凭据文件权限收紧：POSIX chmod 600；Windows 尽力 icacls（失败不阻断，ITSM_SKIP_TOKEN_ACL=1 跳过）。"""
    if os.name != "nt":
        try:
            path.chmod(0o600)
        except OSError:
            pass
        return
    if os.environ.get("ITSM_SKIP_TOKEN_ACL"):
        return
    user = os.environ.get("USERNAME") or ""
    if not user:
        return
    try:
        grant = subprocess.run(
            ["icacls", str(path), "/grant:r", "%s:F" % user],
            capture_output=True, timeout=10, check=False,
        )
        if grant.returncode == 0:
            subprocess.run(
                ["icacls", str(path), "/inheritance:r"],
                capture_output=True, timeout=10, check=False,
            )
    except (OSError, subprocess.SubprocessError):
        pass


def _warn_if_in_repo(path, tool_name):
    """明文凭据落盘位置告警：位于当前目录或 Git 工作树内时提醒勿提交版本库。"""
    probe = path.resolve()
    in_tree = False
    try:
        probe.relative_to(Path.cwd().resolve())
        in_tree = True
    except ValueError:
        pass
    if not in_tree:
        parent = probe.parent
        while True:
            if (parent / ".git").exists():
                in_tree = True
                break
            if parent.parent == parent:
                break
            parent = parent.parent
    if in_tree:
        print(
            "[%s] 警告：token 目标位于当前目录/Git 工作树内（%s）——明文凭据勿提交版本库，"
            "建议改存用户主目录（默认 ~/.itsm/itsm.token）" % (tool_name, path),
            file=sys.stderr,
        )


def save_login_token(result, save_path, tool_name="itsm_cli"):
    """登录/导入 token 落盘（含安全卫生），并就地掩码 result["token"]。

    - 路径 expanduser，默认调用方给 ~/.itsm/itsm.token；自动创建父目录
    - POSIX chmod 600；Windows 尽力 icacls 收紧；仓库目录告警
    - stdout 仅回显掩码（result["maskedToken"]）
    """
    path = Path(save_path).expanduser()
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(result["token"], encoding="utf-8")
    except OSError as exc:
        raise ApiError("token 写入失败 %s：%s" % (path, exc))
    _harden_token_file(path)
    _warn_if_in_repo(path, tool_name)
    print(
        "[%s] token 已写入 %s（含明文凭据，勿提交版本库；后续用 --token-file 引用或直接使用，"
        "默认路径会被自动读取）" % (tool_name, path),
        file=sys.stderr,
    )
    masked = result["token"][:12] + "****"
    result["savedTo"] = str(path)
    result["maskedToken"] = masked
    result["token"] = masked
    return result


# ------------------------- 凭据安全输入 / 登录 -------------------------

PROMPT_TIMEOUT = float(os.environ.get("ITSM_PROMPT_TIMEOUT") or 60)


def _read_with_timeout(reader, timeout):
    """线程执行 reader()；EOF 或超时返回 None（避免 agent/CI 无输入源时永久阻塞）。

    超时后残留的读取线程为 daemon：调用方随即给出非交互引导并退出进程。
    ITSM_PROMPT_TIMEOUT 可调（秒；0/空 表示不限制，退回纯阻塞行为）。
    """
    holder = {}

    def run():
        try:
            holder["value"] = reader()
        except EOFError:
            holder["value"] = None

    worker = threading.Thread(target=run, daemon=True)
    worker.start()
    worker.join(timeout if timeout else None)
    if worker.is_alive():
        print(
            "[itsm] 提示：交互输入超时（%ss 无输入）——当前环境可能不支持交互；"
            "请改用非交互参数（--token-stdin/--from-env 等），或设置 ITSM_PROMPT_TIMEOUT=0 关闭限时"
            % (timeout,),
            file=sys.stderr,
        )
        return None
    return holder.get("value")


def prompt_line(text):
    """交互式提问；EOF/超时（无输入源）返回 None，由调用方转为非交互引导。"""
    return _read_with_timeout(lambda: input(text), PROMPT_TIMEOUT)


def prompt_password(text):
    """隐藏输入（getpass）；EOF/超时返回 None。"""
    return _read_with_timeout(lambda: getpass.getpass(text), PROMPT_TIMEOUT)


def resolve_username(args):
    """用户名：--username > 解析器默认（ITSM_USERNAME）> 交互式输入。"""
    username = (getattr(args, "username", "") or "").strip()
    if username:
        return username
    if sys.stdin.isatty():
        shown = prompt_line("用户名: ")
        if shown and shown.strip():
            return shown.strip()
    raise ApiError("缺少用户名：用 --username 或环境变量 ITSM_USERNAME 提供")


def resolve_password(args):
    """口令解析（安全优先）：--password > --password-stdin > ITSM_PASSWORD > 交互式隐藏输入。"""
    password = getattr(args, "password", None) or ""
    if password:
        return password
    if getattr(args, "password_stdin", False):
        line = sys.stdin.readline().rstrip("\r\n")
        if not line:
            raise ApiError("--password-stdin 未从 stdin 读取到内容")
        return line
    env = os.environ.get("ITSM_PASSWORD") or ""
    if env:
        return env
    if sys.stdin.isatty():
        password = prompt_password("密码（不回显）: ")
        if password:
            return password
    raise ApiError(
        "缺少密码：交互式终端直接运行（隐藏输入），或 --password-stdin / ITSM_PASSWORD 提供",
        hint="安全口径：勿把明文口令写进命令行参数（会进入 shell 历史与进程列表）。",
    )


def do_login(client, args, tool_name="itsm_cli"):
    """共享 login 实现：安全取口令 → 换 JWT →（可选）落盘 + 持久化地址配置。

    - `--save`（省略路径默认 ~/.itsm/itsm.token）时写 token 文件并登记到 config.tokenFile
    - 成功即回写 config.baseUrl（后续命令无需再带 --base）
    - JWT 默认 15 分钟过期；自动化场景建议 `setup` 导入 PAT
    """
    username = resolve_username(args)
    password = resolve_password(args)
    result = client.login(username, password, getattr(args, "tenant_code", "") or "")
    saved_to = ""
    if getattr(args, "save", ""):
        save_login_token(result, args.save, tool_name=tool_name)
        saved_to = result.get("savedTo", "")
    updates = {"baseUrl": client.base}
    if saved_to:
        updates["tokenFile"] = saved_to
    save_config(**updates)
    result["ok"] = True
    if not saved_to:
        result["hint"] = (
            "凭据未落盘：建议运行 `setup` 或加 --save；JWT 默认 15 分钟过期，"
            "自动化/长任务请改用 PAT（有效期最长 365 天）"
        )
    return result


# ------------------------- 连通性 / 配置自检 / 首次配置 -------------------------

def probe_health(base, timeout=5):
    """探测 <base>/health（无需认证），返回 (ok, detail)；用于 setup/config 自检。"""
    url = base.rstrip("/") + "/health"
    req = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return True, {"url": url, "http": getattr(resp, "status", 200)}
    except urllib.error.HTTPError as exc:
        return False, {"url": url, "http": exc.code, "error": "HTTP %s" % exc.code}
    except urllib.error.URLError as exc:
        return False, {"url": url, "error": str(exc.reason)}
    except OSError as exc:
        return False, {"url": url, "error": str(exc)}


def cmd_config(client, args):
    """共享 config 子命令：打印生效地址/凭据来源（不回显明文）并验证 health + /auth/me。

    --offline 只输出本地解析结果。检查未通过时以错误 JSON（data 含明细）退出 1。
    """
    base, base_source = _effective_base(args)
    out = {
        "ok": True,
        "configPath": str(CONFIG_PATH),
        "baseUrl": base,
        "baseSource": base_source,
        "tokenPresent": False,
        "tokenSource": "none",
    }
    try:
        token, token_source = resolve_token_with_source(args)
        out["tokenPresent"] = bool(token)
        out["tokenSource"] = token_source
    except ApiError as err:
        out["tokenError"] = str(err)
        token = ""
    cfg = load_config()
    if cfg.get("tokenFile"):
        out["tokenFile"] = cfg.get("tokenFile")
    if getattr(args, "offline", False):
        out["offline"] = True
        return out

    health_ok, health_detail = probe_health(base, min(args.timeout, 5))
    out["health"] = dict({"ok": health_ok}, **health_detail)
    if token:
        try:
            me = Client(base, token, args.timeout).request("GET", "/auth/me")
            out["auth"] = {"ok": True, "user": (me or {}).get("user") if isinstance(me, dict) else me}
        except ApiError as err:
            out["auth"] = {"ok": False, "http": err.http, "message": str(err)}
    else:
        out["auth"] = {"ok": False, "message": "未发现令牌（运行 setup 配置）"}
    if not health_ok or not out["auth"].get("ok"):
        hints = []
        if not health_ok:
            hints.append("地址不可达：核对 --base / ITSM_BASE_URL / config.json（须含 /api/v1 前缀）")
        if not out["auth"].get("ok"):
            hints.append("凭据缺失或过期：运行 `setup` 导入 PAT（推荐）或重新 login")
        raise ApiError("配置检查未通过", data=out, hint="；".join(hints))
    return out


def cmd_setup(client, args):
    """共享 setup 子命令：首次配置 API 地址 + 凭据（验证后落盘并持久化配置）。

    - 交互式：提示地址（未配置时）与凭据（PAT 或账号；输入均不回显）
    - 非交互：--base + （--token-stdin | --token-file | --from-env | --username + --password-stdin）
    - 落盘：token → ~/.itsm/itsm.token（或 --save 指定）；地址与路径 → ~/.itsm/config.json
    - 验证：/health 连通性 + /auth/me 身份（PAT 走 Bearer；账号走 login 换 JWT）
    """
    tool = getattr(args, "_tool", "itsm_cli")
    interactive = sys.stdin.isatty()
    if getattr(args, "token_stdin", False) and getattr(args, "password_stdin", False):
        raise ApiError("--token-stdin 与 --password-stdin 不能同时使用（二者都读 stdin）")
    base, base_source = _effective_base(args)

    if base_source == SOURCE_DEFAULT:
        if interactive:
            shown = prompt_line("ITSM API 地址（含 /api/v1）[%s]: " % base)
            if shown and shown.strip():
                base, base_source = shown.strip().rstrip("/"), "prompt"
        if base_source == SOURCE_DEFAULT:
            raise ApiError(
                "未配置 API 地址（当前为内置兜底 %s）" % base,
                hint="非交互模式请显式传入：python %s setup --base https://itsm.example.com/api/v1 --token-stdin" % tool,
            )

    health_ok, health_detail = probe_health(base, min(args.timeout, 10))
    if not health_ok:
        raise ApiError(
            "API 地址不可达：%s" % health_detail.get("error", health_detail.get("http")),
            data={"baseUrl": base, "health": health_detail},
            hint="核对地址（含 /api/v1 前缀）、网络与反向代理；部署端口参照 .dev/deploy/README.md",
        )

    pat = (getattr(args, "token", "") or "").strip()
    pat_source = "--token（argv 明文可见，建议 --token-stdin/--from-env）" if pat else ""
    if not pat and (getattr(args, "token_file", "") or "").strip():
        src = args.token_file
        try:
            pat = Path(src).expanduser().read_text(encoding="utf-8").strip()
        except OSError as exc:
            raise ApiError("无法读取 token 文件 %s：%s" % (src, exc))
        pat_source = "--token-file(%s)" % src
    if not pat and getattr(args, "token_stdin", False):
        pat = sys.stdin.readline().strip()
        if not pat:
            raise ApiError("--token-stdin 未从 stdin 读取到内容")
        pat_source = "--token-stdin"
    if not pat and getattr(args, "from_env", False):
        pat = (os.environ.get("ITSM_TOKEN") or os.environ.get("ITSM_PAT") or "").strip()
        if pat:
            pat_source = "--from-env(ITSM_TOKEN/ITSM_PAT)"

    account = None
    if not pat:
        if getattr(args, "from_env", False) and os.environ.get("ITSM_USERNAME") and os.environ.get("ITSM_PASSWORD"):
            account = (os.environ["ITSM_USERNAME"], os.environ["ITSM_PASSWORD"])
            pat_source = "--from-env(ITSM_USERNAME/ITSM_PASSWORD)"
        elif (getattr(args, "username", "") or "").strip():
            account = (resolve_username(args), resolve_password(args))
            pat_source = pat_source or "--username/--password-stdin"
        elif interactive:
            choice = prompt_line("凭据类型：1) PAT（推荐，有效期长） 2) 账号密码 [1]: ")
            if choice is None:
                raise ApiError(
                    "交互输入不可用（stdin 已关闭）：请改用 --token-stdin / --token-file / --from-env，"
                    "账号方式用 --username + --password-stdin",
                    hint="示例：Get-Content pat.txt | python %s setup --base <URL> --token-stdin" % tool,
                )
            choice = choice.strip() or "1"
            if choice.startswith("1"):
                pat = (prompt_password("粘贴 PAT（输入不回显）: ") or "").strip()
                if not pat:
                    raise ApiError("未输入 PAT")
                pat_source = "交互式输入（PAT）"
            else:
                account = (resolve_username(args), resolve_password(args))
                pat_source = "交互式输入（账号）"
        else:
            raise ApiError(
                "缺少凭据：非交互模式请用 --token-stdin / --token-file / --from-env（或 --username + --password-stdin）",
                hint="PAT 在 ITSM 个人中心→安全设置创建；示例：Get-Content pat.txt | python %s setup --base <URL> --token-stdin" % tool,
            )

    if pat:
        me = Client(base, pat, args.timeout).request("GET", "/auth/me")
        result = {"token": pat, "user": (me or {}).get("user") if isinstance(me, dict) else me}
    else:
        username, password = account
        result = Client(base, "", args.timeout).login(username, password, getattr(args, "tenant_code", "") or "")

    target = (getattr(args, "save", "") or "").strip() or load_config().get("tokenFile") or str(DEFAULT_TOKEN_PATH)
    save_login_token(result, target, tool_name=tool)
    saved = result.get("savedTo", str(Path(target).expanduser()))
    save_config(baseUrl=base, tokenFile=saved)
    result["ok"] = True
    result["baseUrl"] = base
    result["baseSource"] = base_source
    result["credentialSource"] = pat_source
    result["configPath"] = str(CONFIG_PATH)
    result["nextSteps"] = [
        "后续命令无需再带 --base/--token-file（自动读取 %s）" % CONFIG_PATH,
        "自检：python %s config" % tool,
        "更新/轮换凭据：重新运行 setup（或 login --save）",
    ]
    print("[%s] 配置完成：%s → %s（配置文件 %s）" % (tool, base, saved, CONFIG_PATH), file=sys.stderr)
    for step in result["nextSteps"]:
        print("[%s] 下一步：%s" % (tool, step), file=sys.stderr)
    return result


# ------------------------- 公共 argparse -------------------------

def add_common_args(parser, suppressed=False):
    """公共参数。子命令侧用 SUPPRESS 默认值：Python 3.13 的 argparse 会让
    subparser 默认值覆盖主 parser 已解析的全局参数（`--token X me` 丢成 ''），
    SUPPRESS 表示"未显式给出则不动命名空间"，全局参数因此前后位置都生效。"""

    def default(value):
        return argparse.SUPPRESS if suppressed else value

    parser.add_argument(
        "--base",
        default=default(None),
        help="API base；解析优先级 --base > 环境变量 ITSM_BASE_URL > ~/.itsm/config.json > 内置兜底 %s" % DEFAULT_BASE,
    )
    parser.add_argument("--token", default=default(""), help="JWT 或 PAT（优先于环境变量；argv 明文可见，优先用 token 文件）")
    parser.add_argument(
        "--token-file",
        default=default(""),
        help="从文件读取 token（login/setup --save 产物；未提供时自动尝试 config.tokenFile 与默认路径）",
    )
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
    """统一 login 子命令：--save 省略路径默认 ~/.itsm/itsm.token。

    口令安全：--password 会进入 argv（shell 历史/进程列表），推荐
    --password-stdin（管道）或交互式隐藏输入。
    """
    lg = add_parser("login", "登录换 JWT（从 Set-Cookie 提取 access_token；自动化建议改用 PAT + setup）")
    lg.add_argument("--username", default=os.environ.get("ITSM_USERNAME", ""))
    lg.add_argument("--password", default=None, help="账号口令（argv 明文可见；优先 --password-stdin 或交互式输入）")
    lg.add_argument("--password-stdin", action="store_true", help="从 stdin 读取口令（CI/Agent 推荐）")
    lg.add_argument("--tenant-code", default="")
    lg.add_argument(
        "--save",
        nargs="?",
        const=str(DEFAULT_TOKEN_PATH),
        default="",
        help="token 写入文件；省略路径默认 ~/.itsm/itsm.token（供 --token-file 复用）",
    )
    lg.set_defaults(_tool=tool_name)
    return lg


def add_setup_parser(sub, add_parser, tool_name="itsm_cli"):
    """统一 setup 子命令：首次配置（地址 + 凭据安全落盘，验证后持久化）。"""
    sp = add_parser("setup", "首次配置：写入 API 地址与凭据（掩码/权限收紧/验证后再落盘）")
    sp.add_argument("--token-stdin", action="store_true", help="从 stdin 读取 PAT（管道传入，不进 argv）")
    sp.add_argument("--from-env", action="store_true", help="从环境变量取凭据：ITSM_TOKEN/ITSM_PAT 或 ITSM_USERNAME/ITSM_PASSWORD")
    sp.add_argument("--username", default=os.environ.get("ITSM_USERNAME", ""), help="账号登录用户名（或环境变量 ITSM_USERNAME）")
    sp.add_argument("--password-stdin", action="store_true", help="从 stdin 读取账号口令")
    sp.add_argument("--tenant-code", default="")
    sp.add_argument("--save", default="", help="token 落盘路径；省略用 config.tokenFile 或 ~/.itsm/itsm.token")
    sp.set_defaults(_tool=tool_name)
    return sp


def add_config_parser(sub, add_parser, tool_name="itsm_cli"):
    """统一 config 子命令：地址/凭据来源 + 连通性 + 身份自检（不回显明文）。"""
    cp = add_parser("config", "配置自检：生效地址/凭据来源/连通性/身份（不回显明文）")
    cp.add_argument("--offline", action="store_true", help="只打印本地解析结果，不发网络请求")
    cp.set_defaults(_tool=tool_name)
    return cp
