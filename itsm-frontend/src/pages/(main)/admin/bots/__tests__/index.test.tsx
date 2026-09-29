import { App } from 'antd';
import { configure, fireEvent, render, screen, waitFor, within } from '@testing-library/react';

import BotTemplatesPage from '..';
import botApi, { type BotGrant, type BotTemplate } from '@/lib/api/bot-api';

/**
 * Bot 管理页组件测试（B2-03）。
 *
 * 环境说明：antd + jsdom 渲染成本高（单次 render 可达数十秒），用例按"一次渲染覆盖多条行为"组织；
 * 纯校验口径见 `src/lib/api/__tests__/bot-api.test.ts`。
 */
jest.setTimeout(300000);
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

const mocked = botApi as jest.Mocked<typeof botApi>;

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
    <App>
      <BotTemplatesPage />
    </App>
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
    const riskSelect = screen.getByRole('combobox', { name: '' }) as HTMLElement;
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
});
