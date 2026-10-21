import { describeRiskConvertError } from '../risk-convert-error';

describe('describeRiskConvertError', () => {
  it('409 带 existingId → 明确回显既有单据，提示勿重复转换', () => {
    const message = describeRiskConvertError(
      { httpStatus: 409, code: 4090, data: { target: 'change', existingId: 11 } },
      'change'
    );

    expect(message).toBe('该风险/问题已转为变更单 #11，请直接查看，勿重复转换');
  });

  it('409 无 existingId → 仍按重复转换提示，不误报服务未配置', () => {
    const message = describeRiskConvertError(
      { httpStatus: 409, code: 4090, requestId: 'req-9' },
      'problem'
    );

    expect(message).toBe('该风险/问题已转换，请刷新后查看既有问题单（请求号 req-9）');
  });

  it('409 risk is closed → 提示先重开，不误报重复转换', () => {
    const message = describeRiskConvertError(
      {
        httpStatus: 409,
        code: 4090,
        data: { hint: 'reopen_before_conversion' },
      },
      'problem'
    );

    expect(message).toBe('转换失败：该风险/问题已关闭，请先重开后再转换');
  });

  it('400 → 透出后端参数校验文案', () => {
    const message = describeRiskConvertError(
      { httpStatus: 400, code: 1001, message: 'invalid target "ticket"' },
      'problem'
    );

    expect(message).toBe('转换失败：invalid target "ticket"');
  });

  it('400 project is closed → 明确告知项目终态，而非「服务未配置」', () => {
    const message = describeRiskConvertError(
      { httpStatus: 400, code: 1001, message: 'invalid risk status transition: project is closed' },
      'problem'
    );

    expect(message).toBe('转换失败：项目已结项或已取消，不能继续转换');
  });

  it('503 → 仅此时提示服务不可用/未配置', () => {
    const message = describeRiskConvertError({ httpStatus: 503, code: 5003 }, 'change');

    expect(message).toContain('变更单服务暂时不可用（或未配置）');
  });

  it('500 → 透出内部错误文案与请求号', () => {
    const message = describeRiskConvertError(
      { httpStatus: 500, code: 5001, message: '转换失败：boom', requestId: 'req-500' },
      'problem'
    );

    expect(message).toBe('转换失败：转换失败：boom（请求号 req-500）');
  });

  it('403 → 提示权限不足', () => {
    const message = describeRiskConvertError({ httpStatus: 403, code: 2003 }, 'problem');

    expect(message).toBe('转换失败：缺少项目写权限');
  });

  it('未知错误 → 回退后端 message 或通用建议', () => {
    expect(describeRiskConvertError(new Error('network down'), 'problem')).toBe(
      '转换失败：network down'
    );
    expect(describeRiskConvertError(undefined, 'problem')).toBe('转换失败：请稍后重试或联系管理员');
  });
});
