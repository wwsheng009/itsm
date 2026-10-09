/**
 * CrossTenantBoard 测试（跨租户单据流转 P2-②/P2-③ 前端入口）。
 *
 * 覆盖：
 * - 任务投影：claim/complete 按 allowedActions 渲染（allowed=true 可点 / false 禁用）+ 认领写调用；
 * - 单据读面：状态推进弹窗（域白名单选项）与写调用；
 * - 动作契约：status.allowed=false 时按钮禁用（XF5：前端只读消费，不推断权限）；
 * - 记录类型切换：服务请求独立面（slug=service-requests）。
 *
 * 说明：与工作台页测试同口径，`useSearchParams` 直接 mock（避免测试期真实 Router 上下文差异）。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor, within } from '@/lib/test-utils';
import userEvent from '@testing-library/user-event';
import CrossTenantBoard, { type CrossTenantBoardApi } from '../CrossTenantBoard';
import type { MSPRecordItem, MSPTaskItem } from '@/lib/api/msp-records-api';

jest.setTimeout(120000);

// 可变的 useSearchParams 输入（jest.mock 工厂仅允许 mock* 前缀引用）。
let mockSearchParams = 'customerTenantIds=11';

jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  useSearchParams: () => [new URLSearchParams(mockSearchParams), jest.fn()],
}));

const queueTask: MSPTaskItem = {
  taskId: 'TASK-queue',
  taskName: '处理客户变更 A',
  taskDefinitionKey: 'msp_task',
  processDefinitionKey: 'change_proc',
  processInstanceId: 1,
  status: 'created',
  priority: 'medium',
  createdTime: '2026-10-09T08:00:00Z',
  customerTenantId: 11,
  customerTenantName: '测试客户',
  source: 'queue',
  allowedActions: [
    { action: 'claim', allowed: true },
    { action: 'complete', allowed: false, reasonCode: 'TASK_NOT_CLAIMED', reasonText: '请先认领' },
  ],
};

const assignedTask: MSPTaskItem = {
  ...queueTask,
  taskId: 'TASK-assigned',
  taskName: '处理客户变更 B',
  source: 'assigned',
  allowedActions: [
    { action: 'claim', allowed: false, reasonCode: 'TASK_ALREADY_ASSIGNED' },
    { action: 'complete', allowed: true },
  ],
};

const changeItem: MSPRecordItem = {
  recordType: 'change',
  id: 26,
  customerTenantId: 11,
  customerName: '测试客户',
  number: 'CHG-P215',
  title: 'P215 实弹变更',
  status: 'approved',
  priority: 'medium',
  assigneeName: '张三',
  updatedAt: '2026-10-09T09:00:00Z',
  allowedActions: [{ action: 'view', allowed: true }, { action: 'status', allowed: true }],
};

const deniedChangeItem: MSPRecordItem = {
  ...changeItem,
  id: 27,
  number: 'CHG-P215-12',
  allowedActions: [
    { action: 'view', allowed: true },
    {
      action: 'status',
      allowed: false,
      reasonCode: 'MSP_ALLOCATION_REQUIRED',
      reasonText: '未分配该客户',
    },
  ],
};

function buildApi(overrides: Partial<CrossTenantBoardApi> = {}): CrossTenantBoardApi {
  return {
    listTasks: jest.fn().mockResolvedValue({ items: [queueTask, assignedTask], total: 2 }),
    claimTask: jest.fn().mockResolvedValue(queueTask),
    completeTask: jest.fn().mockResolvedValue(assignedTask),
    listRecords: jest.fn().mockResolvedValue({ items: [changeItem], total: 1 }),
    getRecord: jest.fn().mockResolvedValue({ ...changeItem, description: '变更描述' }),
    updateRecordStatus: jest.fn().mockResolvedValue(changeItem),
    ...overrides,
  };
}

describe('CrossTenantBoard · 任务投影', () => {
  beforeEach(() => {
    mockSearchParams = 'customerTenantIds=11';
  });

  it('按 allowedActions 渲染认领/完成（池任务认领可点、指派任务完成可点）', async () => {
    const api = buildApi();
    render(<CrossTenantBoard kind="tasks" api={api} />);

    await waitFor(() =>
      expect(api.listTasks).toHaveBeenCalledWith({ customerTenantIds: [11], limit: 50 })
    );
    expect(await screen.findByText('处理客户变更 A')).toBeInTheDocument();

    expect(screen.getByTestId('task-claim-TASK-queue')).toBeEnabled();
    expect(screen.getByTestId('task-complete-TASK-queue')).toBeDisabled();
    expect(screen.getByTestId('task-claim-TASK-assigned')).toBeDisabled();
    expect(screen.getByTestId('task-complete-TASK-assigned')).toBeEnabled();
  });

  it('认领任务：Popconfirm 确认后携带声明租户调用 claim', async () => {
    const api = buildApi();
    const user = userEvent.setup();
    render(<CrossTenantBoard kind="tasks" api={api} />);

    await screen.findByText('处理客户变更 A');
    await user.click(screen.getByTestId('task-claim-TASK-queue'));

    const title = await screen.findByText('认领该任务？');
    const popover = title.closest('.ant-popover') as HTMLElement;
    const confirm = popover.querySelector('.ant-btn-primary') as HTMLElement;
    expect(confirm).toBeTruthy();
    await user.click(confirm);

    await waitFor(() =>
      expect(api.claimTask).toHaveBeenCalledWith('TASK-queue', { customerTenantId: 11 })
    );
  });
});

describe('CrossTenantBoard · 跨租户单据', () => {
  beforeEach(() => {
    mockSearchParams = 'customerTenantIds=11';
  });

  it('状态推进：弹窗目标状态为域白名单，提交携带声明租户', async () => {
    const api = buildApi();
    const user = userEvent.setup();
    render(<CrossTenantBoard kind="records" api={api} />);

    await waitFor(() =>
      expect(api.listRecords).toHaveBeenCalledWith('change', {
        customerTenantIds: [11],
        limit: 50,
        cursor: undefined,
      })
    );
    expect(await screen.findByText('CHG-P215')).toBeInTheDocument();

    await user.click(screen.getByTestId('record-status-change-26'));
    expect(await screen.findByText(/推进变更状态/)).toBeInTheDocument();

    const select = screen.getByTestId('record-status-select');
    fireEvent.mouseDown(select.querySelector('.ant-select-selector') ?? select);
    fireEvent.click(await screen.findByText('已排期'));

    await user.click(screen.getByRole('button', { name: /^推\s*进$/ }));

    await waitFor(() =>
      expect(api.updateRecordStatus).toHaveBeenCalledWith(
        'change',
        26,
        expect.objectContaining({ customerTenantId: 11, status: 'scheduled' })
      )
    );
  });

  it('status.allowed=false：按钮禁用（不渲染可点操作，XF5）', async () => {
    const api = buildApi({
      listRecords: jest.fn().mockResolvedValue({ items: [deniedChangeItem], total: 1 }),
    });
    render(<CrossTenantBoard kind="records" api={api} />);

    expect(await screen.findByText('CHG-P215-12')).toBeInTheDocument();
    expect(screen.getByTestId('record-status-change-27')).toBeDisabled();
  });

  it('切换记录类型：服务请求独立面（slug=service-requests）', async () => {
    const api = buildApi();
    const user = userEvent.setup();
    render(<CrossTenantBoard kind="records" api={api} />);

    await screen.findByText('CHG-P215');
    await user.click(screen.getByText('服务请求'));

    await waitFor(() =>
      expect(api.listRecords).toHaveBeenCalledWith('service_request', expect.anything())
    );
  });
});
