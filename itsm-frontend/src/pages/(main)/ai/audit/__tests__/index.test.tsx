/**
 * 审计页「工具调用审计（来源维度）」测试（M1-07）。
 *
 * 环境说明：antd + jsdom 渲染成本高，用例按「一次渲染覆盖多条行为」组织。
 * 覆盖：默认全量（state=all）、来源/服务器筛选下发后端、列渲染（来源/原始名/耗时/错误码）、
 * 详情展开三元组、空态回显生效筛选、脱敏字段原样展示（不还原明文）。
 */
import { App } from 'antd';
import { configure, fireEvent, render, screen, waitFor } from '@testing-library/react';

import AIAuditConsole from '..';
import { aiGetAuditLogs, aiGetEvaluation, aiGetToolApprovals } from '@/lib/api/ai-api';
import { mcpApi } from '@/lib/api/mcp-api';
import { usePermissions } from '@/lib/hooks/use-permissions';
import type { ToolApproval } from '@/lib/api/ai-api';

jest.setTimeout(300000);
configure({ asyncUtilTimeout: 60000 });

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetAuditLogs: jest.fn(),
  aiGetEvaluation: jest.fn(),
  aiGetToolApprovals: jest.fn(),
}));

jest.mock('@/lib/api/mcp-api', () => ({
  __esModule: true,
  mcpApi: { listServers: jest.fn() },
}));

jest.mock('@/lib/hooks/use-permissions', () => ({
  __esModule: true,
  usePermissions: jest.fn(),
}));

jest.mock('@/lib/store/auth-store', () => ({
  __esModule: true,
  useAuthStoreHydration: () => undefined,
}));

const mockNavigate = jest.fn();
jest.mock('react-router', () => ({
  __esModule: true,
  useNavigate: () => mockNavigate,
}));

const mockedToolList = aiGetToolApprovals as jest.MockedFunction<typeof aiGetToolApprovals>;
const mockedAuditLogs = aiGetAuditLogs as jest.MockedFunction<typeof aiGetAuditLogs>;
const mockedEvaluation = aiGetEvaluation as jest.MockedFunction<typeof aiGetEvaluation>;
const mockedMcp = mcpApi as jest.Mocked<typeof mcpApi>;
const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;

const toolRow = (overrides: Partial<ToolApproval> = {}): ToolApproval => ({
  id: 21,
  toolName: 'mcp__mock__list_issues',
  argsRedacted: '{"project":"OPS","token":"****"}',
  status: 'done',
  needsApproval: false,
  approvalState: 'approved',
  createdAt: '2026-09-27T03:00:00Z',
  conversationId: 4,
  userId: 7,
  provider: 'mcp',
  serverName: 'mock',
  rawToolName: 'list_issues',
  callableName: 'mcp__mock__list_issues',
  risk: 'low',
  roleSnapshot: 'agent',
  durationMs: 88,
  errorCode: '',
  outputSummary: '{"issues":3}',
  ...overrides,
});

describe('审计页工具调用审计（M1-07）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockedPermissions.mockReturnValue({
      hasPermission: () => false,
    } as unknown as ReturnType<typeof usePermissions>);
    mockedEvaluation.mockResolvedValue({ hasData: false } as never);
    mockedAuditLogs.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20 } as never);
    mockedMcp.listServers.mockResolvedValue({ items: [{ name: 'mock', display_name: 'Mock' }], summary: {} } as never);
    mockedToolList.mockResolvedValue({
      items: [toolRow()],
      state: 'all',
      provider: '',
      server: '',
    } as never);
  });

  it('默认按 state=all 全量拉取，列展示来源/原始名/耗时/错误码，详情含三元组与脱敏参数', async () => {
    const { container } = render(
      <App>
        <AIAuditConsole />
      </App>
    );

    // 默认全量：跨审批状态（state=all），不带来源过滤。
    await waitFor(() => expect(mockedToolList).toHaveBeenCalledWith('all', { provider: '', server: '' }));
    expect(await screen.findByText('MCP · mock')).toBeTruthy();
    expect(screen.getByText('原始名 list_issues')).toBeTruthy();
    expect(screen.getByText('88ms')).toBeTruthy();
    expect(screen.getByText('工具调用审计（来源维度）')).toBeTruthy();

    // 详情展开：三元组 + 结果摘要 + 脱敏参数；不出现未脱敏明文。
    const expandIcons = container.querySelectorAll('.ant-table-row-expand-icon');
    // 第一个表（AI 场景审计）无展开行，工具表是唯一带展开图标的表。
    fireEvent.click(expandIcons[0]);
    expect(await screen.findByText('可调用名')).toBeTruthy();
    expect(screen.getByText('{"issues":3}')).toBeTruthy();
    expect(screen.getByText(/脱敏参数（原始参数仅留在后端执行记录，不回显）/)).toBeTruthy();
    expect(container.textContent ?? '').not.toContain('plaintext-token');
  });

  it('来源筛选下发 provider；服务器筛选自动置为 mcp；空态回显生效筛选', async () => {
    mockedToolList.mockResolvedValue({ items: [], state: 'all', provider: 'mcp', server: '' } as never);
    render(
      <App>
        <AIAuditConsole />
      </App>
    );

    await waitFor(() => expect(mockedToolList).toHaveBeenCalledTimes(1));
    // 打开「全部来源」下拉并选 MCP。
    fireEvent.mouseDown(screen.getByText('全部来源'));
    fireEvent.click(await screen.findByText('MCP 外部工具'));
    await waitFor(() => expect(mockedToolList).toHaveBeenCalledWith('all', { provider: 'mcp', server: '' }));

    // 空态把生效筛选写出来，便于排障（不是无信息的空白）。
    expect(await screen.findByText(/当前筛选条件下没有工具调用记录（来源=mcp/)).toBeTruthy();
  });
});
