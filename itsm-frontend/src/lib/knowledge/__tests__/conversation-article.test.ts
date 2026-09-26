/**
 * 「整段会话 → 知识文章」组装层单测（会话页标题栏「保存成文章」的核心逻辑）。
 *
 * 覆盖：① 助手消息 JSON 解析（含纯文本兜底）② 引用去重
 *      ③ 多轮问答配对与 Markdown 组装（不截断、标题口径、参考来源）
 *      ④ 取数壳在空会话下的失败语义。
 */
import { getConversationMessages, type AIMessage } from '@/lib/api/ai-api';
import {
  buildConversationArticle,
  dedupeSources,
  fetchConversationArticle,
  parseAssistantContent,
} from '../conversation-article';

jest.mock('@/lib/api/ai-api', () => ({
  getConversationMessages: jest.fn(),
}));

const mockGetConversationMessages = jest.mocked(getConversationMessages);

const assistantJson = (answer: string, sources: unknown[] = []) =>
  JSON.stringify({ answer, sources });

const message = (partial: Partial<AIMessage> & Pick<AIMessage, 'id' | 'role' | 'content'>): AIMessage => ({
  conversationId: 1,
  createdAt: '2026-01-01T00:00:00Z',
  ...partial,
});

describe('parseAssistantContent', () => {
  it('解析 {answer,sources} JSON 并保留引用', () => {
    const sources = [{ objectType: 'kb', id: 7, snippet: '片段', title: '文档' }];
    expect(parseAssistantContent(assistantJson('# 回答\n正文', sources))).toEqual({
      answer: '# 回答\n正文',
      sources,
    });
  });

  it('纯文本原样返回（历史数据 / 降级路径）', () => {
    expect(parseAssistantContent('抱歉，我没有找到相关的答案。')).toEqual({
      answer: '抱歉，我没有找到相关的答案。',
      sources: [],
    });
  });

  it('JSON 但没有 answer（或 answer 空白）时按纯文本处理', () => {
    expect(parseAssistantContent(JSON.stringify({ sources: [] })).answer).toBe(
      JSON.stringify({ sources: [] })
    );
    expect(parseAssistantContent(JSON.stringify({ answer: '   ' })).answer).toBe(
      JSON.stringify({ answer: '   ' })
    );
  });

  it('sources 非数组 / 混入空值时只保留对象项', () => {
    expect(parseAssistantContent(assistantJson('a', null as unknown as [])).sources).toEqual([]);
    expect(
      parseAssistantContent(assistantJson('a', [{ id: 1 }, null, 'x'] as unknown[])).sources
    ).toEqual([{ id: 1 }]);
  });
});

describe('dedupeSources', () => {
  it('按 objectType:id 去重并保持首次出现顺序', () => {
    const sources = [
      { objectType: 'kb', id: 1, snippet: 'a' },
      { objectType: 'kb', id: 2, snippet: 'b' },
      { objectType: 'kb', id: 1, snippet: 'a-again' },
    ];
    expect(dedupeSources(sources).map(s => s.snippet)).toEqual(['a', 'b']);
  });
});

describe('buildConversationArticle', () => {
  it('按 id 升序配对问答，标题取首个提问', () => {
    const article = buildConversationArticle([
      message({ id: 2, role: 'assistant', content: assistantJson('第二段回答') }),
      message({ id: 1, role: 'user', content: '如何排查 VPN 拨号失败？' }),
      message({ id: 3, role: 'user', content: '那证书呢？' }),
      message({ id: 4, role: 'assistant', content: assistantJson('检查证书链') }),
    ]);

    expect(article?.title).toBe('如何排查 VPN 拨号失败？');
    expect(article?.content).toContain('## 提问 1');
    expect(article?.content).toContain('如何排查 VPN 拨号失败？');
    expect(article?.content).toContain('## 回答 1');
    expect(article?.content).toContain('第二段回答');
    expect(article?.content).toContain('## 提问 2');
    expect(article?.content).toContain('检查证书链');
    expect(article?.content).toContain('共 2 轮问答');
  });

  it('引用来源汇总到文末并按 objectType:id 去重', () => {
    const article = buildConversationArticle([
      message({ id: 1, role: 'user', content: '问题' }),
      message({
        id: 2,
        role: 'assistant',
        content: assistantJson('回答一', [
          { objectType: 'kb', id: 9, title: 'VPN 排查指南', score: 0.87 },
          { objectType: 'kb', id: 9, title: 'VPN 排查指南', score: 0.87 },
        ]),
      }),
      message({
        id: 3,
        role: 'user',
        content: '追问',
      }),
      message({
        id: 4,
        role: 'assistant',
        content: assistantJson('回答二', [{ objectType: 'kb', id: 10, snippet: '另一篇来源' }]),
      }),
    ]);

    const content = article?.content ?? '';
    expect(content.match(/## 参考来源/g)).toHaveLength(1);
    expect(content.match(/VPN 排查指南/g)).toHaveLength(1);
    expect(content).toContain('相关度 87%');
    expect(content).toContain('另一篇来源');
  });

  it('不截断长回答（整段会话正文原样保留）', () => {
    const longAnswer = `# 长回答\n\n${'细节。'.repeat(5000)}`;
    const article = buildConversationArticle([
      message({ id: 1, role: 'user', content: '长问题' }),
      message({ id: 2, role: 'assistant', content: assistantJson(longAnswer) }),
    ]);

    expect(article?.content).toContain(longAnswer);
    expect((article?.content.length ?? 0) > longAnswer.length).toBe(true);
  });

  it('首条即助手消息（无提问）时标题退回回答首行，且不产生空的提问块', () => {
    const article = buildConversationArticle([
      message({ id: 1, role: 'assistant', content: assistantJson('# 结论\n\n直接给答案') }),
    ]);

    expect(article?.title).toBe('结论');
    expect(article?.content).not.toContain('## 提问');
    expect(article?.content).toContain('## 回答 1');
  });

  it('回答为空 / 无助手消息 / 空列表 → null（不产出空文章）', () => {
    expect(buildConversationArticle([])).toBeNull();
    expect(
      buildConversationArticle([message({ id: 1, role: 'user', content: '只有提问' })])
    ).toBeNull();
    expect(
      buildConversationArticle([message({ id: 1, role: 'assistant', content: '   ' })])
    ).toBeNull();
  });
});

describe('fetchConversationArticle', () => {
  beforeEach(() => {
    mockGetConversationMessages.mockReset();
  });

  it('经接口拉取会话并组装（会话正文不在路由 state 里传输）', async () => {
    mockGetConversationMessages.mockResolvedValue([
      message({ id: 1, role: 'user', content: '怎么扩容磁盘？' }),
      message({ id: 2, role: 'assistant', content: assistantJson('先查分区表') }),
    ]);

    await expect(fetchConversationArticle(42)).resolves.toEqual({
      title: '怎么扩容磁盘？',
      content: expect.stringContaining('先查分区表'),
    });
    expect(mockGetConversationMessages).toHaveBeenCalledWith(42);
  });

  it('会话没有可用回答 → 抛错，由调用方呈现错误态', async () => {
    mockGetConversationMessages.mockResolvedValue([]);
    await expect(fetchConversationArticle(43)).rejects.toThrow('该会话没有可用于生成文章的回答');
  });
});
