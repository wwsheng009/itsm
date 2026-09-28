/**
 * 审批页来源增强测试（M1-06）。
 *
 * 环境说明：antd + jsdom 渲染成本高（单次 render 数十秒），因此按「一次渲染覆盖多条行为」组织；
 * 纯函数口径（来源徽标文案）单独快速断言。
 *
 * 覆盖：来源筛选下发到后端 / 列表展示三元组与风险 / 详情展开完整三元组 / mcp:admin 权限隐藏治理跳转。
 */
import { App } from 'antd';
import { configure, fireEvent, render, screen, waitFor } from '@testing-library/react';

import AIApprovalQueue, { formatSource } from '..';
import { aiGetToolApprovals } from '@/lib/api/ai-api';
import { mcpApi } from '@/lib/api/mcp-api';
import { usePermissions } from '@/lib/hooks/use-permissions';
import type { ToolApproval } from '@/lib/api/ai-api';

jest.setTimeout(300000);
configure({ asyncUtilTimeout: 60000 });

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiGetToolApprovals: jest.fn(),
  aiApproveTool: jest.fn(),
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

// 变量名必须以 mock 开头：jest.mock 工厂只允许引用 mock* 前缀的外层变量。
const mockNavigate = jest.fn();
jest.mock('react-router', () => ({
  __esModule: true,
  useNavigate: () => mockNavigate,
}));

const mockedList = aiGetToolApprovals as jest.MockedFunction<typeof aiGetToolApprovals>;
const mockedMcp = mcpApi as jest.Mocked<typeof mcpApi>;
const mockedPermissions = usePermissions as jest.MockedFunction<typeof usePermissions>;

/** antd 会在两个汉字的按钮文案间插空格（autoInsertSpace），断言统一用容错正则。 */
const btn = (label: string) => new RegExp(label.split('').join('\\s*'));

const approval = (overrides: Partial<ToolApproval> = {}): ToolApproval => ({
  id: 9,
  toolName: 'mcp__mock__create_issue',
  argsRedacted: '{"title":"打印机故障","token":"****"}',
  status: 'pending',
  needsApproval: true,
  approvalState: 'pending',
  createdAt: '2026-09-27T02:00:00Z',
  conversationId: 3,
  userId: 7,
  provider: 'mcp',
  serverName: 'mock',
  rawToolName: 'create_issue',
  callableName: 'mcp__mock__create_issue',
  risk: 'act_high',
  roleSnapshot: 'sysadmin',
  permissionCheck: 'passed',
  durationMs: 320,
  errorCode: 'upstream_timeout',
  ...overrides,
});

const setPermission = (granted: boolean) => {
  mockedPermissions.mockReturnValue({
    hasPermission: (resource: string, action: string) => granted && resource === 'mcp' && action === 'admin',
  } as unknown as ReturnType<typeof usePermissions>);
};

function renderPage() {
  return render(
    <App>
      <AIApprovalQueue />
    </App>
  );
}

describe('formatSource（来源徽标口径）', () => {
  it('内置 / MCP·服务器 / MCP（服务器缺失退化）', () => {
    expect(formatSource({ provider: 'builtin', serverName: '' })).toBe('内置');
    expect(formatSource({ provider: 'mcp', serverName: 'mock' })).toBe('MCP · mock');
    expect(formatSource({ provider: 'mcp', serverName: '' })).toBe('MCP');
    // 老记录（provider 为空）按内置展示，不误标为外部工具。
    expect(formatSource({ provider: undefined, serverName: '' })).toBe('内置');
  });
});

describe('审批页来源增强（M1-06）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockedList.mockResolvedValue({ items: [approval()], state: 'pending', provider: '', server: '' } as never);
    mockedMcp.listServers.mockResolvedValue({
      items: [{ name: 'mock', display_name: 'Mock MCP' }],
      summary: {},
    } as never);
    setPermission(false);
  });

  it('列表展示来源/原始工具名/风险，来源筛选下发后端，详情展开完整三元组', async () => {
    const { container } = renderPage();

    // 首次加载：默认不带来源过滤。
    await waitFor(() => expect(mockedList).toHaveBeenCalledWith('pending', { provider: '', server: '' }));
    expect(await screen.findByText('MCP · mock')).toBeTruthy();
    expect(screen.getByText('原始名 create_issue')).toBeTruthy();
    expect(screen.getByText('act_high')).toBeTruthy();

    // 来源切到 MCP：重新请求并带上 provider（前端不做二次筛选）。
    // 以「当前选中项文案」定位来源下拉（antd 在 selector 上监听 mousedown 打开面板）。
    fireEvent.mouseDown(screen.getByText('全部来源'));
    fireEvent.click(await screen.findByText('MCP 外部工具'));
    await waitFor(() => expect(mockedList).toHaveBeenCalledWith('pending', { provider: 'mcp', server: '' }));

    // 详情展开：三元组 + 执行字段 + 脱敏参数；不得出现未脱敏内容。
    fireEvent.click(container.querySelector('.ant-table-row-expand-icon') as Element);
    expect(await screen.findByText('可调用名')).toBeTruthy();
    // 可调用名在「工具列 + 详情」各出现一次（这里只断言渲染过，不锁定次数）。
    expect(screen.getAllByText('mcp__mock__create_issue').length).toBeGreaterThan(0);
    expect(screen.getByText('320ms')).toBeTruthy();
    expect(screen.getByText('upstream_timeout')).toBeTruthy();
    expect(screen.getByText(/脱敏参数（原始参数仅留在后端执行记录，不回显）/)).toBeTruthy();
    expect(container.textContent ?? '').not.toContain('s3cr3t');
  });

  it('无 mcp:admin：不渲染治理跳转入口（含详情内链接）', async () => {
    const { container } = renderPage();
    await screen.findByText('MCP · mock');

    expect(screen.queryByText(btn('工具治理'))).toBeNull();
    fireEvent.click(container.querySelector('.ant-table-row-expand-icon') as Element);
    await screen.findByText('可调用名');
    expect(screen.queryByText(/前往工具治理页/)).toBeNull();
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  it('有 mcp:admin：顶部治理入口可见并跳转治理页', async () => {
    setPermission(true);
    renderPage();
    await screen.findByText('MCP · mock');

    fireEvent.click(screen.getByText(btn('工具治理')));
    expect(mockNavigate).toHaveBeenCalledWith('/admin/mcp-servers');
  });

  it('服务器列表不可用时退化为手填标识，仍能下发 server 过滤', async () => {
    mockedMcp.listServers.mockRejectedValue(new Error('403'));
    const { container } = renderPage();
    await screen.findByText('MCP · mock');

    // 服务器选择框退化为输入框（无下拉选项时手填服务器标识）。
    const fallback = await screen.findByPlaceholderText('服务器标识');
    fireEvent.change(fallback, { target: { value: 'mock' } });
    await waitFor(() => expect(mockedList).toHaveBeenCalledWith('pending', { provider: '', server: 'mock' }));
    // 状态下拉仍在（服务器维度降级不影响其余筛选）。
    expect(screen.getAllByText('待审批').length).toBeGreaterThan(0);
  });
});
