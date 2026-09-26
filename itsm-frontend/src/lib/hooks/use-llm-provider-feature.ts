import { useCallback, useEffect, useRef, useState } from 'react';

import {
  LLMProviderApi,
  type LLMProviderAvailable,
  type LLMUserPreferenceResponse,
} from '@/lib/api/llm-provider-api';

/**
 * 多 LLM Provider 可用性探测（FE-2/FE-3/FE-4 共用）。
 *
 * 设计约束（§3.6 / D9 + P1 权限放开）：
 *  - 前端没有独立的 feature-flag 接口；灰度开关由 `available` 端点**可达性**间接体现
 *    （开关关闭 → 整组路由未注册 → 404 且无 errorCode，见
 *    `service/llm_multi_provider_flag.go` 注释④）。
 *  - P1 起 `GET /ai/providers/available` 与 `GET /ai/user-preference` 由 system:write 降为
 *    ai:read：**所有已登录用户**都发起探测（不再有前端权限门控），持 ai:read 的用户据此
 *    渲染 Provider 切换器；无 ai:read 的用户拿到 403。
 *  - 探测失败（404/403/503/网络错误）一律 fail-closed：`enabled=false`，
 *    调用方据此不渲染页签与选择器；非法结果不抛异常。
 *    注意：写端点 `PUT /ai/user-preference` **仍是 system:write**，调用方需自行门控写按钮。
 */
export interface LLMProviderFeatureState {
  /** 探测完成前为 false；调用方在 ready 之前应保持与现状一致的 UI。 */
  ready: boolean;
  /** 开关开启（路由已注册）且探测成功。 */
  enabled: boolean;
  /** 可用实例（选择器数据源，无密钥）。 */
  providers: LLMProviderAvailable[];
  /** 当前用户偏好（含生效值）；探测失败为 null。 */
  preference: LLMUserPreferenceResponse | null;
  /** 重新探测（测试连通/修改配置/切换失败后调用）。 */
  refresh: () => Promise<void>;
  /** 设置/清除我的默认（null = 清除），成功后自动刷新状态。 */
  setPreference: (providerKey: string | null) => Promise<void>;
}

interface FeatureSnapshot {
  enabled: boolean;
  providers: LLMProviderAvailable[];
  preference: LLMUserPreferenceResponse | null;
}

const EMPTY_SNAPSHOT: FeatureSnapshot = { enabled: false, providers: [], preference: null };

export const useLLMProviderFeature = (): LLMProviderFeatureState => {
  const [ready, setReady] = useState(false);
  const [snapshot, setSnapshot] = useState<FeatureSnapshot>(EMPTY_SNAPSHOT);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    try {
      const [providers, preference] = await Promise.all([
        LLMProviderApi.listAvailable(),
        LLMProviderApi.getUserPreference().catch(() => null),
      ]);
      if (mountedRef.current) {
        setSnapshot({ enabled: true, providers, preference });
      }
    } catch {
      // 404 = 开关关闭；403 = 无 ai:read（或未登录）；503/网络错误 = 不可用。
      // 统一 fail-closed，不打断页面。
      if (mountedRef.current) {
        setSnapshot(EMPTY_SNAPSHOT);
      }
    } finally {
      if (mountedRef.current) {
        setReady(true);
      }
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const refresh = useCallback(async () => {
    await load();
  }, [load]);

  const setPreference = useCallback(
    async (providerKey: string | null) => {
      await LLMProviderApi.setUserPreference(providerKey);
      await load();
    },
    [load]
  );

  return {
    ready,
    enabled: snapshot.enabled,
    providers: snapshot.providers,
    preference: snapshot.preference,
    refresh,
    setPreference,
  };
};
