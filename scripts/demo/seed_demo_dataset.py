#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""ITSM 多租户演示数据集种子器（幂等可重跑）。

用途：
    为「独立演示栈」（docker-compose.demo.yml，入口 :8089）创建一套完整的
    多租户演示数据：演示服务商 + 3 个行业客户租户、各角色/岗位账号、主数据
    （服务目录/工单分类/SLA/CMDB/知识库/审批链）、全状态业务单据，以及
    跨租户 MSP 协同（分配、工作台回复/改状态、托管标记）。

运行（本机即可，直接打演示入口）：
    python scripts/demo/seed_demo_dataset.py --base-url http://172.18.3.238:8089 \
        --admin-password 'Demo@2026!Admin'

常用开关：
    --dry-run          只打印将要创建的内容，不发写请求
    --stages tenants,users,allocations,masterdata,records,cross,verify
    --user-password    演示租户用户统一口令（默认 Demo@2026!）
    --report           输出 JSON 报告路径（默认 scripts/demo/out/demo_seed_report.json）

约定：
    - 平台管理员（admin）口令由部署时的 DEMO_ADMIN_PASSWORD 决定；
    - 所有演示实体带稳定标识（租户 code、用户名、标题前缀 [DEMO-x]），按
      「先查后建」保证幂等；重复执行只补齐缺口，不产生重复数据。
    - 写请求需 CSRF：每次写前重新获取 /api/v1/csrf-token（令牌写后轮换）。
"""

from __future__ import annotations

import argparse
import http.cookiejar
import json
import os
import random
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Callable, Dict, Iterable, List, Optional, Tuple

DEMO_USER_PASSWORD_DEFAULT = "Demo@2026!"
TITLE_PREFIX = "[DEMO]"


class ApiError(Exception):
    """业务/HTTP 错误（保留状态码与响应体，便于定位）。"""

    def __init__(self, method: str, path: str, status: int, body: Any):
        self.method = method
        self.path = path
        self.status = status
        self.body = body
        super().__init__(f"{method} {path} -> HTTP {status}: {str(body)[:300]}")


class Client:
    """最小 HTTP 客户端：cookie 会话 + CSRF 自动处理 + JSON 解析。"""

    def __init__(self, base_url: str, verbose: bool = False):
        self.base = base_url.rstrip("/")
        self.verbose = verbose
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(self.jar)
        )
        self.user: Optional[dict] = None

    # ---------- 基础请求 ----------
    def _raw(self, method: str, path: str, body: Any = None,
             headers: Optional[dict] = None, timeout: int = 90) -> Tuple[int, Any]:
        url = self.base + path
        data = None
        hdrs = {"Accept": "application/json"}
        if body is not None:
            data = json.dumps(body, ensure_ascii=False).encode("utf-8")
            hdrs["Content-Type"] = "application/json"
        if headers:
            hdrs.update(headers)
        req = urllib.request.Request(url, data=data, headers=hdrs, method=method)
        try:
            with self.opener.open(req, timeout=timeout) as resp:
                raw = resp.read().decode("utf-8", "replace")
                status = resp.status
        except urllib.error.HTTPError as e:
            raw = e.read().decode("utf-8", "replace")
            status = e.code
        try:
            parsed = json.loads(raw) if raw else None
        except json.JSONDecodeError:
            parsed = raw
        if self.verbose:
            print(f"  [{method}] {path} -> {status}")
        return status, parsed

    def call(self, method: str, path: str, body: Any = None,
             csrf: bool = False, expect_code0: bool = True,
             timeout: int = 90) -> Any:
        headers = {}
        if csrf:
            headers["X-CSRF-Token"] = self.csrf_token()
        status, parsed = self._raw(method, path, body, headers, timeout)
        if status >= 400 or (expect_code0 and isinstance(parsed, dict)
                             and parsed.get("code") not in (0, None)):
            raise ApiError(method, path, status, parsed)
        if isinstance(parsed, dict) and "data" in parsed:
            return parsed.get("data")
        return parsed

    def get(self, path: str, **kw) -> Any:
        return self.call("GET", path, None, **kw)

    def write(self, method: str, path: str, body: Any = None, **kw) -> Any:
        return self.call(method, path, body, csrf=True, **kw)

    # ---------- 认证 ----------
    def csrf_token(self) -> str:
        data = self.call("GET", "/api/v1/csrf-token")
        token = (data or {}).get("csrf_token")
        if not token:
            raise RuntimeError("csrf token 缺失：/api/v1/csrf-token 未返回 data.csrf_token")
        return token

    def login(self, username: str, password: str) -> dict:
        for attempt in range(3):
            try:
                data = self.call("POST", "/api/v1/auth/login",
                                 {"username": username, "password": password},
                                 expect_code0=False)
                if not isinstance(data, dict) or "user" not in data:
                    raise RuntimeError(f"登录失败（{username}）：{data}")
                self.user = data["user"]
                return data
            except ApiError as e:
                # 登录限流（默认 10/min）：按 retryAfterSeconds 退避后重试
                body = e.body if isinstance(e.body, dict) else {}
                wait_s = int(((body.get("data") or {}).get("retryAfterSeconds")) or 0)
                if e.status == 403 and wait_s > 0 and attempt < 2:
                    time.sleep(min(wait_s + 1, 40))
                    continue
                raise
        raise RuntimeError("unreachable")

    def whoami(self) -> Optional[dict]:
        try:
            return self.get("/api/v1/auth/me")
        except ApiError:
            return None


@dataclass
class Counters:
    created: int = 0
    existing: int = 0
    skipped: int = 0
    failed: int = 0
    actions: List[str] = field(default_factory=list)

    def note(self, kind: str, msg: str) -> None:
        self.actions.append(f"[{kind}] {msg}")


@dataclass
class Ctx:
    """运行上下文：客户端、口令、计数器、dry-run。"""
    client: Client
    password: str
    dry_run: bool
    c: Counters
    cache: Dict[str, Any] = field(default_factory=dict)

    def log(self, msg: str) -> None:
        print(msg, flush=True)


def slug(s: str) -> str:
    return "".join(ch if ch.isalnum() else "-" for ch in s.lower())


# =====================================================================
# 1. 数据集定义：租户 / 用户（角色·岗位）
# =====================================================================

@dataclass
class TenantSpec:
    key: str
    code: str
    name: str
    type: str                      # msp_provider | msp_customer
    industry: str = ""
    manager_name: str = ""


TENANTS: List[TenantSpec] = [
    TenantSpec("provider", "demo-provider", "云维科技（演示服务商）", "msp_provider"),
    TenantSpec("mfg", "demo-mfg", "华辰智能制造", "msp_customer", "制造业"),
    TenantSpec("med", "demo-med", "仁和医疗集团", "msp_customer", "医疗"),
    TenantSpec("edu", "demo-edu", "云帆教育科技", "msp_customer", "教育"),
]


@dataclass
class UserSpec:
    key: str
    username: str
    name: str
    role: str                      # users.role 主角色（domain/role 词表）
    msp_role: str = ""             # provider_admin | provider_agent（仅服务商租户）
    position: str = ""             # 演示用「岗位」描述（写入 department 便于展示）
    email: str = ""


def build_users() -> Dict[str, List[UserSpec]]:
    """按租户 key 返回用户清单（用户名全局唯一、口令统一为演示口令）。"""
    users: Dict[str, List[UserSpec]] = {
        "provider": [
            UserSpec("msp_manager", "demo.msp.manager", "林致远（服务商经理）",
                     "end_user", "provider_admin", "MSP 服务经理"),
            UserSpec("msp_agent1", "demo.msp.agent1", "苏晴（一线工程师）",
                     "end_user", "provider_agent", "MSP 技术支持"),
            UserSpec("msp_agent2", "demo.msp.agent2", "郑凯（二线工程师）",
                     "end_user", "provider_agent", "MSP 高级支持"),
        ],
        "mfg": [
            UserSpec("admin", "demo.mfg.admin", "陈志远（制造-管理员）",
                     "admin", "", "信息化管理员"),
            UserSpec("agent", "demo.mfg.agent", "王莉（制造-服务台）",
                     "agent", "", "IT 服务台"),
            UserSpec("lead", "demo.mfg.lead", "赵建国（制造-部门经理）",
                     "manager", "", "生产部经理"),
            UserSpec("user", "demo.mfg.user", "刘洋（制造-员工）",
                     "end_user", "", "车间主管"),
        ],
        "med": [
            UserSpec("admin", "demo.med.admin", "周敏（医疗-管理员）",
                     "admin", "", "信息科管理员"),
            UserSpec("agent", "demo.med.agent", "吴桐（医疗-服务台）",
                     "agent", "", "信息科工程师"),
            UserSpec("lead", "demo.med.lead", "孙倩（医疗-科室主任）",
                     "manager", "", "门诊科室主任"),
            UserSpec("user", "demo.med.user", "李晨（医疗-医生）",
                     "end_user", "", "主治医师"),
        ],
        "edu": [
            UserSpec("admin", "demo.edu.admin", "黄鹤（教育-管理员）",
                     "admin", "", "信息中心管理员"),
            UserSpec("agent", "demo.edu.agent", "许文（教育-服务台）",
                     "agent", "", "IT 支持工程师"),
            UserSpec("lead", "demo.edu.lead", "钱多（教育-教务主任）",
                     "manager", "", "教务主任"),
            UserSpec("user", "demo.edu.user", "林小满（教育-教师）",
                     "end_user", "", "任课教师"),
        ],
    }
    # 补默认邮箱
    for lst in users.values():
        for u in lst:
            if not u.email:
                u.email = f"{u.username}@demo.example.com"
    return users


USERS = build_users()


# MSP 分配（服务商员工 → 客户租户；role 为 allocation 角色）
ALLOCATIONS: List[Tuple[str, str, str]] = [
    # (用户 key, 客户租户 key, allocation role)
    ("msp_manager", "mfg", "primary"),
    ("msp_manager", "med", "primary"),
    ("msp_manager", "edu", "primary"),
    ("msp_agent1", "mfg", "backup"),
    ("msp_agent1", "edu", "primary"),
    ("msp_agent2", "med", "backup"),
    ("msp_agent2", "mfg", "specialist"),
]


# =====================================================================
# 2. 平台侧基础能力
# =====================================================================

def list_tenants(ctx: Ctx) -> List[dict]:
    data = ctx.client.get("/api/v1/tenants?page=1&pageSize=100")
    return (data or {}).get("tenants") or []


def ensure_tenant(ctx: Ctx, spec: TenantSpec, provider_id: Optional[int]) -> Optional[int]:
    """按 code 幂等创建租户并返回 id。"""
    for t in list_tenants(ctx):
        if t.get("code") == spec.code:
            ctx.c.existing += 1
            ctx.c.note("existing", f"租户 {spec.code} (id={t['id']})")
            return int(t["id"])
    if ctx.dry_run:
        ctx.c.note("dry-run", f"将创建租户 {spec.code}（{spec.name}）")
        return None
    body: Dict[str, Any] = {"name": spec.name, "code": spec.code, "type": spec.type}
    if spec.type == "msp_customer":
        body["mspProviderId"] = provider_id
    data = ctx.client.write("POST", "/api/v1/tenants", body)
    tid = int(data["id"])
    ctx.c.created += 1
    ctx.c.note("created", f"租户 {spec.code} {spec.name} (id={tid})")
    return tid


def provision_tenant(ctx: Ctx, tenant_id: int, code: str) -> bool:
    """模板开通（roles/menus/SLA/catalog/BPMN...）；幂等，返回 ready。"""
    if ctx.dry_run:
        return True
    data = ctx.client.write("POST", f"/api/v1/tenants/{tenant_id}/provision", {})
    ready = bool((data or {}).get("ready"))
    if ready:
        ctx.c.note("provisioned", f"租户 {code} 模板开通 ready=true")
    else:
        ctx.c.note("warn", f"租户 {code} 模板开通 ready=false：{data}")
    return ready


def ensure_user(ctx: Ctx, tenant_id: int, spec: UserSpec, password: str) -> Optional[int]:
    """平台通道建号（幂等：USERNAME_EXISTS 视为已存在并回读 id）。"""
    if ctx.dry_run:
        ctx.c.note("dry-run", f"将建号 {spec.username} @租户{tenant_id}（{spec.role}/{spec.msp_role}）")
        return None
    body: Dict[str, Any] = {
        "username": spec.username,
        "email": spec.email,
        "name": spec.name,
        "password": password,
        "role": spec.role,
        "department": spec.position,
    }
    if spec.msp_role:
        body["mspRole"] = spec.msp_role
    try:
        data = ctx.client.write("POST", f"/api/v1/tenants/{tenant_id}/users", body)
        uid = int(data["id"])
        ctx.c.created += 1
        ctx.c.note("created", f"用户 {spec.username} ({spec.role or spec.msp_role}) id={uid}")
        return uid
    except ApiError as e:
        code = ""
        if isinstance(e.body, dict):
            code = str(e.body.get("error") or e.body.get("code") or "")
        if "USERNAME_EXISTS" in code or "已存在" in str(e.body):
            ctx.c.existing += 1
            ctx.c.note("existing", f"用户 {spec.username}（已存在）")
            return None
        raise


def resolve_tenant_ids(ctx: Ctx) -> None:
    """按 code 解析全部演示租户 id 并写入缓存（支持分阶段/分进程独立执行）。"""
    by_code = {t.get("code"): t for t in list_tenants(ctx)}
    for spec in TENANTS:
        t = by_code.get(spec.code)
        if t:
            ctx.cache[f"tenant:{spec.key}"] = int(t["id"])


def resolve_user_id(ctx: Ctx, username: str, password: str) -> Optional[int]:
    """通过登录解析用户 id（缓存缺失时使用；顺带校验账号口令有效）。"""
    cached = ctx.cache.get(f"user:{username}")
    if cached:
        return int(cached)
    cli = Client(ctx.client.base, ctx.client.verbose)
    try:
        cli.login(username, password)
    except Exception:  # noqa: BLE001
        return None
    uid = (cli.user or {}).get("id")
    if uid:
        ctx.cache[f"user:{username}"] = int(uid)
        return int(uid)
    return None


# =====================================================================
# 3. 阶段实现：租户 / 用户 / 分配
# =====================================================================

def stage_tenants(ctx: Ctx) -> None:
    ctx.log("== 阶段 1/6：租户与模板开通 ==")
    provider_id: Optional[int] = None
    for spec in TENANTS:
        if spec.type == "msp_provider":
            tid = ensure_tenant(ctx, spec, None)
            provider_id = tid
            ctx.cache["provider_id"] = tid
        else:
            tid = ensure_tenant(ctx, spec, provider_id)
        if tid:
            ctx.cache[f"tenant:{spec.key}"] = tid
            provision_tenant(ctx, tid, spec.code)


def stage_users(ctx: Ctx) -> None:
    ctx.log("== 阶段 2/6：多角色账号 ==")
    resolve_tenant_ids(ctx)
    for spec in TENANTS:
        tid = ctx.cache.get(f"tenant:{spec.key}")
        if not tid:
            ctx.c.note("skip", f"租户 {spec.code} 不存在，跳过建号")
            continue
        for u in USERS[spec.key]:
            uid = ensure_user(ctx, tid, u, ctx.password)
            if uid:
                ctx.cache[f"user:{u.username}"] = uid


def stage_allocations(ctx: Ctx) -> None:
    ctx.log("== 阶段 3/6：MSP 分配（服务商 ↔ 客户） ==")
    resolve_tenant_ids(ctx)
    manager = USERS["provider"][0]
    cli = Client(ctx.client.base, ctx.client.verbose)
    cli.login(manager.username, ctx.password)
    # 现有分配集合
    existing = set()
    try:
        data = cli.get("/api/v1/msp/allocations?page=1&pageSize=100")
        for row in (data or {}).get("allocations") or []:
            existing.add((int(row.get("mspUserId") or 0), int(row.get("customerTenantId") or 0),
                          str(row.get("role") or "")))
    except ApiError as e:
        ctx.c.note("warn", f"读取分配列表失败（按创建幂等继续）：{e.body}")

    for ukey, tkey, role in ALLOCATIONS:
        uname = next(u.username for u in USERS["provider"] if u.key == ukey)
        uid = resolve_user_id(ctx, uname, ctx.password)
        tid = ctx.cache.get(f"tenant:{tkey}")
        if not uid or not tid:
            ctx.c.note("skip", f"分配 {uname}→{tkey} 缺少 id（用户或租户未就绪）")
            continue
        if (uid, tid, role) in existing:
            ctx.c.existing += 1
            ctx.c.note("existing", f"分配 {uname}→{tkey}（{role}）")
            continue
        if ctx.dry_run:
            ctx.c.note("dry-run", f"将分配 {uname}→{tkey}（{role}）")
            continue
        try:
            cli.write("POST", "/api/v1/msp/allocations",
                      {"mspUserId": uid, "customerTenantId": tid, "role": role})
            ctx.c.created += 1
            ctx.c.note("created", f"分配 {uname}→{tkey}（{role}）")
        except ApiError as e:
            body = json.dumps(e.body, ensure_ascii=False) if isinstance(e.body, dict) else str(e.body)
            if "ALLOCATION_EXISTS" in body:
                ctx.c.existing += 1
            else:
                ctx.c.failed += 1
                ctx.c.note("fail", f"分配 {uname}→{tkey}（{role}）：{body[:160]}")


# =====================================================================
# 9. 主流程 / 报告
# =====================================================================

STAGES: Dict[str, Callable[[Ctx], None]] = {
    "tenants": stage_tenants,
    "users": stage_users,
    "allocations": stage_allocations,
}


def credentials_table(password: str) -> str:
    lines = ["| 租户 | 用户名 | 口令 | 角色/岗位 |", "|---|---|---|---|"]
    for spec in TENANTS:
        users = USERS[spec.key]
        for i, u in enumerate(users):
            tenant_label = spec.name if i == 0 else ""
            role = u.msp_role or u.role
            lines.append(f"| {tenant_label} | `{u.username}` | `{password}` | {role}（{u.position}） |")
    lines.append(f"| 平台 | `admin` | `Demo@2026!Admin` | super_admin（平台管理员） |")
    return "\n".join(lines)


def parse_args(argv: Optional[List[str]] = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(description="ITSM 多租户演示数据集种子器（幂等）")
    p.add_argument("--base-url", default=os.environ.get("DEMO_BASE_URL", "http://172.18.3.238:8089"))
    p.add_argument("--admin-user", default="admin")
    p.add_argument("--admin-password", default=os.environ.get("DEMO_ADMIN_PASSWORD", ""))
    p.add_argument("--user-password", default=os.environ.get("DEMO_USER_PASSWORD", DEMO_USER_PASSWORD_DEFAULT))
    p.add_argument("--stages", default="tenants,users,allocations",
                   help="逗号分隔：tenants,users,allocations,masterdata,records,cross,verify")
    p.add_argument("--dry-run", action="store_true")
    p.add_argument("--verbose", action="store_true")
    p.add_argument("--report", default=os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                                    "out", "demo_seed_report.json"))
    return p.parse_args(argv)


def main(argv: Optional[List[str]] = None) -> int:
    args = parse_args(argv)
    if not args.admin_password:
        print("缺少 --admin-password（或环境变量 DEMO_ADMIN_PASSWORD）", file=sys.stderr)
        return 2

    cli = Client(args.base_url, verbose=args.verbose)
    try:
        admin = cli.login(args.admin_user, args.admin_password)
    except Exception as e:  # noqa: BLE001
        print(f"平台管理员登录失败：{e}", file=sys.stderr)
        return 3
    print(f"[ok] 平台管理员登录：{admin['user'].get('username')} "
          f"(role={admin['user'].get('role')}, tenant={admin['user'].get('tenantId')})")

    ctx = Ctx(cli, args.user_password, args.dry_run, Counters())
    stages = [s.strip() for s in args.stages.split(",") if s.strip()]
    unknown = [s for s in stages if s not in STAGES]
    if unknown:
        print(f"未知阶段：{unknown}（可用：{sorted(STAGES)}）", file=sys.stderr)
        return 2

    t0 = time.time()
    for name in stages:
        STAGES[name](ctx)

    elapsed = time.time() - t0
    print("\n== 汇总 ==")
    print(f"新建 {ctx.c.created}｜已存在 {ctx.c.existing}｜跳过 {ctx.c.skipped}｜失败 {ctx.c.failed}｜耗时 {elapsed:.1f}s")
    print("\n== 演示账号清单 ==")
    print(credentials_table(ctx.password))

    os.makedirs(os.path.dirname(args.report), exist_ok=True)
    with open(args.report, "w", encoding="utf-8") as f:
        json.dump({
            "base_url": args.base_url,
            "stages": stages,
            "dry_run": args.dry_run,
            "counters": ctx.c.__dict__,
            "elapsed_sec": round(elapsed, 1),
            "credentials_markdown": credentials_table(ctx.password),
        }, f, ensure_ascii=False, indent=2)
    print(f"\n报告：{args.report}")
    return 1 if ctx.c.failed else 0


# 说明：新增阶段在本文件末尾统一注册（STAGES.update），入口守卫置于文件末尾，
# 保证所有数据集定义与阶段函数先于 main() 求值。


# =====================================================================
# 4. 数据集定义：主数据与业务单据
# =====================================================================

# 知识库文章（按行业定制；发布走 /publish）
KB_ARTICLES: Dict[str, List[dict]] = {
    "mfg": [
        {"title": "车间 MES 终端无法登录排查手册", "category": "故障排查",
         "content": "适用：车间平板/MES 终端登录失败。\n\n1. 确认终端与车间 AP 连接正常；\n2. 检查终端时间是否偏差超过 5 分钟（会导致认证失败）；\n3. 重启 MES 客户端后重试；\n4. 若同班组多台同时失败，报服务台并注明产线编号。"},
        {"title": "新员工 IT 账号开通流程（制造基地）", "category": "服务指南",
         "content": "1. HR 发起入职后自动生成服务请求；\n2. IT 按岗位模板开通：域账号/邮箱/内网/VPN；\n3. 产线设备权限需车间主管确认；\n4. 标准时效 1 个工作日。"},
    ],
    "med": [
        {"title": "门诊系统打印处方失败应急指引", "category": "应急预案",
         "content": "1. 确认打印机电源与纸盒；\n2. 检查打印服务状态（信息科值班）；\n3. 紧急时切换备用诊室打印；\n4. 记录患者就诊号与时间，事后补打并归档。"},
        {"title": "医疗设备网络接入规范", "category": "规范制度",
         "content": "1. 新设备接入须先登记资产并申请 IP；\n2. 禁止私接交换机/路由器；\n3. 影像类设备走专网 VLAN；\n4. 接入变更需提交变更单并评估影响。"},
    ],
    "edu": [
        {"title": "在线课堂卡顿自助排查", "category": "常见问题",
         "content": "1. 测速（建议 ≥ 10Mbps）；\n2. 关闭占用带宽的下载/视频；\n3. 优先使用有线网络或 5G 频段 Wi-Fi；\n4. 全班普遍卡顿时联系信息中心检查出口带宽。"},
        {"title": "教师办公设备借用流程", "category": "服务指南",
         "content": "1. 在服务目录提交「设备借用」申请；\n2. 教务主任审批；\n3. 信息中心核对库存后发放并登记；\n4. 归还时检查设备完好性。"},
    ],
}

# CMDB 配置项样本（type_code 需命中租户 CI 类型；匹配不到时回退第一个类型）
CI_SAMPLES: Dict[str, List[dict]] = {
    "mfg": [
        {"name": "MES-APP-01 制造执行系统", "type_code": "application", "status": "active",
         "environment": "production", "criticality": "high", "assetTag": "AST-MFG-0001",
         "attributes": {"版本": "v8.2", "负责人": "信息部"}},
        {"name": "MES-DB-01 生产数据库", "type_code": "database", "status": "active",
         "environment": "production", "criticality": "critical", "assetTag": "AST-MFG-0002",
         "attributes": {"引擎": "MySQL 8.0", "规格": "16C64G"}},
        {"name": "SW-CORE-01 核心交换机", "type_code": "network", "status": "active",
         "environment": "production", "criticality": "critical", "assetTag": "AST-MFG-0003",
         "attributes": {"厂商": "华为", "型号": "S12700E"}},
        {"name": "web-03 车间看板服务器", "type_code": "server", "status": "maintenance",
         "environment": "production", "criticality": "medium", "assetTag": "AST-MFG-0004",
         "attributes": {"CPU": "16 vCPU", "内存": "32G"}},
    ],
    "med": [
        {"name": "HIS-APP-01 医院信息系统", "type_code": "application", "status": "active",
         "environment": "production", "criticality": "critical", "assetTag": "AST-MED-0001",
         "attributes": {"版本": "v6.5", "厂商": "东华"}},
        {"name": "PACS-STORAGE-01 影像存储", "type_code": "storage", "status": "active",
         "environment": "production", "criticality": "high", "assetTag": "AST-MED-0002",
         "attributes": {"容量": "200TB", "协议": "NFS"}},
        {"name": "门诊自助机-01", "type_code": "server", "status": "active",
         "environment": "production", "criticality": "medium", "assetTag": "AST-MED-0003",
         "attributes": {"位置": "门诊大厅"}},
    ],
    "edu": [
        {"name": "在线课堂平台", "type_code": "application", "status": "active",
         "environment": "production", "criticality": "high", "assetTag": "AST-EDU-0001",
         "attributes": {"并发": "5000", "CDN": "已接入"}},
        {"name": "校园网出口防火墙", "type_code": "network", "status": "active",
         "environment": "production", "criticality": "critical", "assetTag": "AST-EDU-0002",
         "attributes": {"带宽": "10Gbps"}},
        {"name": "教务数据库主库", "type_code": "database", "status": "active",
         "environment": "production", "criticality": "high", "assetTag": "AST-EDU-0003",
         "attributes": {"引擎": "PostgreSQL 17"}},
    ],
}

# 业务单据（每租户一组；requester 指定登录身份；path 为状态流转路径）
# assignee: tenant=租户服务台, admin=租户管理员, msp=服务商技术员（MSP 托管）
TICKET_SPECS: Dict[str, List[dict]] = {
    "mfg": [
        {"key": "vpn", "title": "[DEMO] VPN 无法连接（车间办公区）", "type": "incident",
         "priority": "high", "requester": "user", "assignee": "tenant", "path": ["in_progress"],
         "comments": ["已按知识库《车间 MES 终端无法登录排查手册》自查，仍无法连接。", "已抓包定位，怀疑是运营商侧线路抖动，继续跟进。"]},
        {"key": "mes_slow", "title": "[DEMO] MES 报工页面加载缓慢", "type": "incident",
         "priority": "medium", "requester": "user", "assignee": "tenant", "path": ["in_progress", "pending"],
         "comments": ["二号线多名员工反馈，上午 10 点后明显。", "初步定位为报表查询拖慢数据库，先转待处理，候窗口优化。"]},
        {"key": "printer", "title": "[DEMO] 车间标签打印机离线", "type": "incident",
         "priority": "medium", "requester": "user", "assignee": "tenant", "path": ["in_progress", "resolved", "closed"],
         "comments": ["打印机红灯常亮。", "更换网线后恢复，已电话回访确认可用。"]},
        {"key": "account", "title": "[DEMO] 新员工入职账号开通（产线质检岗）", "type": "service_request",
         "priority": "medium", "requester": "lead", "assignee": "tenant", "path": ["in_progress", "resolved", "closed"], "comments": []},
        {"key": "firewall", "title": "[DEMO] 研发区出站端口开通申请（8443）", "type": "change",
         "priority": "low", "requester": "user", "assignee": "admin", "path": ["in_progress"], "comments": []},
        {"key": "cancel", "title": "[DEMO] 打印机驱动安装申请（重复提交）", "type": "service_request",
         "priority": "low", "requester": "user", "assignee": "", "path": ["cancelled"], "comments": ["重复提交，取消。"]},
        {"key": "msp_db", "title": "[DEMO] 主从复制延迟告警处理", "type": "incident",
         "priority": "critical", "requester": "agent", "assignee": "msp", "path": ["in_progress"],
         "comments": ["已升级 MSP 支持。", "[服务商] 已扩容从库规格并调整复制并行度，延迟回落到 1s 内。"]},
        {"key": "msp_patch", "title": "[DEMO] 月度安全补丁窗口执行", "type": "change",
         "priority": "high", "requester": "admin", "assignee": "msp", "path": ["in_progress"],
         "comments": ["[服务商] 补丁已按窗口执行完成，验证通过。"]},
        {"key": "new1", "title": "[DEMO] 车间显示器闪烁报修（待受理）", "type": "incident",
         "priority": "low", "requester": "user", "assignee": "", "path": [], "comments": []},
        {"key": "open1", "title": "[DEMO] 门禁卡权限开通申请", "type": "service_request",
         "priority": "medium", "requester": "user", "assignee": "tenant", "path": [], "comments": []},
        {"key": "pending1", "title": "[DEMO] 报表权限变更（待用户确认清单）", "type": "service_request",
         "priority": "medium", "requester": "lead", "assignee": "tenant", "path": ["in_progress", "pending"], "comments": []},
        {"key": "resolved1", "title": "[DEMO] 邮箱签名模板调整", "type": "service_request",
         "priority": "low", "requester": "user", "assignee": "tenant", "path": ["in_progress", "resolved"],
         "comments": ["模板已更新，请确认效果。"]},
    ],
    "med": [
        {"key": "his_login", "title": "[DEMO] HIS 医生站登录超时", "type": "incident",
         "priority": "critical", "requester": "user", "assignee": "tenant", "path": ["in_progress"],
         "comments": ["门诊高峰期多名医生反馈。", "已临时扩容应用节点，正在定位连接池瓶颈。"]},
        {"key": "printer_rx", "title": "[DEMO] 处方打印机卡纸", "type": "incident",
         "priority": "medium", "requester": "user", "assignee": "tenant", "path": ["in_progress", "resolved", "closed"],
         "comments": ["已清理卡纸并更换纸卷。"]},
        {"key": "pacs", "title": "[DEMO] PACS 影像调阅缓慢", "type": "incident",
         "priority": "high", "requester": "agent", "assignee": "tenant", "path": ["in_progress", "pending"], "comments": []},
        {"key": "device", "title": "[DEMO] 新购监护仪入网登记", "type": "service_request",
         "priority": "medium", "requester": "lead", "assignee": "tenant", "path": ["in_progress", "resolved", "closed"], "comments": []},
        {"key": "vlan", "title": "[DEMO] 影像设备 VLAN 调整变更", "type": "change",
         "priority": "medium", "requester": "admin", "assignee": "admin", "path": ["in_progress"], "comments": []},
        {"key": "cancel2", "title": "[DEMO] 自助机屏保设置申请（撤回）", "type": "service_request",
         "priority": "low", "requester": "user", "assignee": "", "path": ["cancelled"], "comments": []},
        {"key": "msp_sec", "title": "[DEMO] 等保整改复核支持", "type": "service_request",
         "priority": "high", "requester": "admin", "assignee": "msp", "path": ["in_progress"],
         "comments": ["[服务商] 已提交整改清单，待院方确认。"]},
        {"key": "new1", "title": "[DEMO] 护士站电脑蓝屏（待受理）", "type": "incident",
         "priority": "high", "requester": "user", "assignee": "", "path": [], "comments": []},
        {"key": "open1", "title": "[DEMO] 科室共享盘扩容申请", "type": "service_request",
         "priority": "medium", "requester": "lead", "assignee": "tenant", "path": [], "comments": []},
        {"key": "pending1", "title": "[DEMO] 医生站账号权限调整（待科室确认）", "type": "service_request",
         "priority": "medium", "requester": "lead", "assignee": "tenant", "path": ["in_progress", "pending"], "comments": []},
        {"key": "resolved1", "title": "[DEMO] 叫号屏显示异常", "type": "incident",
         "priority": "low", "requester": "user", "assignee": "tenant", "path": ["in_progress", "resolved"],
         "comments": ["重启显示服务后恢复。"]},
    ],
    "edu": [
        {"key": "class", "title": "[DEMO] 在线课堂高峰期卡顿", "type": "incident",
         "priority": "high", "requester": "user", "assignee": "tenant", "path": ["in_progress"],
         "comments": ["晚自习时段 7-8 点集中出现。", "已与运营商核对出口带宽，扩容中。"]},
        {"key": "wifi", "title": "[DEMO] 教学楼 3 层 Wi-Fi 断连", "type": "incident",
         "priority": "medium", "requester": "user", "assignee": "tenant", "path": ["in_progress", "resolved", "closed"],
         "comments": ["调整 AP 信道后恢复。"]},
        {"key": "account_edu", "title": "[DEMO] 新教师账号及邮箱开通", "type": "service_request",
         "priority": "medium", "requester": "lead", "assignee": "tenant", "path": ["in_progress", "resolved", "closed"], "comments": []},
        {"key": "borrow", "title": "[DEMO] 教室投影仪借用申请", "type": "service_request",
         "priority": "low", "requester": "user", "assignee": "tenant", "path": ["in_progress"], "comments": []},
        {"key": "upgrade", "title": "[DEMO] 教务系统版本升级变更", "type": "change",
         "priority": "medium", "requester": "admin", "assignee": "admin", "path": ["in_progress", "pending"], "comments": []},
        {"key": "cancel3", "title": "[DEMO] 多媒体教室巡检申请（合并处理）", "type": "service_request",
         "priority": "low", "requester": "user", "assignee": "", "path": ["cancelled"], "comments": []},
        {"key": "msp_cloud", "title": "[DEMO] 云资源巡检与优化建议", "type": "change",
         "priority": "medium", "requester": "admin", "assignee": "msp", "path": ["in_progress"],
         "comments": ["[服务商] 已完成巡检，输出 3 条优化建议。"]},
        {"key": "new1", "title": "[DEMO] 教室一体机无法开机（待受理）", "type": "incident",
         "priority": "medium", "requester": "user", "assignee": "", "path": [], "comments": []},
        {"key": "open1", "title": "[DEMO] 教务系统导出权限申请", "type": "service_request",
         "priority": "low", "requester": "user", "assignee": "tenant", "path": [], "comments": []},
        {"key": "pending1", "title": "[DEMO] 校园卡消费记录核对（待确认）", "type": "service_request",
         "priority": "medium", "requester": "lead", "assignee": "tenant", "path": ["in_progress", "pending"], "comments": []},
        {"key": "resolved1", "title": "[DEMO] 宿舍区网络认证失败", "type": "incident",
         "priority": "high", "requester": "user", "assignee": "tenant", "path": ["in_progress", "resolved"],
         "comments": ["认证服务重启后恢复。"]},
    ],
}


# =====================================================================
# 5. 会话与通用工具（阶段 4+）
# =====================================================================

def tenant_of(tkey: str) -> TenantSpec:
    return next(s for s in TENANTS if s.key == tkey)


def user_of(tkey: str, ukey: str) -> UserSpec:
    return next(u for u in USERS[tkey] if u.key == ukey)


def session_as(ctx: Ctx, tkey: str, ukey: str) -> Client:
    """以指定租户的指定岗位登录（独立会话；顺带缓存 user id）。"""
    u = user_of(tkey, ukey)
    cached = ctx.cache.get(f"session:{u.username}")
    if isinstance(cached, Client) and cached.user:
        return cached
    cli = Client(ctx.client.base, ctx.client.verbose)
    cli.login(u.username, ctx.password)
    uid = (cli.user or {}).get("id")
    if uid:
        ctx.cache[f"user:{u.username}"] = int(uid)
    ctx.cache[f"session:{u.username}"] = cli
    return cli


def extract_list(data: Any, keys: Iterable[str]) -> List[dict]:
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        for k in keys:
            v = data.get(k)
            if isinstance(v, list):
                return v
    return []


def find_ticket(cli: Client, title: str) -> Optional[dict]:
    data = cli.get(f"/api/v1/tickets?search={urllib.parse.quote(title)}&page=1&pageSize=50")
    rows = extract_list(data, ("tickets", "items", "list"))
    for row in rows:
        if isinstance(row, dict) and row.get("title") == title:
            return row
    return None


def expected_final_status(tk: dict) -> str:
    """按单据规格推导最终状态（空路径=创建后语义状态）。"""
    path = tk.get("path", [])
    if path:
        return str(path[-1])
    return "open" if tk.get("assignee") in ("tenant", "admin", "msp") else "new"


# =====================================================================
# 6. 阶段实现：主数据（知识库 / CMDB 配置项）
# =====================================================================

def stage_masterdata(ctx: Ctx) -> None:
    ctx.log("== 阶段 4/6：主数据（知识库 / CMDB） ==")
    resolve_tenant_ids(ctx)
    for spec in TENANTS:
        if spec.type != "msp_customer":
            continue
        tid = ctx.cache.get(f"tenant:{spec.key}")
        if not tid:
            ctx.c.note("skip", f"租户 {spec.code} 不存在，跳过主数据")
            continue
        try:
            admin = session_as(ctx, spec.key, "admin")
        except Exception as e:  # noqa: BLE001
            ctx.c.failed += 1
            ctx.c.note("fail", f"{spec.code} 管理员登录失败：{e}")
            continue

        # --- 知识库 ---
        try:
            arts = extract_list(admin.get("/api/v1/knowledge/articles?page=1&pageSize=100"),
                                ("articles", "items", "list"))
        except ApiError:
            arts = []
        titles = {a.get("title") for a in arts if isinstance(a, dict)}
        for art in KB_ARTICLES.get(spec.key, []):
            if art["title"] in titles:
                ctx.c.existing += 1
                continue
            if ctx.dry_run:
                ctx.c.note("dry-run", f"将创建知识《{art['title']}》@{spec.code}")
                continue
            try:
                created = admin.write("POST", "/api/v1/knowledge/articles", {
                    "title": art["title"], "content": art["content"], "category": art["category"],
                })
                aid = (created or {}).get("id")
                if aid:
                    admin.write("POST", f"/api/v1/knowledge/articles/{aid}/publish", {})
                ctx.c.created += 1
                ctx.c.note("created", f"知识《{art['title']}》@{spec.code}")
            except ApiError as e:
                ctx.c.failed += 1
                ctx.c.note("fail", f"知识《{art['title']}》@{spec.code}：{str(e.body)[:140]}")

        # --- CMDB 配置项 ---
        try:
            type_rows = extract_list(admin.get("/api/v1/cmdb/ci-types?page=1&pageSize=100"),
                                     ("types", "ciTypes", "items", "list"))
        except ApiError:
            type_rows = []
        type_by_code: Dict[str, int] = {}
        for t in type_rows:
            if isinstance(t, dict) and t.get("id"):
                # 契约：CI 类型列表项以 name 作为稳定 key（code 为兼容字段）
                for key in (t.get("code"), t.get("name")):
                    if key:
                        type_by_code[str(key)] = int(t["id"])
        default_type = next(iter(type_by_code.values()), 0)
        for ci in CI_SAMPLES.get(spec.key, []):
            if ctx.dry_run:
                ctx.c.note("dry-run", f"将创建 CI {ci['name']}@{spec.code}")
                continue
            try:
                found = extract_list(admin.get(
                    f"/api/v1/cmdb/cis?search={urllib.parse.quote(ci['name'])}&page=1&pageSize=50"),
                    ("cis", "items", "list"))
            except ApiError:
                found = []
            if any(isinstance(r, dict) and r.get("name") == ci["name"] for r in found):
                ctx.c.existing += 1
                continue
            body = dict(ci)
            body.pop("type_code", None)
            body["ciTypeId"] = type_by_code.get(ci.get("type_code", ""), default_type)
            try:
                admin.write("POST", "/api/v1/cmdb/cis", body)
                ctx.c.created += 1
                ctx.c.note("created", f"CI {ci['name']}@{spec.code}")
            except ApiError as e:
                ctx.c.failed += 1
                ctx.c.note("fail", f"CI {ci['name']}@{spec.code}：{str(e.body)[:140]}")


# =====================================================================
# 7. 阶段实现：业务单据（全状态工单 + 评论）与跨租户协同
# =====================================================================

def _transition(ctx: Ctx, cli: Client, tid: int, status: str, tag: str) -> None:
    """按状态语义选择入口：resolved→/resolve，closed→/close，其余→PUT /status。"""
    if status == "resolved":
        cli.write("POST", f"/api/v1/tickets/{tid}/resolve",
                  {"resolution": "演示数据：已完成处理并验证，问题不再复现。"})
    elif status == "closed":
        cli.write("POST", f"/api/v1/tickets/{tid}/close",
                  {"closeReason": "演示数据：回访确认，关闭工单。"})
    else:
        cli.write("PUT", f"/api/v1/tickets/{tid}/status", {"status": status})
    ctx.c.note("transition", f"{tag} → {status}")


def _transition_msp(ctx: Ctx, prov: Client, tid: int, customer_tid: int, status: str, tag: str) -> None:
    """MSP 托管工单由服务商在工作台推进状态（跨租户入口）。"""
    prov.write("POST", f"/api/v1/msp/tickets/{tid}/status",
               {"customerTenantId": customer_tid, "status": status})
    ctx.c.note("transition", f"{tag} → {status}（MSP 工作台）")


def stage_records(ctx: Ctx) -> None:
    ctx.log("== 阶段 5/6：业务单据（全状态 + 评论 + MSP 托管） ==")
    resolve_tenant_ids(ctx)
    provider_agent_by_tenant = {"mfg": "msp_agent1", "med": "msp_agent2", "edu": "msp_agent1"}

    for spec in TENANTS:
        if spec.type != "msp_customer":
            continue
        tid = ctx.cache.get(f"tenant:{spec.key}")
        if not tid:
            ctx.c.note("skip", f"租户 {spec.code} 不存在，跳过单据")
            continue
        try:
            admin = session_as(ctx, spec.key, "admin")
            agent = session_as(ctx, spec.key, "agent")
            user = session_as(ctx, spec.key, "user")
            lead = session_as(ctx, spec.key, "lead")
        except Exception as e:  # noqa: BLE001
            ctx.c.failed += 1
            ctx.c.note("fail", f"{spec.code} 岗位会话登录失败：{e}")
            continue
        sides = {"admin": admin, "agent": agent, "user": user, "lead": lead}
        ids = {k: int((c.user or {}).get("id") or 0) for k, c in sides.items()}

        for tk in TICKET_SPECS.get(spec.key, []):
            tag = f"[{spec.code}:{tk['key']}]"
            creator = sides.get(tk["requester"], user)
            existing = None
            if not ctx.dry_run:
                try:
                    existing = find_ticket(agent, tk["title"]) or find_ticket(admin, tk["title"])
                except ApiError:
                    existing = None
            if existing:
                # 自愈：状态与规格不符（含早期运行被跨租户阶段误推进）时删除重建，
                # 保证幂等重跑后状态覆盖仍然正确。
                if str(existing.get("status")) == expected_final_status(tk):
                    ctx.c.existing += 1
                    ctx.c.note("existing", f"工单《{tk['title']}》@{spec.code} id={existing.get('id')}")
                    continue
                try:
                    admin.write("DELETE", f"/api/v1/tickets/{existing.get('id')}", None)
                    ctx.c.note("repair", f"{tag} 状态 {existing.get('status')}≠{expected_final_status(tk)}，删除重建")
                except ApiError as e:
                    ctx.c.note("warn", f"{tag} 状态修复删除失败：{str(e.body)[:120]}")
                    ctx.c.existing += 1
                    continue
            if ctx.dry_run:
                ctx.c.note("dry-run", f"将创建工单《{tk['title']}》@{spec.code}")
                continue

            try:
                created = creator.write("POST", "/api/v1/tickets", {
                    "title": tk["title"],
                    "description": f"{tk['title']}（多租户演示数据集自动生成；租户={spec.name}）。",
                    "priority": tk["priority"],
                    "type": tk["type"],
                })
                ticket_id = int((created or {}).get("id") or 0)
                if not ticket_id:
                    raise ApiError("POST", "/api/v1/tickets", 200, created)
                ctx.c.created += 1
                ctx.c.note("created", f"工单《{tk['title']}》@{spec.code} id={ticket_id}（{tk['type']}/{tk['priority']}）")
            except ApiError as e:
                ctx.c.failed += 1
                ctx.c.note("fail", f"{tag} 建单失败：{str(e.body)[:160]}")
                continue

            # 指派
            try:
                if tk["assignee"] == "tenant" and ids["agent"]:
                    agent.write("POST", f"/api/v1/tickets/{ticket_id}/assign", {"assigneeId": ids["agent"]})
                elif tk["assignee"] == "admin" and ids["admin"]:
                    admin.write("POST", f"/api/v1/tickets/{ticket_id}/assign", {"assigneeId": ids["admin"]})
                elif tk["assignee"] == "msp":
                    pkey = provider_agent_by_tenant.get(spec.key, "msp_agent1")
                    prov = session_as(ctx, "provider", pkey)
                    prov.write("POST", f"/api/v1/msp/tickets/{ticket_id}/assign",
                               {"customerTenantId": tid})
                    ctx.c.note("msp-assign", f"{tag} 已指派 MSP（{pkey}），写入 managed_by/MSP 快照")
            except ApiError as e:
                ctx.c.note("warn", f"{tag} 指派失败：{str(e.body)[:140]}")

            # 状态流转：入口按处理人选择（处理人/创建人/管理员可流转；MSP 托管走服务商工作台）
            if tk["assignee"] == "msp":
                pkey = provider_agent_by_tenant.get(spec.key, "msp_agent1")
                prov = session_as(ctx, "provider", pkey)
                for st in tk.get("path", []):
                    try:
                        _transition_msp(ctx, prov, ticket_id, tid, st, tag)
                    except ApiError as e:
                        ctx.c.note("warn", f"{tag} MSP 流转 {st} 失败：{str(e.body)[:140]}")
            else:
                tcli = admin if tk["assignee"] == "admin" else agent
                for st in tk.get("path", []):
                    try:
                        _transition(ctx, tcli, ticket_id, st, tag)
                    except ApiError as e:
                        ctx.c.note("warn", f"{tag} 流转 {st} 失败：{str(e.body)[:140]}")

            # 评论（客户侧可见时间线）
            for text in tk.get("comments", []):
                try:
                    agent.write("POST", f"/api/v1/tickets/{ticket_id}/comments", {"content": text})
                except ApiError as e:
                    ctx.c.note("warn", f"{tag} 评论失败：{str(e.body)[:120]}")


def stage_cross(ctx: Ctx) -> None:
    """跨租户协同：服务商在工作台对 MSP 托管工单回复 + 推进状态（幂等按标记去重）。"""
    ctx.log("== 阶段 6/6：跨租户协同（工作台回复 / 状态推进） ==")
    resolve_tenant_ids(ctx)
    marker = "[服务商-演示]"
    provider_agent_by_tenant = {"mfg": "msp_agent1", "med": "msp_agent2", "edu": "msp_agent1"}

    for spec in TENANTS:
        if spec.type != "msp_customer":
            continue
        tid = ctx.cache.get(f"tenant:{spec.key}")
        if not tid:
            continue
        pkey = provider_agent_by_tenant.get(spec.key, "msp_agent1")
        try:
            prov = session_as(ctx, "provider", pkey)
            agent = session_as(ctx, spec.key, "agent")
        except Exception as e:  # noqa: BLE001
            ctx.c.failed += 1
            ctx.c.note("fail", f"{spec.code} 跨租户会话失败：{e}")
            continue
        try:
            wb = prov.get(f"/api/v1/msp/workbench/tickets?customerTenantIds={tid}&limit=50")
            items = extract_list(wb, ("items", "tickets", "list"))
        except ApiError as e:
            ctx.c.failed += 1
            ctx.c.note("fail", f"{spec.code} 工作台读取失败：{str(e.body)[:140]}")
            continue

        handled = 0
        msp_titles = {t["title"] for t in TICKET_SPECS.get(spec.key, [])
                      if t.get("assignee") == "msp"}
        for item in items:
            if not isinstance(item, dict):
                continue
            ticket_id = int(item.get("id") or 0)
            title = str(item.get("title") or "")
            # 仅处理 MSP 托管工单（assignee=msp 的规格单），避免误推进客户自处理单
            if not ticket_id or title not in msp_titles:
                continue
            if str(item.get("status")) in ("closed", "cancelled"):
                continue
            # 幂等：检查评论中是否已有服务商标记
            try:
                comments = extract_list(agent.get(f"/api/v1/tickets/{ticket_id}/comments"),
                                        ("comments", "items", "list"))
            except ApiError:
                comments = []
            if any(marker in str(c.get("content") or "") for c in comments if isinstance(c, dict)):
                ctx.c.existing += 1
                continue
            if ctx.dry_run:
                ctx.c.note("dry-run", f"将在工单 {ticket_id}（{spec.code}）上回复并推进状态")
                continue
            try:
                prov.write("POST", f"/api/v1/msp/tickets/{ticket_id}/reply",
                           {"customerTenantId": tid,
                            "content": f"{marker} 服务商已受理并远程协助处理，请留意后续进度。"})
                # 状态推进仅在当前状态 ≠ 目标状态时调用（MSP 状态端点拒绝无变化/越界转换）
                if str(item.get("status")) not in ("in_progress",):
                    try:
                        prov.write("POST", f"/api/v1/msp/tickets/{ticket_id}/status",
                                   {"customerTenantId": tid, "status": "in_progress"})
                    except ApiError as e:
                        ctx.c.note("warn", f"工单 {ticket_id} MSP 状态推进失败：{str(e.body)[:120]}")
                ctx.c.created += 1
                handled += 1
                ctx.c.note("created", f"跨租户协同：工单 {ticket_id}（{spec.code}）服务商回复 + in_progress")
            except ApiError as e:
                ctx.c.note("warn", f"工单 {ticket_id} 跨租户操作失败：{str(e.body)[:140]}")
        ctx.log(f"   - {spec.name}：工作台可见 {len(items)} 单，本轮处理 {handled} 单")


# =====================================================================
# 8. 阶段实现：可见性验证（各角色视角清点）
# =====================================================================

def stage_verify(ctx: Ctx) -> None:
    ctx.log("== 验证：各角色视角数据清点 ==")
    resolve_tenant_ids(ctx)
    rows: List[str] = []

    for spec in TENANTS:
        if spec.type != "msp_customer":
            continue
        tid = ctx.cache.get(f"tenant:{spec.key}")
        if not tid:
            continue
        agent = session_as(ctx, spec.key, "agent")
        user = session_as(ctx, spec.key, "user")
        admin = session_as(ctx, spec.key, "admin")
        try:
            tk = extract_list(agent.get("/api/v1/tickets?page=1&pageSize=100"), ("tickets", "items", "list"))
            by_status: Dict[str, int] = {}
            for t in tk:
                if isinstance(t, dict):
                    by_status[str(t.get("status"))] = by_status.get(str(t.get("status")), 0) + 1
        except ApiError:
            by_status = {}
        try:
            mine = extract_list(user.get("/api/v1/tickets?page=1&pageSize=100"), ("tickets", "items", "list"))
        except ApiError:
            mine = []
        try:
            kb = extract_list(admin.get("/api/v1/knowledge/articles?page=1&pageSize=100"),
                              ("articles", "items", "list"))
        except ApiError:
            kb = []
        try:
            cis = extract_list(admin.get("/api/v1/cmdb/cis?page=1&pageSize=100"), ("cis", "items", "list"))
        except ApiError:
            cis = []
        row = (f"| {spec.name} | {len(tk)} | {len(mine)} | "
               f"{', '.join(f'{k}×{v}' for k, v in sorted(by_status.items())) or '-'} | "
               f"{len(kb)} | {len(cis)} |")
        rows.append(row)
        ctx.c.note("verify", f"{spec.code}: tickets={len(tk)} my={len(mine)} by_status={by_status} kb={len(kb)} cis={len(cis)}")

    # 服务商视角
    try:
        # 用服务商经理（对三个客户均为 primary）统一核对工作台可见性
        prov = session_as(ctx, "provider", "msp_manager")
        all_ids = ",".join(str(ctx.cache.get(f"tenant:{k}")) for k in ("mfg", "med", "edu")
                           if ctx.cache.get(f"tenant:{k}"))
        wb = extract_list(prov.get(f"/api/v1/msp/workbench/tickets?customerTenantIds={all_ids}&limit=50"),
                          ("items", "tickets", "list"))
        alloc = extract_list(prov.get("/api/v1/msp/allocations?page=1&pageSize=100"), ("allocations", "items"))
        rows.append(f"| 服务商（工作台/分配） | {len(wb)} | - | - | - | {len(alloc)} 项分配 |")
        ctx.c.note("verify", f"provider: workbench={len(wb)} allocations={len(alloc)}")
    except Exception as e:  # noqa: BLE001
        ctx.c.note("fail", f"服务商视角验证失败：{e}")

    print("\n| 租户 | 工单(服务台视角) | 我的工单(end_user) | 状态分布 | 知识 | CI |")
    print("|---|---|---|---|---|---|")
    for r in rows:
        print(r)


# =====================================================================
# 9. 阶段注册与入口（必须位于文件末尾：数据集定义先于 main() 求值）
# =====================================================================

STAGES.update({
    "masterdata": stage_masterdata,
    "records": stage_records,
    "cross": stage_cross,
    "verify": stage_verify,
})

if __name__ == "__main__":
    raise SystemExit(main())
