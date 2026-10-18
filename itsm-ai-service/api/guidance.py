"""
Guidance Sidecar 兼容端点

后端 GuidanceClient 期望独立 sidecar 提供受限 JSON 生成：
  POST /triage  ->  {category, priority, confidence, explanation, suggestedFix, assigneeId, method, latencyMs}
  GET  /health  ->  已在 main.py 提供

本模块让 itsm-ai-service 直接扮演该 sidecar：调用配置的 LLM provider
（OpenAI 兼容接口）生成受约束的分类 JSON，避免回代理后端造成鉴权环。
"""
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from typing import Optional
import json
import logging
import os
import time

import httpx

from config_loader import get_config

logger = logging.getLogger(__name__)

router = APIRouter()

# 与后端 triage_service.go 的 validCategories / validPriorities 保持一致
_VALID_CATEGORIES = [
    "database", "network", "server", "application",
    "security", "storage", "user_access", "general",
]
_VALID_PRIORITIES = ["critical", "high", "medium", "low"]


class GuidanceLLMTarget(BaseModel):
    """请求级 LLM 目标（F2）：由后端按应用内（DB）配置解析后随请求下发。

    出现该字段时 sidecar 不再读取自身 env/config.yaml——url 已按与后端网关相同的
    规则解析为最终请求地址，直接 POST；密钥只在本进程内使用，禁止落日志。
    """

    protocol: str = "openai_chat_completions"
    variant: Optional[str] = None
    url: str
    model: str
    apiKey: Optional[str] = None


class GuidanceTriageRequest(BaseModel):
    title: str
    description: str = ""
    tenantId: int = 1
    llm: Optional[GuidanceLLMTarget] = None


class GuidanceTriageResponse(BaseModel):
    category: str
    priority: str
    confidence: float
    explanation: str
    suggestedFix: Optional[str] = None
    assigneeId: int = 0
    method: str = "guidance"
    latencyMs: float = 0.0


_SYSTEM_PROMPT = (
    "You are an ITSM ticket triage classifier. "
    "Respond ONLY with a single JSON object, no markdown, no commentary. "
    "Required fields: "
    "category (one of: database, network, server, application, security, storage, user_access, general), "
    "priority (one of: critical, high, medium, low), "
    "confidence (float 0.0-1.0), "
    "explanation (short string), "
    "suggestedFix (short string or null)."
)


def _build_user_prompt(title: str, description: str) -> str:
    desc = description or "(no description)"
    return f"Classify this ITSM ticket.\nTitle: {title}\nDescription: {desc}"


def _coerce_category(value: str) -> str:
    if value in _VALID_CATEGORIES:
        return value
    lowered = (value or "").strip().lower()
    for c in _VALID_CATEGORIES:
        if c in lowered:
            return c
    return "general"


def _coerce_priority(value: str) -> str:
    if value in _VALID_PRIORITIES:
        return value
    lowered = (value or "").strip().lower()
    if "crit" in lowered:
        return "critical"
    if "high" in lowered or "urgent" in lowered:
        return "high"
    if "low" in lowered:
        return "low"
    return "medium"


def _keyword_classify(title: str, description: str) -> GuidanceTriageResponse:
    """基于关键词的降级分类，与后端 triage_service.go validCategories 对齐。"""
    text = f"{title} {description}".lower()

    category = "general"
    if any(k in text for k in ["mysql", "postgres", "mongodb", "redis", "oracle", "数据库", "查询", "慢查询"]):
        category = "database"
    elif any(k in text for k in ["wifi", "router", "switch", "firewall", "vpn", "网络", "连接", "网卡", "dns", "ping"]):
        category = "network"
    elif any(k in text for k in ["cpu", "memory", "disk", "linux", "windows server", "服务器", "宕机", "重启", "硬件"]):
        category = "server"
    elif any(k in text for k in ["software", "app", "api", "deployment", "软件", "应用", "程序", "部署"]):
        category = "application"
    elif any(k in text for k in ["vulnerability", "attack", "permission", "auth", "安全", "漏洞", "攻击", "权限"]):
        category = "security"
    elif any(k in text for k in ["backup", "snapshot", "storage", "磁盘", "存储", "空间"]):
        category = "storage"
    elif any(k in text for k in ["account", "login", "password", "access", "账号", "密码", "登录"]):
        category = "user_access"

    priority = "medium"
    if any(k in text for k in ["critical", "urgent", "emergency", "紧急", "严重", "宕机", "无法工作", "服务不可用"]):
        priority = "critical"
    elif any(k in text for k in ["high", "important", "重要", "影响工作", "故障"]):
        priority = "high"
    elif any(k in text for k in ["low", "minor", "不紧急", "轻微"]):
        priority = "low"

    return GuidanceTriageResponse(
        category=category,
        priority=priority,
        confidence=0.7,
        explanation="keyword-based classification (LLM unavailable)",
        method="guidance-fallback",
        latencyMs=0.0,
    )


def _post_openai_compatible(
    title: str, description: str, url: str, model: str, api_key: str, timeout: float
) -> dict:
    """OpenAI Chat Completions 形态调用（env 配置路径与请求级 target 共用）。

    url 为最终请求地址：env 路径由 base_url 拼接；请求级 target 由后端按与网关
    相同的规则解析后下发。
    """
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": _SYSTEM_PROMPT},
            {"role": "user", "content": _build_user_prompt(title, description)},
        ],
        "temperature": 0.2,
        "response_format": {"type": "json_object"},
    }
    headers = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"

    with httpx.Client(timeout=httpx.Timeout(timeout or 120)) as client:
        resp = client.post(url, json=payload, headers=headers)
        resp.raise_for_status()
        data = resp.json()

    content = data["choices"][0]["message"]["content"]
    parsed = json.loads(content)
    if not isinstance(parsed, dict):
        raise ValueError("LLM did not return a JSON object")
    return parsed


def _post_anthropic_compatible(
    title: str, description: str, url: str, model: str, api_key: str, timeout: float
) -> dict:
    """Anthropic Messages 形态调用（官方 / MiniMax 兼容端点共用；F2 起 url 由调用方给出）。"""
    payload = {
        "model": model,
        "max_tokens": 512,
        "system": _SYSTEM_PROMPT,
        "temperature": 0.2,
        "messages": [{"role": "user", "content": _build_user_prompt(title, description)}],
    }
    headers = {
        "Content-Type": "application/json",
        "anthropic-version": "2023-06-01",
    }
    if api_key:
        headers["x-api-key"] = api_key

    with httpx.Client(timeout=httpx.Timeout(timeout or 120)) as client:
        resp = client.post(url, json=payload, headers=headers)
        resp.raise_for_status()
        data = resp.json()

    text = next(
        (block.get("text", "") for block in data.get("content", []) if block.get("type") == "text"),
        "",
    )
    parsed = json.loads(text)
    if not isinstance(parsed, dict):
        raise ValueError("Anthropic-compatible provider did not return a JSON object")
    return parsed


def _call_llm(title: str, description: str) -> dict:
    """Call the provider configured in env/config.yaml and return a constrained triage object."""
    cfg = get_config()
    api_key = os.getenv("LLM_API_KEY") or cfg.llm.api_key
    if not api_key:
        raise RuntimeError("LLM_API_KEY not configured")

    timeout = cfg.llm.timeout or 120
    if cfg.llm.provider == "minimax":
        # 保留原 MiniMax 兼容口径：base_url 默认 .../anthropic/v1，路径 /messages。
        base_url = (cfg.llm.base_url or "https://api.minimaxi.com/anthropic/v1").rstrip("/")
        return _post_anthropic_compatible(
            title, description, f"{base_url}/messages", cfg.llm.model, api_key, timeout
        )

    base_url = (cfg.llm.base_url or "https://api.openai.com/v1").rstrip("/")
    model = cfg.llm.model or "gpt-4o-mini"
    return _post_openai_compatible(
        title, description, f"{base_url}/chat/completions", model, api_key, timeout
    )


def _call_llm_override(title: str, description: str, target: GuidanceLLMTarget) -> dict:
    """请求级 LLM 目标（F2）：URL/模型/密钥全部来自应用内配置，不再读自身 env/config.yaml。

    后端（TriageService）已按与网关相同的规则解析 url，sidecar 只负责按协议 POST：
    anthropic_messages → Anthropic Messages 形态；其余（openai_chat_completions）→ OpenAI 形态。
    """
    cfg = get_config()
    timeout = cfg.llm.timeout or 120
    protocol = (target.protocol or "").strip().lower()
    if protocol == "anthropic_messages":
        return _post_anthropic_compatible(
            title, description, target.url, target.model, target.apiKey or "", timeout
        )
    return _post_openai_compatible(
        title, description, target.url, target.model, target.apiKey or "", timeout
    )


@router.post("/triage", response_model=GuidanceTriageResponse)
async def guidance_triage(req: GuidanceTriageRequest):
    """Guidance sidecar 兼容的受限分诊入口。"""
    start = time.perf_counter()
    try:
        if req.llm is not None:
            # F2：应用内（DB）配置下发——URL/模型/密钥来自后端解析，不再读自身配置。
            # 仅记录非敏感字段；apiKey 禁止进日志。
            logger.info(
                "Guidance triage using request-scoped LLM target "
                f"tenant={req.tenantId} protocol={req.llm.protocol} variant={req.llm.variant} "
                f"url={req.llm.url} model={req.llm.model}"
            )
            raw = _call_llm_override(req.title, req.description, req.llm)
        else:
            raw = _call_llm(req.title, req.description)
    except Exception as e:  # noqa: BLE001 - 任何失败都降级为关键词分类
        logger.warning(f"Guidance triage LLM unavailable, using keyword fallback: {e}")
        latency = (time.perf_counter() - start) * 1000
        fallback = _keyword_classify(req.title, req.description)
        fallback.latencyMs = round(latency, 2)
        return fallback

    latency = (time.perf_counter() - start) * 1000
    category = _coerce_category(str(raw.get("category", "general")))
    priority = _coerce_priority(str(raw.get("priority", "medium")))
    try:
        confidence = float(raw.get("confidence", 0.5))
    except (TypeError, ValueError):
        confidence = 0.5
    confidence = max(0.0, min(1.0, confidence))

    suggested = raw.get("suggestedFix")
    if suggested is not None:
        suggested = str(suggested)

    return GuidanceTriageResponse(
        category=category,
        priority=priority,
        confidence=round(confidence, 3),
        explanation=str(raw.get("explanation", ""))[:500],
        suggestedFix=suggested,
        assigneeId=0,
        method="guidance",
        latencyMs=round(latency, 2),
    )
