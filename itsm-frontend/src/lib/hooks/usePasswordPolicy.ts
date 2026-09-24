
import { useEffect, useMemo, useState } from 'react';
import type { FormItemProps } from 'antd';
import {
  DEFAULT_PASSWORD_POLICY,
  describePasswordPolicy,
  getPasswordPolicy,
  type PasswordPolicy,
} from '@/lib/api/password-policy-api';

type PasswordRules = NonNullable<FormItemProps['rules']>;

export interface UsePasswordPolicyResult {
  /** 当前生效的密码策略（首帧为默认策略，拿到接口结果后自动更新） */
  policy: PasswordPolicy;
  /** 直接喂给 Form.Item 的 extra，展示当前规则 */
  hint: string;
  /** 生成与后端 service.PasswordPolicy.Validate 对齐的 antd 校验规则 */
  buildRules: (requiredMessage: string) => PasswordRules;
}

/**
 * 密码策略 Hook。
 *
 * 规则来源是「系统配置 - 密码策略」（GET /api/v1/auth/password-policy），
 * 与后端 service.PasswordPolicy.Validate 保持同源，避免前后端规则不一致导致提交后 400。
 * 多次挂载共享同一份进程内缓存（见 password-policy-api.ts），不会重复请求。
 */
export function usePasswordPolicy(): UsePasswordPolicyResult {
  const [policy, setPolicy] = useState<PasswordPolicy>(DEFAULT_PASSWORD_POLICY);

  useEffect(() => {
    let alive = true;
    void getPasswordPolicy()
      .then(next => {
        if (alive) setPolicy(next);
      })
      .catch(() => {
        /* getPasswordPolicy 内部已兜底，这里不会触发 */
      });
    return () => {
      alive = false;
    };
  }, []);

  const hint = useMemo(
    () => (policy.description && policy.description.trim() ? policy.description : describePasswordPolicy(policy)),
    [policy],
  );

  const buildRules = useMemo(
    () =>
      (requiredMessage: string): PasswordRules => {
        const rules: PasswordRules = [{ required: true, message: requiredMessage }];
        if (policy.minLength > 0) {
          rules.push({ min: policy.minLength, message: `密码长度不少于 ${policy.minLength} 位` });
        }
        if (policy.maxLength > 0) {
          rules.push({ max: policy.maxLength, message: `密码长度不超过 ${policy.maxLength} 位` });
        }
        if (policy.requireUppercase) {
          rules.push({ pattern: /[A-Z]/, message: '需包含至少一个大写字母' });
        }
        if (policy.requireLowercase) {
          rules.push({ pattern: /[a-z]/, message: '需包含至少一个小写字母' });
        }
        if (policy.requireNumbers) {
          rules.push({ pattern: /[0-9]/, message: '需包含至少一个数字' });
        }
        if (policy.requireSpecialChars) {
          rules.push({ pattern: /[^A-Za-z0-9]/, message: '需包含至少一个特殊字符' });
        }
        return rules;
      },
    [policy],
  );

  return { policy, hint, buildRules };
}
