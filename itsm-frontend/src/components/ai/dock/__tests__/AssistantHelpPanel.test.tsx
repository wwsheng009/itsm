/**
 * H0-04 面板帮助视图测试：覆盖状态渲染 / 缺失-过时引导 / 检索 / 未注册页面降级。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import AssistantHelpPanel from '../AssistantHelpPanel';
import { aiGetPageHelp, aiSearchHelp, type HelpPageResult } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetPageHelp: jest.fn(),
  aiSearchHelp: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));
// 需要真实路由上下文解析 pageKey；jest.setup.js 全局 mock 了 useLocation → 还原真实实现。
jest.mock('react-router', () => jest.requireActual('react-router'));

const mockedPageHelp = aiGetPageHelp as jest.MockedFunction<typeof aiGetPageHelp>;
const mockedSearch = aiSearchHelp as jest.MockedFunction<typeof aiSearchHelp>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const okFixture: HelpPageResult = {
  pageKey: 'ticket.detail',
  status: 'ok',
  title: '工单详情',
  pattern: '/tickets/:id',
  entrypoint: 'ticket_detail',
  docs: [
    {
      path: 'docs/help/tickets/detail.md',
      title: '工单详情',
      pageKeys: ['ticket.detail'],
      text: '# 工单详情\n\n点击「问 AI」可生成分诊建议与答复草稿（只读）。',
    },
  ],
  coverage: { coverage: 1, totals: { pages: 21, ok: 21, missing: 0, stale: 0, orphan: 0 } },
};

const renderPanel = (route: string, onDraft = jest.fn()) => {
  const utils = render(
    <MemoryRouter initialEntries={[route]}>
      <AssistantHelpPanel onDraft={onDraft} />
    </MemoryRouter>
  );
  return { onDraft, container: utils.container };
};

beforeEach(() => {
  mockedPageHelp.mockReset();
  mockedSearch.mockReset();
  mockedTrack.mockReset();
});

describe('AssistantHelpPanel（H0-04 面板帮助视图）', () => {
  it('已覆盖页面：渲染状态、覆盖率与文档正文', async () => {
    mockedPageHelp.mockResolvedValue(okFixture);

    renderPanel('/tickets/42');

    await waitFor(() =>
      expect(mockedPageHelp).toHaveBeenCalledWith('ticket.detail')
    );
    expect(await screen.findByTestId('help-status')).toHaveTextContent('已覆盖');
    expect(screen.getByTestId('help-coverage')).toHaveTextContent('覆盖率 100%');
    expect(screen.getByTestId('help-doc')).toHaveTextContent('分诊建议');
  });

  it('加载中不得谎报「未注册页面」：已注册页面首帧显示查询中', async () => {
    // 永不 resolve，锁定首帧状态。
    mockedPageHelp.mockReturnValue(new Promise<HelpPageResult>(() => {}));

    renderPanel('/tickets/42');

    const tag = await screen.findByTestId('help-status');
    expect(tag).toHaveTextContent('查询中');
    expect(tag).not.toHaveTextContent('未注册页面');
  });

  it('缺失页面：提示 + 「让 AI 生成初稿」发送结构化 prompt（含 pageKey 与不发布约束）', async () => {
    mockedPageHelp.mockResolvedValue({
      ...okFixture,
      pageKey: 'dashboard',
      status: 'missing',
      title: '仪表盘',
      docs: [],
    });
    const { onDraft } = renderPanel('/dashboard');

    expect(await screen.findByTestId('help-missing-alert')).toHaveTextContent('暂无帮助文档');
    fireEvent.click(screen.getByTestId('help-generate-draft'));

    expect(onDraft).toHaveBeenCalledTimes(1);
    const prompt = onDraft.mock.calls[0][0] as string;
    expect(prompt).toContain('pageKey=dashboard');
    expect(prompt).toContain('help_create_draft');
    expect(prompt).toContain('不要发布');
  });

  it('过时页面：提示改为「已过时」，仍可引导生成初稿', async () => {
    mockedPageHelp.mockResolvedValue({
      ...okFixture,
      status: 'stale',
      docs: [],
    });
    renderPanel('/tickets/42');

    expect(await screen.findByTestId('help-missing-alert')).toHaveTextContent('已过时');
    expect(screen.getByTestId('help-generate-draft')).toBeInTheDocument();
  });

  it('帮助检索：提交关键词调用 /ai/help/search 并渲染结果', async () => {
    mockedPageHelp.mockResolvedValue(okFixture);
    mockedSearch.mockResolvedValue({
      results: [
        {
          path: 'docs/help/sla/monitor.md',
          title: 'SLA 监控',
          pageKeys: ['sla.monitor'],
          snippet: '临近违约清单与风险解释',
          score: 5,
        },
      ],
    });

    renderPanel('/tickets/42');
    await waitFor(() => expect(mockedPageHelp).toHaveBeenCalled());

    // antd Input.Search 的 data-testid 落在包装节点上：用真实输入框（placeholder 定位）。
    const input = screen.getByPlaceholderText('搜索帮助文档（如：SLA 违约）');
    fireEvent.change(input, { target: { value: 'SLA' } });
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', keyCode: 13 });
    // 回车未触发时，点击搜索按钮兜底（不同 antd 版本的 enterButton 行为差异）。
    const searchButton = document.querySelector('.ant-input-search-button');
    if (searchButton) fireEvent.click(searchButton);

    await waitFor(() =>
      expect(mockedSearch).toHaveBeenCalledWith('SLA', undefined, 5)
    );
    expect(await screen.findByTestId('help-result-docs/help/sla/monitor.md')).toHaveTextContent(
      'SLA 监控'
    );
  });

  it('未注册页面：显示“暂无帮助文档”且提供推荐搜索入口，不请求页面帮助', async () => {
    mockedSearch.mockResolvedValue({ results: [] });
    renderPanel('/no/such/page');

    const alert = await screen.findByTestId('help-unknown-page');
    expect(alert).toHaveTextContent('当前页面暂无帮助文档');
    expect(alert).not.toHaveTextContent('未注册页面键');
    expect(screen.getByTestId('help-status')).toHaveTextContent('暂无帮助');
    expect(mockedPageHelp).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId('help-topic-topicProgress'));
    await waitFor(() => expect(mockedSearch).toHaveBeenCalledWith('请求进度', undefined, 5));
  });

  it('H1-01：缺失页面上报 help_gap（含 coverageStatus 与 route）', async () => {
    mockedPageHelp.mockResolvedValue({
      ...okFixture,
      pageKey: 'dashboard',
      status: 'missing',
      title: '仪表盘',
      docs: [],
    });
    renderPanel('/dashboard');

    expect(await screen.findByTestId('help-missing-alert')).toBeInTheDocument();
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'help_gap',
        expect.objectContaining({
          pageKey: 'dashboard',
          route: '/dashboard',
          coverageStatus: 'missing',
        })
      )
    );
  });

  it('H1-01：已覆盖页面不上报 help_gap', async () => {
    mockedPageHelp.mockResolvedValue(okFixture);
    renderPanel('/tickets/42');

    expect(await screen.findByTestId('help-status')).toHaveTextContent('已覆盖');
    expect(mockedTrack).not.toHaveBeenCalledWith('help_gap', expect.anything());
  });

  it('P2：帮助打开上报 help_opened（含 coverageStatus/docCount），同一页面只报一次', async () => {
    mockedPageHelp.mockResolvedValue(okFixture);
    renderPanel('/tickets/42');

    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'help_opened',
        expect.objectContaining({
          pageKey: 'ticket.detail',
          route: '/tickets/42',
          coverageStatus: 'ok',
          extra: expect.objectContaining({ docCount: 1 }),
        })
      )
    );
    // 去重：同一 pageKey 只上报一次（重渲染/刷新不重复放大分母）。
    expect(mockedTrack.mock.calls.filter(call => call[0] === 'help_opened')).toHaveLength(1);
  });

  it('P2：文档反馈「已解决」→ doc_feedback(helpful=true)，反馈后固化', async () => {
    mockedPageHelp.mockResolvedValue(okFixture);
    renderPanel('/tickets/42');

    const helpful = await screen.findByTestId('help-feedback-helpful');
    expect(screen.getByTestId('help-feedback-unhelpful')).toBeInTheDocument();

    fireEvent.click(helpful);
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'doc_feedback',
        expect.objectContaining({
          pageKey: 'ticket.detail',
          extra: expect.objectContaining({ helpful: true, docCount: 1 }),
        })
      )
    );
    expect(await screen.findByTestId('help-feedback-thanks')).toBeInTheDocument();
    // 反馈后两档按钮均禁用，不重复上报（每条文档视图最多一次）。
    expect(screen.getByTestId('help-feedback-helpful')).toBeDisabled();
    expect(screen.getByTestId('help-feedback-unhelpful')).toBeDisabled();
    expect(mockedTrack.mock.calls.filter(call => call[0] === 'doc_feedback')).toHaveLength(1);
  });

  it('P2：文档反馈「未解决」→ doc_feedback(helpful=false)', async () => {
    mockedPageHelp.mockResolvedValue(okFixture);
    renderPanel('/tickets/42');

    fireEvent.click(await screen.findByTestId('help-feedback-unhelpful'));
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'doc_feedback',
        expect.objectContaining({
          pageKey: 'ticket.detail',
          extra: expect.objectContaining({ helpful: false }),
        })
      )
    );
  });

  it('P2：缺失页面不渲染文档反馈（无文档可反馈）', async () => {
    mockedPageHelp.mockResolvedValue({
      ...okFixture,
      pageKey: 'dashboard',
      status: 'missing',
      title: '仪表盘',
      docs: [],
    });
    renderPanel('/dashboard');

    expect(await screen.findByTestId('help-missing-alert')).toBeInTheDocument();
    expect(screen.queryByTestId('help-doc-feedback')).not.toBeInTheDocument();
  });

  it('P2：渲染服务商分发帮助（来源标注，active/unbound 区分）', async () => {
    mockedPageHelp.mockResolvedValue({
      ...okFixture,
      distributedDocs: [
        {
          title: '服务商入库指引',
          pageKey: 'ticket.detail',
          text: '# 入库指引\n\n先登记资产再入库',
          source: 'msp_provider',
          providerTenantId: 7,
          sourceSlug: 'intake-guide',
          sourceVersion: 2,
          status: 'active',
        },
        {
          title: '历史副本',
          pageKey: 'ticket.detail',
          text: '# 历史',
          source: 'msp_provider',
          providerTenantId: 7,
          sourceSlug: 'legacy-guide',
          sourceVersion: 1,
          status: 'unbound',
        },
      ],
    });
    renderPanel('/tickets/42');

    expect(await screen.findByTestId('help-distributed-doc-intake-guide')).toHaveTextContent('服务商分发');
    expect(screen.getByTestId('help-distributed-doc-intake-guide')).toHaveTextContent('intake-guide v2');
    expect(screen.getByTestId('help-distributed-doc-legacy-guide')).toHaveTextContent('已解绑·原生副本');
  });

  it('P2：仅服务商分发文档时仍可反馈（docCount 含分发数）', async () => {
    mockedPageHelp.mockResolvedValue({
      ...okFixture,
      docs: [],
      distributedDocs: [
        {
          title: '服务商入库指引',
          pageKey: 'ticket.detail',
          text: '# 入库指引',
          source: 'msp_provider',
          providerTenantId: 7,
          sourceSlug: 'intake-guide',
          sourceVersion: 2,
          status: 'active',
        },
      ],
    });
    renderPanel('/tickets/42');

    fireEvent.click(await screen.findByTestId('help-feedback-helpful'));
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'doc_feedback',
        expect.objectContaining({ extra: expect.objectContaining({ helpful: true, docCount: 1 }) })
      )
    );
  });

  it('P2：检索结果包含服务商分发分区（内嵌为空也不显示「没有匹配」）', async () => {
    mockedPageHelp.mockResolvedValue(okFixture);
    mockedSearch.mockResolvedValue({
      results: [],
      distributedResults: [
        {
          title: '服务商入库指引',
          pageKey: 'ticket.list',
          snippet: '先登记资产再入库',
          source: 'msp_provider',
          providerTenantId: 7,
          sourceSlug: 'intake-guide',
          sourceVersion: 2,
          status: 'active',
        },
      ],
    });
    renderPanel('/tickets/42');
    await waitFor(() => expect(mockedPageHelp).toHaveBeenCalled());

    const input = screen.getByPlaceholderText('搜索帮助文档（如：SLA 违约）');
    fireEvent.change(input, { target: { value: '入库' } });
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', keyCode: 13 });
    const searchButton = document.querySelector('.ant-input-search-button');
    if (searchButton) fireEvent.click(searchButton);

    expect(await screen.findByTestId('help-distributed-result-intake-guide')).toHaveTextContent(
      '服务商分发'
    );
    expect(screen.queryByText('没有匹配的帮助文档')).not.toBeInTheDocument();
  });
});
