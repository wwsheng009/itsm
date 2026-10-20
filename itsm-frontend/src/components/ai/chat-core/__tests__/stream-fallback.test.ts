import { buildBudgetExceededFallback } from '../stream-fallback';

describe('buildBudgetExceededFallback', () => {
  it('保留已完成检索摘要并给出收敛建议', () => {
    const text = buildBudgetExceededFallback({
      content: '',
      toolEvents: [
        {
          tool: 'help_search',
          provider: 'builtin',
          phase: 'read',
          status: 'done',
          summary: '命中《VPN 开通指引》《邮箱账号申请》',
        },
        {
          tool: 'list_cis',
          provider: 'builtin',
          phase: 'read',
          status: 'done',
          summary: '未找到匹配资产',
        },
        {
          tool: 'help_search',
          provider: 'builtin',
          phase: 'read',
          status: 'done',
          summary: '命中《VPN 开通指引》《邮箱账号申请》',
        },
      ],
    });

    expect(text).toContain('未能生成完整回答');
    expect(text).toContain('已查到的线索');
    expect(text).toContain('命中《VPN 开通指引》');
    expect(text).toContain('未找到匹配资产');
    expect(text).toContain('把问题拆成两个更具体的请求');
    expect(text).toContain('第二张');
    expect(text.match(/命中《VPN 开通指引》/g)).toHaveLength(1);
  });

  it('已有流式正文时说明内容已保留', () => {
    const text = buildBudgetExceededFallback({
      content: '先确认工单类型，再提交。',
      toolEvents: [],
    });
    expect(text).toContain('以上已生成内容已保留');
    expect(text).toContain('本轮没有可复用的检索结果');
  });
});
