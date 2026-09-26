/**
 * 「AI 助手 → 补充为知识文章」链路回归。
 *
 * 缺陷背景：入口此前只做一次不带数据的裸跳转（且目标是会丢 `location.state` 的历史别名），
 * 回答内容从未被采集，用户拿到的始终是空表单。本文件锁定修复后的两端：
 * - 读侧（真实渲染新建页）：从路由 state 取回预填写入表单；有预填时固定 Markdown 正文；
 * - 写侧（源码契约）：会话页必须携带 state 跳 `/knowledge/articles/new`，不得回退历史别名。
 */
import fs from 'node:fs';
import path from 'node:path';
import { render, screen, waitFor } from '@testing-library/react';

import {
  buildArticlePrefillState,
  buildConversationArticlePrefillState,
} from '@/lib/knowledge/ai-article-prefill';
import { fetchConversationArticle } from '@/lib/knowledge/conversation-article';
import NewKnowledgeArticlePage from '@/pages/(main)/knowledge/articles/new';

const mockNavigate = jest.fn();
const mockLocation: { state: unknown } = { state: undefined };

jest.mock('react-router', () => ({
  useLocation: () => ({ state: mockLocation.state }),
  useNavigate: () => mockNavigate,
}));

jest.mock('@/lib/api/knowledge-base-api', () => ({
  KnowledgeBaseApi: {
    getCategories: jest.fn(() => Promise.resolve([{ id: 1, name: '故障处理' }])),
    createArticle: jest.fn(),
    updateArticle: jest.fn(),
  },
}));

// 会话态预填经接口拉取正文：单测里替身掉取数，只验证页面接线与落地时机。
jest.mock('@/lib/knowledge/conversation-article', () => ({
  fetchConversationArticle: jest.fn(),
}));

const mockFetchConversationArticle = jest.mocked(fetchConversationArticle);

const MARKDOWN = '# VPN 拨号失败排查\n\n1. 检查账号状态\n2. 检查网络连通性\n';
const TITLE_PLACEHOLDER = '例如：VPN 拨号失败排查指南';
const MARKDOWN_PLACEHOLDER = /# 问题描述/;
const RICH_EDITOR_PLACEHOLDER = /可直接粘贴或拖拽图片/;
const PREFILL_HINT = /正文来自 AI 助手回答/;

/** 关掉富文本开关（渲染期读取），让「无预填」用例聚焦回退语义而不触发编辑器懒加载。 */
function withRichTextOff(): () => void {
  const prev = process.env.VITE_RICH_TEXT;
  process.env.VITE_RICH_TEXT = 'off';
  return () => {
    if (prev === undefined) delete process.env.VITE_RICH_TEXT;
    else process.env.VITE_RICH_TEXT = prev;
  };
}

describe('新建文章页：承接 AI 助手预填', () => {
  beforeEach(() => {
    mockLocation.state = undefined;
    mockNavigate.mockReset();
    mockFetchConversationArticle.mockReset();
  });

  it('预填标题与正文写入表单，且正文固定 Markdown 模式', async () => {
    mockLocation.state = buildArticlePrefillState(MARKDOWN);
    render(<NewKnowledgeArticlePage />);

    await waitFor(() =>
      expect(screen.getByPlaceholderText(TITLE_PLACEHOLDER)).toHaveValue('VPN 拨号失败排查')
    );
    expect(screen.getByPlaceholderText(MARKDOWN_PLACEHOLDER)).toHaveValue(MARKDOWN);
    // 富文本分支不应出现（整篇 Markdown 塞进 TipTap 会丢标题 / 表格语义）
    expect(screen.queryByPlaceholderText(RICH_EDITOR_PLACEHOLDER)).toBeNull();
    expect(screen.getByText(PREFILL_HINT)).toBeInTheDocument();
  });

  it('无预填：空白新建，不注入内容与提示', async () => {
    const restore = withRichTextOff();
    try {
      render(<NewKnowledgeArticlePage />);

      await waitFor(() => expect(screen.getByPlaceholderText(TITLE_PLACEHOLDER)).toHaveValue(''));
      expect(screen.getByPlaceholderText(MARKDOWN_PLACEHOLDER)).toHaveValue('');
      expect(screen.queryByText(PREFILL_HINT)).toBeNull();
    } finally {
      restore();
    }
  });

  it('来源不符 / 结构不完整的 state 一律视为无预填', async () => {
    const restore = withRichTextOff();
    try {
      mockLocation.state = { source: 'other-page', prefill: { title: 'x', content: 'y' } };
      render(<NewKnowledgeArticlePage />);

      await waitFor(() => expect(screen.getByPlaceholderText(TITLE_PLACEHOLDER)).toHaveValue(''));
      expect(screen.getByPlaceholderText(MARKDOWN_PLACEHOLDER)).toHaveValue('');
    } finally {
      restore();
    }
  });

  it('会话态：经接口拉取整段会话后填入表单（正文不经路由 state 传输）', async () => {
    mockLocation.state = buildConversationArticlePrefillState(3);
    mockFetchConversationArticle.mockResolvedValue({
      title: '会话整理',
      content: '# 会话整理\n\n问答内容',
    });

    render(<NewKnowledgeArticlePage />);

    // 正文由新建页经接口拉取，而不是随路由 state 携带
    expect(mockFetchConversationArticle).toHaveBeenCalledWith(3);

    await waitFor(() =>
      expect(screen.getByPlaceholderText(TITLE_PLACEHOLDER)).toHaveValue('会话整理')
    );
    expect(screen.getByPlaceholderText(MARKDOWN_PLACEHOLDER)).toHaveValue('# 会话整理\n\n问答内容');
    expect(screen.getByText(/正文来自 AI 助手会话/)).toBeInTheDocument();
    expect(screen.queryByText('正在拉取 AI 会话内容…')).toBeNull();
  }, 30000);

  it('会话态拉取失败：给出错误提示且保持空白表单（不产生半成品文章）', async () => {
    mockLocation.state = buildConversationArticlePrefillState(4);
    mockFetchConversationArticle.mockRejectedValue(new Error('boom'));

    render(<NewKnowledgeArticlePage />);

    expect(await screen.findByText(/会话内容加载失败/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByPlaceholderText(TITLE_PLACEHOLDER)).toHaveValue(''));
    expect(screen.getByPlaceholderText(MARKDOWN_PLACEHOLDER)).toHaveValue('');
  });
});

describe('写侧接线（源码契约，防回退到裸跳转）', () => {
  const chatSource = fs.readFileSync(
    path.join(process.cwd(), 'src/components/ai/AIChat.tsx'),
    'utf8'
  );

  it('「补充为知识文章」携带 state 跳转 /knowledge/articles/new', () => {
    expect(chatSource).toMatch(
      /navigate\('\/knowledge\/articles\/new',\s*\{\s*state:\s*buildArticlePrefillState\(target\.content\)\s*\}\)/
    );
  });

  it('不再跳转会丢 state 的历史别名，且回调拿到消息本体', () => {
    expect(chatSource).not.toMatch(/navigate\(\s*['"`]\/knowledge\/articles\/create/);
    expect(chatSource).toContain('onClick={() => onCreateArticle(message)}');
  });

  it('标题栏「保存成文章」只传会话 ID，正文留给新建页经接口拉取', () => {
    expect(chatSource).toMatch(/state:\s*buildConversationArticlePrefillState\(convId\)/);
    expect(chatSource).toContain('保存成文章');
    // 性能约束：不把整段会话（messages / content）内联进 history.state
    expect(chatSource).not.toMatch(/buildConversationArticlePrefillState\(\s*messages/);
  });

  it('会话态拉取期间禁止提交（避免落库一篇空文章）', () => {
    const pageSource = fs.readFileSync(
      path.join(process.cwd(), 'src/pages/(main)/knowledge/articles/new/index.tsx'),
      'utf8'
    );
    expect(pageSource).toContain('if (prefillLoading) return;');
    expect(pageSource).toMatch(/disabled=\{prefillLoading\}/);
  });
});
