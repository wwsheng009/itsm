/// <reference types="vite/client" />

/**
 * 前端构建期环境变量（仅 `VITE_` 前缀会暴露给客户端代码）。
 * 对应迁移前的 `NEXT_PUBLIC_*`，见 docs/plan/vite-migration-plan.md §5.7。
 *
 * 说明：这些键声明为可变（非 readonly），以便测试中可以临时覆盖/删除
 * （例如 lib/services/__tests__/notification-ws.test.ts 覆写 VITE_WS_URL）。
 */
interface ImportMetaEnv {
  VITE_API_URL?: string;
  VITE_API_BASE_URL?: string;
  VITE_API_VERSION?: string;
  VITE_API_TIMEOUT?: string;
  VITE_API_RETRY_COUNT?: string;
  VITE_APP_NAME?: string;
  VITE_APP_VERSION?: string;
  VITE_BUILD_TIME?: string;
  VITE_ENABLE_AI?: string;
  VITE_ENABLE_ANALYTICS?: string;
  VITE_ENABLE_DEBUG?: string;
  VITE_ENABLE_MOCK?: string;
  VITE_ENABLE_PERFORMANCE_MONITORING?: string;
  VITE_RICH_TEXT?: string;
  VITE_RICH_TEXT_IMAGE_HOSTS?: string;
  VITE_SENTRY_DSN?: string;
  VITE_WS_URL?: string;
  VITE_FEISHU_APP_ID?: string;
  VITE_WECOM_APP_ID?: string;
  VITE_WECOM_AGENT_ID?: string;
  VITE_DINGTALK_APP_ID?: string;
  readonly MODE: string;
  readonly DEV: boolean;
  readonly PROD: boolean;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

declare const __APP_VERSION__: string;
declare const __BUILD_TIME__: string;
