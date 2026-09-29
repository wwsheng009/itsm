import { App } from 'antd';
import { configure, fireEvent, render, screen, waitFor, within } from '@testing-library/react';

import ToolsCatalogPage from '..';
import { aiListToolCatalog, type ToolCatalogItem } from '@/lib/api/ai-api';

/**
 * 工具目录页组件测试（/admin/tools）。
 *
 * 环境说明：antd + jsdom 渲染成本高（单次 render 可达数十秒），用例按"一次渲染覆盖多条行为"组织。
 */
jest.setTimeout(300000);
configure({ asyncUtilTimeout: 60000 });

jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiListToolCatalog: jest.fn(),
}));

const mockedList = aiListToolCatalog as jest.MockedFunction<typeof aiListToolCatalog>;

const builtinTool = (overrides: Partial<ToolCatalogItem> = {}): ToolCatalogItem => ({
  name: 'create_ticket',
  description: '创建工单',
  readOnly: false,
  risk: 'act_low',
  category: 'ticket',
  provider: 'builtin',
  ...overrides,
});

const mcpTool = (overrides: Partial<ToolCatalogItem> = {}): ToolCatalogItem => ({
  name: 'mcp__mock__echo',
  description: 'Echo back',
  readOnly: true,
  risk: 'read',
  category: 'mock',
  provider: 'mcp',
  serverName: 'mock',
  rawToolName: 'echo',
  ...overrides,
});

function renderPage() {
  return render(
    <App>
      <ToolsCatalogPage />
    </App>
  );
}

/** 打开指定筛选 Select 的下拉（antd 在 selector 上监听 mousedown）。 */
function openSelect(testId: string) {
  fireEvent.mouseDown(within(screen.getByTestId(testId)).getByRole('combobox'));
}

describe('工具目录页（/admin/tools）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockedList.mockResolvedValue({ items: [builtinTool(), mcpTool()], total: 2 });
  });

  it('渲染内置与 MCP 两行：MCP 标签、服务器小字、只读/风险投影与统计行', async () => {
    renderPage();

    expect(await screen.findByText('create_ticket')).toBeInTheDocument();
    expect(screen.getByText('mcp__mock__echo')).toBeInTheDocument();
    // MCP 行：工具名旁的 MCP 标签 + 名称下的服务器小字。
    expect(screen.getByText('MCP')).toBeInTheDocument();
    expect(screen.getByText('服务器：mock')).toBeInTheDocument();
    // 来源列：内置工具 / MCP 工具。
    expect(screen.getByText('内置工具')).toBeInTheDocument();
    expect(screen.getByText('MCP 工具')).toBeInTheDocument();
    // 只读/风险投影（内置写工具 + MCP 只读工具）。
    expect(screen.getByText('是')).toBeInTheDocument();
    expect(screen.getByText('否')).toBeInTheDocument();
    expect(screen.getByText('低风险写')).toBeInTheDocument();
    // 「只读」同时出现在列头与 MCP 行的风险 Tag（read）。
    expect(screen.getAllByText('只读')).toHaveLength(2);
    // 统计行插值。
    expect(screen.getByText('共 2 个工具')).toBeInTheDocument();
    expect(mockedList).toHaveBeenCalledTimes(1);
  });

  it('搜索防抖与来源/只读/风险筛选均触发带参重查', async () => {
    renderPage();
    await screen.findByText('create_ticket');

    // 搜索：300ms 防抖后带 q 重查（服务端过滤）。
    fireEvent.change(screen.getByPlaceholderText('搜索工具名 / 描述 / 服务器'), {
      target: { value: 'echo' },
    });
    await waitFor(() => expect(mockedList).toHaveBeenCalledWith(expect.objectContaining({ q: 'echo' })));

    // 来源：MCP → source=mcp。
    openSelect('tool-catalog-source-filter');
    fireEvent.click(await screen.findByTitle('MCP 工具'));
    await waitFor(() => expect(mockedList).toHaveBeenCalledWith(expect.objectContaining({ source: 'mcp' })));

    // 只读：仅只读 → readOnly=true。
    openSelect('tool-catalog-readonly-filter');
    fireEvent.click(await screen.findByTitle('仅只读'));
    await waitFor(() => expect(mockedList).toHaveBeenCalledWith(expect.objectContaining({ readOnly: true })));

    // 风险：高风险写 → risk=act_high。
    openSelect('tool-catalog-risk-filter');
    fireEvent.click(await screen.findByTitle('高风险写'));
    await waitFor(() => expect(mockedList).toHaveBeenCalledWith(expect.objectContaining({ risk: 'act_high' })));
  });

  it('空结果显示空态文案与 total=0', async () => {
    mockedList.mockResolvedValue({ items: [], total: 0 });
    renderPage();

    expect(await screen.findByText(/没有匹配的工具/)).toBeInTheDocument();
    expect(screen.getByText('共 0 个工具')).toBeInTheDocument();
  });
});
