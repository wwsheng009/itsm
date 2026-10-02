import { App } from 'antd';
import { configure, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import BotTemplatesPage from '..';
import { aiListToolCatalog } from '@/lib/api/ai-api';
import botApi, { type BotGrant, type BotTemplate } from '@/lib/api/bot-api';

/**
 * Bot 管理页组件测试（B2-03）。
 *
 * 环境说明：antd + jsdom 渲染成本高（单次 render 可达数十秒），用例按"一次渲染覆盖多条行为"组织；
 * 纯校验口径见 `src/lib/api/__tests__/bot-api.test.ts`。
 */
// antd + jsdom 在全量并跑 + 覆盖率插桩下单例可达 6min+，统一放大等待预算。
jest.setTimeout(900000);
configure({ asyncUtilTimeout: 60000 });

jest.mock('@/lib/api/bot-api', () => {
  const actual = jest.requireActual('@/lib/api/bot-api');
  return {
    __esModule: true,
    ...actual,
    default: {
      listTemplates: jest.fn(),
      getTemplate: jest.fn(),
      createTemplate: jest.fn(),
      updateTemplate: jest.fn(),
      deleteTemplate: jest.fn(),
      listGrants: jest.fn(),
      upsertGrant: jest.fn(),
      deleteGrant: jest.fn(),
    },
  };
});

jest.mock('@/lib/api/ai-api', () => {
  const actual = jest.requireActual('@/lib/api/ai-api');
  return { __esModule: true, ...actual, aiListToolCatalog: jest.fn() };
});

const mocked = botApi as jest.Mocked<typeof botApi>;
const mockedCatalog = aiListToolCatalog as jest.MockedFunction<typeof aiListToolCatalog>;

const template = (overrides: Partial<BotTemplate> = {}): BotTemplate => ({
  id: 1,
  slug: 'ticket-helper',
  name: '工单助手',
  audience: 'internal',
  riskLimit: 'act_low',
  entrypointsJson: '["chat"]',
  systemPromptRef: '',
  status: 'pilot',
  ...overrides,
});

const grant = (overrides: Partial<BotGrant> = {}): BotGrant => ({
  id: 11,
  botId: 1,
  toolName: 'create_ticket',
  riskLimit: 'act_low',
  argsPolicyJson: '',
  ...overrides,
});

/** antd 会在两个汉字的按钮文案间插空格（autoInsertSpace），断言统一用容错正则。 */
const btn = (label: string) => new RegExp(label.split('').join('\\s*'));

function renderPage() {
  return render(
    <MemoryRouter>
      <App>
        <BotTemplatesPage />
      </App>
    </MemoryRouter>
  );
}

describe('Bot 管理页（B2-03）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mocked.listTemplates.mockResolvedValue({ items: [template()], total: 1 } as never);
    mocked.listGrants.mockResolvedValue({ items: [grant()], total: 1 } as never);
    mocked.createTemplate.mockResolvedValue(template() as never);
    mocked.updateTemplate.mockResolvedValue(template() as never);
    mocked.deleteTemplate.mockResolvedValue({ deleted: true } as never);
    mocked.upsertGrant.mockResolvedValue(grant() as never);
    mocked.deleteGrant.mockResolvedValue({ deleted: true } as never);
    mockedCatalog.mockResolvedValue({
      items: [
        {
          name: 'mcp__mock__echo',
          description: '回显工具',
          readOnly: true,
          risk: 'read',
          provider: 'mcp',
          serverName: 'mock',
        },
        {
          name: 'create_ticket',
          description: '建单',
          readOnly: false,
          risk: 'act_low',
          provider: 'builtin',
        },
      ],
      total: 2,
    });
  });

  it('列表渲染：名称/slug、状态、受众、风险上限、入口标签', async () => {
    renderPage();

    expect(await screen.findByText('工单助手')).toBeInTheDocument();
    expect(screen.getByText('ticket-helper')).toBeInTheDocument();
    expect(screen.getByText('试运行')).toBeInTheDocument();
    expect(screen.getByText('内部人员')).toBeInTheDocument();
    expect(screen.getByText('低风险写')).toBeInTheDocument();
    expect(screen.getByText('chat')).toBeInTheDocument();
    expect(mocked.listTemplates).toHaveBeenCalledTimes(1);
  });

  it('入口未配置的模板显式提示"未配置（不下发）"', async () => {
    mocked.listTemplates.mockResolvedValue({
      items: [template({ id: 2, slug: 'empty-bot', name: '空入口 Bot', entrypointsJson: '[]' })],
      total: 1,
    } as never);
    renderPage();

    expect(await screen.findByText('空入口 Bot')).toBeInTheDocument();
    expect(screen.getByText('未配置（不下发）')).toBeInTheDocument();
  });

  it('新建：必填校验 + 提交调用 createTemplate（slug 落库）', async () => {
    renderPage();
    await screen.findByText('工单助手');

    fireEvent.click(screen.getByRole('button', { name: /新\s*建/ }));
    // 直接提交：slug/name 必填拦截，不触发后端。
    fireEvent.click(screen.getByRole('button', { name: btn('保 存') }));
    await waitFor(() => expect(mocked.createTemplate).not.toHaveBeenCalled());

    fireEvent.change(screen.getByLabelText('slug'), { target: { value: 'kb-helper' } });
    fireEvent.change(screen.getByLabelText('名称'), { target: { value: '知识助手' } });
    fireEvent.click(screen.getByRole('button', { name: btn('保 存') }));

    await waitFor(() =>
      expect(mocked.createTemplate).toHaveBeenCalledWith(
        expect.objectContaining({ slug: 'kb-helper', name: '知识助手', status: 'draft' })
      )
    );
    // 保存后刷新列表。
    await waitFor(() => expect(mocked.listTemplates).toHaveBeenCalledTimes(2));
  });

  it('授权抽屉：加载授权、风险上限越界被前端拦截、保存调用 upsertGrant、删除调用 deleteGrant', async () => {
    renderPage();
    await screen.findByText('工单助手');

    fireEvent.click(screen.getByRole('button', { name: /工具\s*授权/ }));
    await waitFor(() => expect(mocked.listGrants).toHaveBeenCalledWith(1));
    expect(await screen.findByText('create_ticket')).toBeInTheDocument();

    // 越界：模板上限 act_low，选 act_high → 前端拦截，不调后端。
    fireEvent.change(screen.getByPlaceholderText('工具名'), { target: { value: 'delete_ticket' } });
    const riskSelect = screen.getByRole('combobox', { name: '授权风险上限' }) as HTMLElement;
    fireEvent.mouseDown(within(riskSelect.closest('.ant-select') as HTMLElement).getByRole('combobox'));
    fireEvent.click(await screen.findByTitle('高风险写'));
    fireEvent.click(screen.getByRole('button', { name: /保存授权/ }));
    await waitFor(() => expect(mocked.upsertGrant).not.toHaveBeenCalled());

    // 合法：选低风险写 → 调用后端（同工具名 upsert）。
    fireEvent.mouseDown(within(riskSelect.closest('.ant-select') as HTMLElement).getByRole('combobox'));
    fireEvent.click(await screen.findByTitle('低风险写'));
    fireEvent.click(screen.getByRole('button', { name: /保存授权/ }));
    await waitFor(() =>
      expect(mocked.upsertGrant).toHaveBeenCalledWith(
        1,
        expect.objectContaining({ toolName: 'delete_ticket', riskLimit: 'act_low' })
      )
    );

    // 删除授权：Popconfirm 二次确认后调用 deleteGrant。
    const row = (await screen.findByText('create_ticket')).closest('tr') as HTMLElement;
    fireEvent.click(within(row).getByRole('button'));
    fireEvent.click(await screen.findByRole('button', { name: btn('确 认') }));
    await waitFor(() => expect(mocked.deleteGrant).toHaveBeenCalledWith(1, 11));
  });

  it('删除模板二次确认后调用 deleteTemplate；列表刷新', async () => {
    renderPage();
    const name = await screen.findByText('工单助手');
    const row = name.closest('tr') as HTMLElement;

    // 行内三个按钮：编辑 / 工具授权 / 删除（删除为 danger 图标按钮）。
    const rowButtons = within(row).getAllByRole('button');
    fireEvent.click(rowButtons[rowButtons.length - 1]);
    fireEvent.click(await screen.findByRole('button', { name: btn('确 认') }));

    await waitFor(() => expect(mocked.deleteTemplate).toHaveBeenCalledWith(1));
    await waitFor(() => expect(mocked.listTemplates).toHaveBeenCalledTimes(2));
  });

  it('授权抽屉：工具目录（内置 + MCP）可搜索选择，选中 MCP 工具自动预填风险并提示', async () => {
    renderPage();
    await screen.findByText('工单助手');

    fireEvent.click(screen.getByRole('button', { name: /工具\s*授权/ }));
    await waitFor(() => expect(mockedCatalog).toHaveBeenCalled());

    // 输入关键词触发服务端搜索（防抖 300ms），目录项以「名称（来源 · 风险）」渲染。
    const toolInput = screen.getByPlaceholderText('工具名') as HTMLInputElement;
    fireEvent.focus(toolInput);
    fireEvent.change(toolInput, { target: { value: 'mcp__mock__echo' } });
    await waitFor(() => expect(mockedCatalog).toHaveBeenCalledWith(expect.objectContaining({ q: 'mcp__mock__echo' })));

    const option = await screen.findByText(/mcp__mock__echo（MCP/);
    fireEvent.click(option);

    // 选中后：工具名回填 + 按工具风险（read）预填授权风险上限（只读）+ MCP 专项提示可见。
    await waitFor(() => expect(toolInput.value).toBe('mcp__mock__echo'));
    expect(await screen.findByTitle('只读')).toBeInTheDocument();
    expect(screen.getByText(/MCP 工具需服务器与工具均已启用/)).toBeInTheDocument();
  });

  it('bot.enabled=false（整组路由 404）：降级为"功能未启用"引导，不渲染列表', async () => {
    mocked.listTemplates.mockRejectedValue({ httpStatus: 404 } as never);
    renderPage();

    expect(await screen.findByText('功能未启用')).toBeInTheDocument();
    expect(screen.queryByText('工单助手')).not.toBeInTheDocument();
  });

  it('无 ai:write（403）时写操作提示只读，页面仍可浏览', async () => {
    mocked.createTemplate.mockRejectedValue({ httpStatus: 403 } as never);
    renderPage();
    await screen.findByText('工单助手');

    fireEvent.click(screen.getByRole('button', { name: /新\s*建/ }));
    fireEvent.change(screen.getByLabelText('slug'), { target: { value: 'readonly-bot' } });
    fireEvent.change(screen.getByLabelText('名称'), { target: { value: '只读 Bot' } });
    fireEvent.click(screen.getByRole('button', { name: btn('保 存') }));

    expect(await screen.findByText(/无 ai:write 权限/)).toBeInTheDocument();
    // 列表仍在（未被清空）。
    expect(screen.getByText('工单助手')).toBeInTheDocument();
  });

  it('botEnabled=false：顶部 Alert + 新建/编辑/删除/授权保存全部禁用（含 tooltip 说明）', async () => {
    mocked.listTemplates.mockResolvedValue({
      items: [template()],
      total: 1,
      capabilities: { botEnabled: false, mcpWriteEnabled: true },
    } as never);
    renderPage();

    expect(await screen.findByTestId('bots-capability-disabled')).toBeInTheDocument();
    expect(screen.getByTestId('bots-create')).toBeDisabled();

    const row = (await screen.findByText('工单助手')).closest('tr') as HTMLElement;
    const rowButtons = within(row).getAllByRole('button');
    // [编辑, 工具授权(读入口), 删除]：写操作禁用，授权抽屉仍可打开查看。
    expect(rowButtons[0]).toBeDisabled();
    expect(rowButtons[1]).toBeEnabled();
    expect(rowButtons[rowButtons.length - 1]).toBeDisabled();

    fireEvent.click(rowButtons[1]);
    await waitFor(() => expect(mocked.listGrants).toHaveBeenCalledWith(1));
    // 授权保存（「保存授权」提交）同样被禁用；授权仍可展示。
    expect(await screen.findByTestId('bots-grant-submit')).toBeDisabled();
  });

  it('mcpWriteEnabled=false：写面 Alert + 写工具授权徽标 + 选中写工具后禁止保存（可展示不可保存）', async () => {
    mocked.listTemplates.mockResolvedValue({
      items: [template()],
      total: 1,
      capabilities: { botEnabled: true, mcpWriteEnabled: false },
    } as never);
    renderPage();

    expect(await screen.findByTestId('bots-write-face-disabled')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /工具\s*授权/ }));
    expect(await screen.findByTestId('bots-grant-write-face-disabled')).toBeInTheDocument();
    // 目录中 create_ticket 为 readOnly=false：已有授权行显示"全局写面已关闭"徽标。
    expect(await screen.findByTestId('bots-grant-write-blocked-create_ticket')).toBeInTheDocument();

    // 未选中写工具时提交可用；选中写工具后禁用并给出专项提示（仍然展示）。
    expect(screen.getByTestId('bots-grant-submit')).toBeEnabled();
    const toolInput = screen.getByPlaceholderText('工具名') as HTMLInputElement;
    fireEvent.focus(toolInput);
    fireEvent.change(toolInput, { target: { value: 'create_ticket' } });
    const option = await screen.findByText(/create_ticket（内置/);
    fireEvent.click(option);

    await waitFor(() => expect(screen.getByTestId('bots-grant-submit')).toBeDisabled());
    expect(screen.getByTestId('bots-grant-write-picked-blocked')).toBeInTheDocument();
  });
});
