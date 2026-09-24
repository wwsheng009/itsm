import { httpClient } from './http-client';

/**
 * 密码强度策略：与后端 dto.PasswordPolicyResponse 字段一一对应。
 * 数据源：GET /api/v1/auth/password-policy（公开端点，按 tenantCode 解析租户策略）。
 */
export interface PasswordPolicy {
  minLength: number;
  maxLength: number;
  requireUppercase: boolean;
  requireLowercase: boolean;
  requireNumbers: boolean;
  requireSpecialChars: boolean;
  /** 后端生成的规则文案（service.PasswordPolicy.Description()） */
  description?: string;
}

/**
 * 与后端 service.DefaultPasswordPolicy() 对齐的安全兜底：
 * 接口不可用时用同一套默认值，既不放松约束，也不至于让表单无法渲染。
 */
export const DEFAULT_PASSWORD_POLICY: PasswordPolicy = {
  minLength: 8,
  maxLength: 128,
  requireUppercase: true,
  requireLowercase: true,
  requireNumbers: true,
  requireSpecialChars: false,
};

// 进程内缓存：登录/注册/用户管理/个人中心会在同一会话内反复挂载，
// 而密码策略是低频变更的租户级配置。缓存 Promise 还能让并发挂载只发一次请求。
let pending: Promise<PasswordPolicy> | null = null;

/** 清空缓存：系统配置页保存密码策略后调用，确保下一次读取拿到新值。 */
export function clearPasswordPolicyCache(): void {
  pending = null;
}

/** 生成中文提示文案（后端未返回 description 时的兜底）。 */
export function describePasswordPolicy(policy: PasswordPolicy): string {
  const parts: string[] = [];
  if (policy.minLength > 0) {
    parts.push(
      policy.maxLength > 0
        ? `长度 ${policy.minLength}-${policy.maxLength} 位`
        : `长度不少于 ${policy.minLength} 位`,
    );
  }
  const requires: string[] = [];
  if (policy.requireUppercase) requires.push('大写字母');
  if (policy.requireLowercase) requires.push('小写字母');
  if (policy.requireNumbers) requires.push('数字');
  if (policy.requireSpecialChars) requires.push('特殊字符');
  if (requires.length > 0) {
    parts.push(`需包含${requires.join('、')}`);
  }
  return parts.join('，');
}

/** 读取当前租户生效的密码策略；失败时回退默认策略，并允许后续请求重试。 */
export function getPasswordPolicy(): Promise<PasswordPolicy> {
  if (!pending) {
    pending = httpClient
      .get<PasswordPolicy>('/api/v1/auth/password-policy')
      .then(policy => ({ ...DEFAULT_PASSWORD_POLICY, ...policy }))
      .catch(() => {
        // 失败不入缓存，避免一次网络抖动把错误策略固化一整个会话
        pending = null;
        return DEFAULT_PASSWORD_POLICY;
      });
  }
  return pending;
}
