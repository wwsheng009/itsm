/**
 * I2-02 合规与无障碍：AI Copilot 关键界面的 axe 扫描（WCAG 2.1 A/AA + best-practice）。
 *
 * 口径：
 * - 扫描对象：AssistantChatPanel（面板）/ DiagnosticsDrawer（诊断抽屉，含轨迹视图）/ ProactiveSuggestionBar（内联建议条）
 *   / AssistantMemoryChips（V2-01 记忆条）/ AssistantAsyncAnswerCard（V2-02 异步回执卡）；
 * - jsdom 无真实布局与级联样式，`color-contrast` 无法可靠计算 → **仅**禁用该规则，其余规则全开；
 * - 失败信息包含 axe 的 helpUrl 修复指引（jest-axe 输出）。
 */
import React from 'react';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { App } from 'antd';
import { axe, toHaveNoViolations } from 'jest-axe';

import AssistantChatPanel from '@/components/ai/dock/AssistantChatPanel';
import DiagnosticsDrawer from '@/components/ai/diagnostics/DiagnosticsDrawer';
import ProactiveSuggestionBar from '@/components/ai/dock/ProactiveSuggestionBar';
import AssistantMemoryChips from '@/components/ai/dock/AssistantMemoryChips';
import AssistantAsyncAnswerCard from '@/components/ai/dock/AssistantAsyncAnswerCard';
import { IntentUnderstandingCard } from '@/components/a2ui/IntentUnderstandingCard';
import {
  mergeServerTask,
  useAssistantAsyncStore,
} from '@/lib/ai/async/assistant-async-store';
import { useAssistantMemoryStore } from '@/lib/ai/memory/assistant-memory-store';
import { getCapabilities } from '@/lib/api/capability-api';
import { getUserPreferences } from '@/lib/api/user-preferences-api';
import { getTelemetryMode } from '@/lib/ai/telemetry';
import {
  AIApi,
  aiGetAsyncAnswer,
  aiGetAssistantContext,
  aiGetConversationTrace,
  aiGetPageHelp,
  aiGetRunTrace,
  listConversations,
} from '@/lib/api/ai-api';
import { useLLMProviderFeature } from '@/lib/hooks/use-llm-provider-feature';
import { usePermissions } from '@/lib/hooks/use-permissions';

expect.extend(toHaveNoViolations);
jest.setTimeout(300000);

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  AIApi: { listVisibleBotsWithCapabilities: jest.fn() },
  aiChatStream: jest.fn(),
  getConversationMessages: jest.fn(),
  listConversations: jest.fn(),
  deleteConversation: jest.fn(),
  exportConversation: jest.fn(),
  aiGetPageHelp: jest.fn(),
  aiSearchHelp: jest.fn(),
  aiGetAssistantContext: jest.fn(),
  aiGetRunTrace: jest.fn(),
  aiGetConversationTrace: jest.fn(),
  aiStartAsyncAnswer: jest.fn(),
  aiGetAsyncAnswer: jest.fn(),
}));
jest.mock('@/lib/api/capability-api', () => ({
  __esModule: true,
  getCapabilities: jest.fn(),
}));
jest.mock('@/lib/api/user-preferences-api', () => ({
  __esModule: true,
  getUserPreferences: jest.fn(),
  updateUserPreferences: jest.fn(),
}));
jest.mock('@/lib/hooks/use-llm-provider-feature', () => ({
  __esModule: true,
  useLLMProviderFeature: jest.fn(),
}));
jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
  isAssistantEventName: () => true,
  getTelemetryMode: jest.fn(() => 'off'),
  setTelemetryMode: jest.fn(),
}));
jest.mock('react-router', () => jest.requireActual('react-router'));

// jsdom 限制：color-contrast 依赖真实布局/级联样式，禁用后其余规则照常生效。
const AXE_OPTIONS = { rules: { 'color-contrast': { enabled: false } } };

beforeEach(() => {
  window.localStorage.clear();
  useAssistantMemoryStore.getState().reset();
  useAssistantAsyncStore.getState().reset();
  (getTelemetryMode as jest.Mock).mockReturnValue('off');
  (AIApi as unknown as { listVisibleBotsWithCapabilities: jest.Mock }).listVisibleBotsWithCapabilities.mockResolvedValue({ items: [], capabilities: null });
  (listConversations as jest.Mock).mockResolvedValue([]);
  (aiGetAssistantContext as jest.Mock).mockResolvedValue({ pageKey: 'dashboard', known: false, suggestions: [] });
  (aiGetPageHelp as jest.Mock).mockResolvedValue({ pageKey: 'dashboard', status: 'ok', sections: [] });
  (useLLMProviderFeature as jest.Mock).mockReturnValue({
    enabled: false,
    ready: true,
    providers: [],
    preference: null,
    refresh: jest.fn(),
    setPreference: jest.fn(),
    loading: false,
    error: null,
  });
  (usePermissions as jest.Mock).mockReturnValue({ hasPermission: () => true });
});

describe('AI Copilot 无障碍（I2-02 axe 扫描）', () => {
  it('AssistantChatPanel：面板（空态/页签/输入坞）无 axe 违规', async () => {
    const { container } = render(
      <MemoryRouter>
        <AssistantChatPanel />
      </MemoryRouter>
    );
    await screen.findByTestId('assistant-panel-empty');

    const results = await axe(container, AXE_OPTIONS);
    expect(results).toHaveNoViolations();
  });

  it('DiagnosticsDrawer：诊断抽屉（含轨迹视图）无 axe 违规', async () => {
    (aiGetRunTrace as jest.Mock).mockResolvedValue({
      run: {
        id: 7,
        conversationId: 9,
        botId: 0,
        entrypoint: 'chat',
        status: 'completed',
        startedAt: '2026-10-18T10:00:00Z',
        finishedAt: '2026-10-18T10:00:02Z',
      },
      steps: [],
      events: [],
      toolInvocations: [],
      artifacts: [],
    });

    render(
      <DiagnosticsDrawer
        open
        onClose={() => undefined}
        conversationId={9}
        streaming={false}
        messages={[
          {
            id: 'a1',
            role: 'assistant',
            content: '完成',
            createdAt: '2026-10-18T10:00:02Z',
            run: { runId: 7, status: 'done', stepCount: 1 },
            steps: [{ runId: 7, stepIndex: 0, type: 'llm', durationMs: 500 }],
          },
        ]}
      />
    );
    await screen.findByTestId('run-trace-view');

    // Drawer 渲染在 body portal 内：以 document.body 为扫描上下文。
    const results = await axe(document.body, AXE_OPTIONS);
    expect(results).toHaveNoViolations();
  });

  it('DiagnosticsDrawer：纯问答会话轨迹（ConversationTraceView）无 axe 违规', async () => {
    // O1-01 条件项：纯问答（无 run）回退会话维度轨迹，纳入 axe 扫描面。
    (aiGetConversationTrace as jest.Mock).mockResolvedValue({
      conversation: { id: 9, userId: 7, botId: 0, title: '连不上网', createdAt: '2026-10-18T10:00:00Z' },
      messages: [
        { id: 1, role: 'user', content: '我连不上网了', createdAt: '2026-10-18T10:00:00Z' },
        { id: 2, role: 'assistant', content: '请先确认范围', createdAt: '2026-10-18T10:00:01Z' },
      ],
      messageCount: 2,
      messagesTruncated: false,
      runs: [],
      toolInvocations: [
        {
          id: 3,
          toolName: 'search_kb',
          status: 'success',
          approvalState: 'auto',
          confirmationState: 'unknown',
          needsApproval: false,
          argsRedacted: '{"q":"***"}',
          durationMs: 30,
          stepId: 0,
          userId: 7,
          createdAt: '2026-10-18T10:00:02Z',
        },
      ],
      artifacts: [],
    });

    render(
      <DiagnosticsDrawer
        open
        onClose={() => undefined}
        conversationId={9}
        streaming={false}
        messages={[{ id: 'u1', role: 'user', content: '我连不上网了', createdAt: '2026-10-18T10:00:00Z' }]}
      />
    );
    await screen.findByTestId('conversation-trace-view');

    const results = await axe(document.body, AXE_OPTIONS);
    expect(results).toHaveNoViolations();
  });

  it('ProactiveSuggestionBar：内联建议条无 axe 违规', async () => {
    const { container } = render(
      <ProactiveSuggestionBar
        signal={{
          id: 'page:dashboard:attention_today',
          kind: 'page_suggestion',
          scopeKey: 'page:dashboard',
          label: '建议：今天最需要关注什么？',
          description: '仪表盘',
          severity: 'info',
          count: 0,
          action: { type: 'prompt', prompt: '今天最需要关注什么？' },
        }}
        onAction={jest.fn()}
        onDismiss={jest.fn()}
        onDismissForever={jest.fn()}
      />
    );

    const results = await axe(container, AXE_OPTIONS);
    expect(results).toHaveNoViolations();
  });

  it('AssistantMemoryChips：记忆条（常用语/模板/管理入口）无 axe 违规', async () => {
    (getTelemetryMode as jest.Mock).mockReturnValue('on');
    (getCapabilities as jest.Mock).mockResolvedValue({ items: [], acceptanceVersion: 'test' });
    (getUserPreferences as jest.Mock).mockResolvedValue({
      preferences: {
        assistantMemory: {
          version: 1,
          phrases: ['申请 VPN'],
          templates: [{ name: '网络故障', body: '无法访问内网，已尝试重启' }],
          recent: [],
        },
      },
    });

    const { container } = render(
      <App>
        <AssistantMemoryChips query="申请 VPN" onInsert={jest.fn()} />
      </App>
    );
    await screen.findByTestId('assistant-memory-chips');

    const results = await axe(container, AXE_OPTIONS);
    expect(results).toHaveNoViolations();
  });

  it('AssistantAsyncAnswerCard：异步回执卡（先给可用答案 + 已完成精算）无 axe 违规', async () => {
    useAssistantAsyncStore.getState().start(
      mergeServerTask(undefined, {
        taskId: 'aa_a11y',
        status: 'done',
        prompt: '连不上网怎么办',
        createdAt: '2026-10-18T10:00:00.000Z',
        finishedAt: '2026-10-18T10:00:30.000Z',
        fast: {
          source: 'help_index',
          items: [{ title: '网络故障排查', snippet: '检查 DNS 与网关' }],
        },
        refined: { model: 'gpt-4o', answer: '完整回答：先检查网关…' },
      })
    );

    const { container } = render(
      <App>
        <AssistantAsyncAnswerCard />
      </App>
    );
    await screen.findByTestId('assistant-async-card');

    const results = await axe(container, AXE_OPTIONS);
    expect(results).toHaveNoViolations();
  });

  it('IntentUnderstandingCard：口语→术语确认卡（V1-03）无 axe 违规', async () => {
    const { container } = render(
      <App>
        <IntentUnderstandingCard
          interpretation={{
            summary: '网络故障（高）',
            matched: true,
            needsConfirmation: true,
            confidence: 0.915,
            terms: [
              { field: 'type', surface: '连不上网', term: '网络故障', value: 'network', confidence: 0.93 },
              { field: 'priority', surface: '很急', term: '高', value: 'high', confidence: 0.9 },
            ],
            fields: {
              title: '我的电脑连不上网了，很急',
              type: 'network',
              priority: 'high',
              description: '我的电脑连不上网了，很急',
            },
          }}
          onConfirm={jest.fn()}
          onModify={jest.fn()}
          pageKey="ticket.aiCreate"
        />
      </App>
    );
    await screen.findByText(/我理解成：/);

    const results = await axe(container, AXE_OPTIONS);
    expect(results).toHaveNoViolations();
  });
});
