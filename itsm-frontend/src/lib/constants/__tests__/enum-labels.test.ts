/**
 * 枚举中文字典覆盖测试（根因 B）。
 *
 * 核心目的：锁定每个枚举取值都有中文标签，防止“键不匹配 → 静默直出原始 code”
 * 这一回归（本类问题在页面审查中被反复标记为“第 13 次复现”）。
 */
import { ServiceRequestStatus, ApprovalStep } from '@/constants/service-request';
import {
  enumLabel,
  SERVICE_REQUEST_STATUS_LABEL,
  APPROVAL_STEP_LABEL,
  CI_TYPE_LABEL,
} from '@/lib/constants';

describe('enumLabel 通用行为', () => {
  it('命中词表返回中文', () => {
    expect(enumLabel(SERVICE_REQUEST_STATUS_LABEL, 'submitted')).toBe('已提交');
  });

  it('未命中返回原始 code（不产生 undefined / 不抛错）', () => {
    expect(enumLabel(SERVICE_REQUEST_STATUS_LABEL, 'mystery_status')).toBe('mystery_status');
  });

  it('空值返回 fallback', () => {
    expect(enumLabel(SERVICE_REQUEST_STATUS_LABEL, '', '未知')).toBe('未知');
    expect(enumLabel(SERVICE_REQUEST_STATUS_LABEL, null)).toBe('');
  });
});

describe('服务请求枚举本地化覆盖', () => {
  it('每个 ServiceRequestStatus 取值都有中文标签', () => {
    const values = Object.values(ServiceRequestStatus) as string[];
    expect(values.length).toBeGreaterThan(0);
    for (const v of values) {
      expect(enumLabel(SERVICE_REQUEST_STATUS_LABEL, v)).not.toBe(v);
    }
  });

  it('每个 ApprovalStep 取值都有中文标签', () => {
    const values = Object.values(ApprovalStep) as string[];
    expect(values.length).toBeGreaterThan(0);
    for (const v of values) {
      expect(enumLabel(APPROVAL_STEP_LABEL, v)).not.toBe(v);
    }
  });
});

describe('CI 类型枚举本地化', () => {
  it('审查中反复直出的 code 已映射为中文', () => {
    for (const code of ['application', 'database', 'network', 'server', 'storage']) {
      expect(enumLabel(CI_TYPE_LABEL, code)).not.toBe(code);
    }
    expect(enumLabel(CI_TYPE_LABEL, 'application')).toBe('应用系统');
  });
});
