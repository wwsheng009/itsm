/**
 * 单测：创建工单错误文案映射（IP-P2-6 配额 422 的 UI 呈现）。
 */
import { mapTicketCreateError } from '../ticket-create-error';

describe('mapTicketCreateError', () => {
  it('HTTP 422（租户硬配额）→ 用户可读中文提示，不透出英文技术文案', () => {
    const msg = mapTicketCreateError({
      httpStatus: 422,
      code: 5003, // 业务码（envelope.code），与 HTTP 状态不同值
      message: 'tenant quota exceeded: maxTicketsPerMonth (limit=1, used=23)',
    });
    expect(msg).toContain('已达租户配额上限');
    expect(msg).not.toContain('tenant quota exceeded');
  });

  it('业务码 422 但 HTTP 状态非 422（防御：不得误判）', () => {
    expect(mapTicketCreateError({ code: 422, message: '参数错误' })).toBe('参数错误');
  });

  it('其他业务错误 → 透传后端 message', () => {
    expect(mapTicketCreateError({ httpStatus: 400, message: '标题不能为空' })).toBe('标题不能为空');
  });

  it('未知错误 → 兜底文案', () => {
    expect(mapTicketCreateError(undefined)).toBe('创建工单失败，请检查输入或重新登录');
    expect(mapTicketCreateError({ error: { message: '嵌套消息' } })).toBe('嵌套消息');
  });
});
