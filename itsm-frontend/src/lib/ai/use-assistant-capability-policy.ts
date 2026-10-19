/**
 * T5-05：AI 助手租户灰度策略（Dock / 帮助智能体）。
 *
 * - 数据源：`GET /api/v1/capabilities` 的 `assistant` 段（所有登录用户可读）；
 * - fail-safe：字段缺省、请求失败、能力面未装配 → 一律按产品默认开启，绝不阻断主链路；
 * - 免重启：管理台保存能力开关后，前端下一次挂载/读取即生效（多实例后端 ≤30s 收敛）。
 */
import { useEffect, useState } from 'react';

import { getCapabilities, type CapabilityResponse } from '@/lib/api/capability-api';

export interface AssistantCapabilityPolicy {
  dockEnabled: boolean;
  helpEnabled: boolean;
  memoryEnabled: boolean;
}

export const DEFAULT_ASSISTANT_CAPABILITY_POLICY: AssistantCapabilityPolicy = {
  dockEnabled: true,
  helpEnabled: true,
  memoryEnabled: true,
};

/** 把公共能力面响应归一化为 fail-safe 的助手策略（缺省/非法值 = 开启）。 */
export const parseAssistantCapabilityPolicy = (
  caps?: CapabilityResponse | null
): AssistantCapabilityPolicy => ({
  dockEnabled:
    typeof caps?.assistant?.dockEnabled === 'boolean'
      ? caps.assistant.dockEnabled
      : DEFAULT_ASSISTANT_CAPABILITY_POLICY.dockEnabled,
  helpEnabled:
    typeof caps?.assistant?.helpEnabled === 'boolean'
      ? caps.assistant.helpEnabled
      : DEFAULT_ASSISTANT_CAPABILITY_POLICY.helpEnabled,
  memoryEnabled:
    typeof caps?.assistant?.memoryEnabled === 'boolean'
      ? caps.assistant.memoryEnabled
      : DEFAULT_ASSISTANT_CAPABILITY_POLICY.memoryEnabled,
});

/**
 * 读取助手灰度策略。
 *
 * `enabled=false`（如无 ai:read 或工作区路由）时不发请求，直接返回产品默认。
 */
export function useAssistantCapabilityPolicy(enabled = true): AssistantCapabilityPolicy {
  const [policy, setPolicy] = useState<AssistantCapabilityPolicy>(
    DEFAULT_ASSISTANT_CAPABILITY_POLICY
  );

  useEffect(() => {
    if (!enabled) return undefined;
    let alive = true;
    getCapabilities()
      .then(caps => {
        if (alive) setPolicy(parseAssistantCapabilityPolicy(caps));
      })
      .catch(() => {
        // 能力读取失败按默认开启；不打扰用户，也不阻断助手主链路。
      });
    return () => {
      alive = false;
    };
  }, [enabled]);

  return policy;
}
