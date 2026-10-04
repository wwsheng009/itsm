/**
 * 租户管理页：MSP 客户「服务商必填 + 载荷透传」规则测试。
 *
 * 背景：全页 UI 用例（打开弹窗 → antd v6 Select 选择 msp_customer → 提交）在本仓
 * jsdom/worktree 环境下无法收敛（dropdown 交互触发事件循环饥饿，实测 >10min CPU 满载
 * 且 jest 超时都不触发），因此改为确定性验证页面**同一套**规则实现：
 * - `isMspProviderRequired`：仅 msp_customer 展示/必填（页面显隐与清空逻辑复用）；
 * - `mspProviderRules`：必填校验规则（直接挂到页面 Form.Item，并在此用真实 antd Form 验证拦截）；
 * - `buildTenantPayload`：提交载荷仅在 msp_customer 时携带 mspProviderId（quota 收敛一并回归）。
 */
import React from 'react';
import { Button, Form, Input } from 'antd';
import { fireEvent, render, screen } from '@/lib/test-utils';
import {
  buildTenantPayload,
  isMspProviderRequired,
  mspProviderRules,
} from '..';
import type { TenantFormValues } from '..';

const baseValues: TenantFormValues = {
  name: 'Acme Child',
  code: 'acme_child',
  type: 'msp_customer',
  status: 'active',
  mspProviderId: 3,
};

function MinimalProviderForm({ onFinish }: { onFinish: (values: unknown) => void }) {
  return (
    <Form onFinish={onFinish}>
      <Form.Item label="MSP 服务商" name="mspProviderId" rules={mspProviderRules}>
        <Input />
      </Form.Item>
      <Button htmlType="submit">提交</Button>
    </Form>
  );
}

describe('TenantManagement · MSP 客户服务商规则', () => {
  it('isMspProviderRequired 仅对 msp_customer 为真', () => {
    expect(isMspProviderRequired('msp_customer')).toBe(true);
    expect(isMspProviderRequired('internal')).toBe(false);
    expect(isMspProviderRequired('saas_customer')).toBe(false);
    expect(isMspProviderRequired('msp_provider')).toBe(false);
    expect(isMspProviderRequired(undefined)).toBe(false);
  });

  it('mspProviderRules 为必填且提示与页面一致', () => {
    expect(mspProviderRules).toEqual([{ required: true, message: '请选择 MSP 服务商' }]);
  });

  it('msp_customer：载荷携带 mspProviderId', () => {
    const payload = buildTenantPayload(baseValues);
    expect(payload).toMatchObject({
      name: 'Acme Child',
      type: 'msp_customer',
      status: 'active',
      mspProviderId: 3,
    });
  });

  it('非 msp_customer：即使表单残留 mspProviderId 也不提交该字段', () => {
    const payload = buildTenantPayload({ ...baseValues, type: 'internal' });
    expect('mspProviderId' in payload).toBe(false);
    expect(payload.type).toBe('internal');
  });

  it('msp_customer 未选服务商（undefined）时不得伪造字段', () => {
    const payload = buildTenantPayload({ ...baseValues, mspProviderId: undefined });
    expect('mspProviderId' in payload).toBe(false);
  });

  it('quota 收敛：仅 >0 的键；全空为 {}（清空 → 不限）', () => {
    expect(
      buildTenantPayload({ ...baseValues, maxUsers: 5, maxTicketsPerMonth: 0 }).quota
    ).toEqual({ maxUsers: 5 });
    expect(buildTenantPayload({ ...baseValues }).quota).toEqual({});
  });

  it('必填规则接入 antd Form：空值被拦截，不触发提交', async () => {
    const onFinish = jest.fn();
    render(<MinimalProviderForm onFinish={onFinish} />);

    // antd Button 会在两个中文字符间插入空格，accessible name 为「提 交」。
    fireEvent.click(screen.getByRole('button', { name: /提\s*交/ }));

    expect(await screen.findByText('请选择 MSP 服务商')).toBeInTheDocument();
    expect(onFinish).not.toHaveBeenCalled();
  });
});
