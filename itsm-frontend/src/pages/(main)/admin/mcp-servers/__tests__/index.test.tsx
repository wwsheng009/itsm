import { App } from 'antd';
import { configure, fireEvent, render, screen, waitFor, within } from '@testing-library/react';

import MCPServersAdminPage from '..';
import mcpApi, { type MCPServer, type MCPTool } from '@/lib/api/mcp-api';

/**
 * MCP 管理页组件测试（M0-12）。
 *
 * 环境说明：antd + jsdom 渲染成本高（单次 render 数十秒），因此用例按"一次渲染覆盖多条行为"组织，
 * 纯状态/校验口径另见同目录 `mcp-helpers.test.ts`（快速、无渲染）。
 */
// antd + jsdom 在本仓环境渲染很慢（单次 render 可达数十秒），统一放大等待预算。
jest.setTimeout(300000);
configure({ asyncUtilTimeout: 60000 });

jest.mock('@/lib/api/mcp-api', () => {
  const actual = jest.requireActual('@/lib/api/mcp-api');
  return {
    __esModule: true,
    ...actual,
    default: {
      listServers: jest.fn(),
      healthSummary: jest.fn(),
      getServer: jest.fn(),
      createServer: jest.fn(),
      updateServer: jest.fn(),
      deleteServer: jest.fn(),
      testServer: jest.fn(),
      enableServer: jest.fn(),
      disableServer: jest.fn(),
      reloadServer: jest.fn(),
      listTools: jest.fn(),
      setToolEnabled: jest.fn(),
      bulkSetTools: jest.fn(),
      setToolClassification: jest.fn(),
      rotateCredential: jest.fn(),
      listEvents: jest.fn(),
    },
  };
});

const mocked = mcpApi as jest.Mocked<typeof mcpApi>;

const server = (overrides: Partial<MCPServer> = {}): MCPServer => ({
  id: 1,
  name: 'gitlab',
  display_name: 'GitLab MCP',
  transport: 'streamable_http',
  url: 'https://mcp.example.com/mcp',
  credential_type: 'static_header',
  trust_level: 'untrusted',
  enabled: true,
  status: 'connected',
  running_status: 'connected',
  last_error: '',
  protocol_version: '2025-06-18',
  server_info: 'gitlab-mcp/1.0',
  timeout_ms: 10000,
  max_parallel_calls: 4,
  max_retry: 1,
  version: 3,
  tool_count: 4,
  enabled_tool_count: 2,
  quarantined_tool_count: 0,
  headers_masked: {},
  credential_masked: { token: '****' },
  created_at: '2026-09-27T00:00:00Z',
  updated_at: '2026-09-27T01:00:00Z',
  ...overrides,
});

const tool = (overrides: Partial<MCPTool> = {}): MCPTool => ({
  id: 11,
  raw_name: 'list_issues',
  callable_name: 'mcp__gitlab__list_issues',
  description: 'List issues',
  input_schema: '{"type":"object"}',
  schema_hash: 'abc123',
  read_only: true,
  risk: 'read',
  category: 'issue',
  enabled: true,
  healthy: true,
  configured_enabled: true,
  quarantined: false,
  quarantine_reason: '',
  last_error: '',
  discovered_at: '2026-09-27T00:00:00Z',
  updated_at: '2026-09-27T00:00:00Z',
  server_running_state: 'connected',
  ...overrides,
});

const summary = (overrides: Record<string, number> = {}) => ({
  total: 1,
  enabled: 1,
  connected: 1,
  error: 0,
  auth_required: 0,
  tools: 2,
  enabled_tools: 2,
  quarantined_tools: 0,
  ...overrides,
});

/** antd 会在两个汉字的按钮文案间插空格（autoInsertSpace），断言统一用容错正则。 */
const btn = (label: string) => new RegExp(label.split('').join('\\s*'));

function renderPage() {
  return render(
    <App>
      <MCPServersAdminPage />
    </App>,
  );
}

describe('MCP 管理页（M0-12）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mocked.listServers.mockResolvedValue({ items: [server()], summary: summary() } as never);
    mocked.listTools.mockResolvedValue([] as never);
    mocked.getServer.mockResolvedValue(server() as never);
    mocked.bulkSetTools.mockResolvedValue({ affected: 2, enabled: false } as never);
  });

  it('服务器三态 + 隔离计数 + 工具生效状态 + 批量停用二次确认', async () => {
    mocked.listServers.mockResolvedValue({
      items: [
        server({ quarantined_tool_count: 1 }),
        server({
          id: 2,
          name: 'legacy',
          display_name: 'Legacy',
          enabled: false,
          running_status: 'error',
          last_error: 'auth_required',
          protocol_version: '',
        }),
      ],
      summary: summary({ total: 2, enabled: 1, quarantined_tools: 1 }),
    } as never);
    mocked.listTools.mockResolvedValue([
      tool(),
      tool({
        id: 12,
        callable_name: 'mcp__gitlab__create_issue',
        configured_enabled: true,
        quarantined: true,
        quarantine_reason: 'canonical_collision',
      }),
    ] as never);

    renderPage();

    // 三态：已连接展示运行态 + 协议版本；管理位关闭展示"未启用"并保留最近错误。
    await waitFor(() => expect(screen.getByTestId('mcp-status-1')).toHaveTextContent('connected'));
    expect(screen.getByTestId('mcp-status-2')).toHaveTextContent('未启用');
    expect(screen.getByText('auth_required')).toBeInTheDocument();
    // 协议版本仅对有连接的服务器展示（同文案只出现一次）。
    expect(screen.getAllByText('2025-06-18')).toHaveLength(1);
    // 列表级隔离计数（与工具级状态分离）。
    expect(screen.getByTestId('mcp-quarantine-1')).toHaveTextContent('1 个已隔离');
    expect(screen.getByTestId('mcp-summary-quarantined')).toHaveTextContent('1');

    // 工具治理：行操作直达该服务器（按行限定，避免与其他行同名按钮冲突）。
    const row1 = screen.getByTestId('mcp-row-1');
    fireEvent.click(within(row1).getByRole('button', { name: btn('工具治理') }));
    await waitFor(() =>
      expect(screen.getByTestId('mcp-tool-status-mcp__gitlab__create_issue')).toBeInTheDocument(),
    );
    expect(screen.getByTestId('mcp-tool-status-mcp__gitlab__create_issue')).toHaveTextContent('已隔离');
    expect(screen.getByTestId('mcp-tool-status-mcp__gitlab__list_issues')).toHaveTextContent('生效中');
    expect(screen.getByText('canonical_collision')).toBeInTheDocument();
    // 隔离工具不可直接启用（fail-closed）。
    const enabledSwitches = screen.getAllByRole('switch', { name: '启用工具' });
    expect(enabledSwitches[1]).toBeDisabled();

    // 批量停用：未选行 = 全部，必须先二次确认。
    fireEvent.click(screen.getByRole('button', { name: /批量停用/ }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/该服务器全部工具/)).toBeInTheDocument();
    expect(mocked.bulkSetTools).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole('button', { name: btn('确定') }));
    await waitFor(() => expect(mocked.bulkSetTools).toHaveBeenCalledWith(1, { enabled: false }));
  });

  it('凭据只写不读回（编辑掩码）+ 新增向导第一步校验', async () => {
    renderPage();
    const firstRow = await screen.findByTestId('mcp-row-1');
    const editButton = within(firstRow).getByRole('button', { name: btn('编辑') });

    // 编辑：只展示掩码键名；值输入框为空（不回填明文/掩码）。
    fireEvent.click(editButton);
    await waitFor(() => expect(screen.getByText(/已保存凭据（掩码）：token/)).toBeInTheDocument());
    expect(screen.queryByDisplayValue('****')).toBeNull();
    const lockedName = screen.getByPlaceholderText('gitlab') as HTMLInputElement;
    expect(lockedName).toBeDisabled();
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: btn('取消') }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());

    // 新增向导：必填 → 格式校验，均不得发请求（查询限定在最新打开的弹窗内）。
    fireEvent.click(screen.getByRole('button', { name: /新增服务器/ }));
    const wizard = (await screen.findAllByRole('dialog')).at(-1) as HTMLElement;
    fireEvent.click(within(wizard).getByRole('button', { name: '下一步' }));
    await waitFor(() => expect(within(wizard).getByText('请填写标识')).toBeInTheDocument());

    fireEvent.change(within(wizard).getByPlaceholderText('gitlab'), { target: { value: 'Bad Name' } });
    fireEvent.click(within(wizard).getByRole('button', { name: '下一步' }));
    await waitFor(() =>
      expect(
        within(wizard).getByText('标识仅允许小写字母、数字、下划线、连字符，长度 1–32'),
      ).toBeInTheDocument(),
    );
    expect(mocked.createServer).not.toHaveBeenCalled();
  });

  it('降级路径：开关关闭（404）与服务未就绪（503）分别给出引导', async () => {
    mocked.listServers.mockRejectedValueOnce({ httpStatus: 404 } as never);
    const first = renderPage();
    await waitFor(() => expect(screen.getByTestId('mcp-unavailable')).toHaveTextContent('MCP 外部工具功能未启用'));
    expect(screen.queryByText('还没有 MCP 服务器')).toBeNull();
    first.unmount();

    mocked.listServers.mockRejectedValueOnce({ httpStatus: 503, errorCode: 'unavailable' } as never);
    renderPage();
    await waitFor(() => expect(screen.getByTestId('mcp-unavailable')).toHaveTextContent('MCP 管理服务未就绪'));
  });
});
