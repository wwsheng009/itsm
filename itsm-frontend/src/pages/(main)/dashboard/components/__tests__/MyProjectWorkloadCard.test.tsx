/**
 * MyProjectWorkloadCard 测试（PE-16）：我的项目任务 + 里程碑到期聚合。
 *
 * 覆盖：并行拉取与参数、逾期文案、单侧失败局部降级、双侧失败错误态、空态。
 */
import React from 'react';
import { MemoryRouter } from 'react-router';
import { render, screen, waitFor } from '@/lib/test-utils';
import MyProjectWorkloadCard, { milestoneDueLabel } from '../MyProjectWorkloadCard';
import { projectService } from '@/lib/services/project-service';
import type { MyMilestone } from '@/lib/services/project-service';

jest.mock('@/lib/services/project-service', () => ({
  projectService: {
    myTasks: jest.fn(),
    myMilestones: jest.fn(),
  },
}));

const mockMyTasks = projectService.myTasks as jest.Mock;
const mockMyMilestones = projectService.myMilestones as jest.Mock;

const task = {
  id: 11,
  projectId: 1,
  name: '迁移任务一',
  description: '',
  status: 'in_progress',
  priority: 'high',
  progress: 10,
  sortOrder: 0,
  version: 1,
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-01T00:00:00Z',
  projectName: '迁移改造项目',
};

const milestone: MyMilestone = {
  id: 21,
  projectId: 1,
  name: '上线评审',
  description: '',
  dueDate: '2026-10-15T00:00:00Z',
  status: 'planned',
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-01T00:00:00Z',
  projectName: '迁移改造项目',
  projectCode: 'PRJ-MIG-1',
  overdue: true,
  daysLeft: -2,
};

describe('MyProjectWorkloadCard（PE-16 个人工作台聚合）', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('并行拉取我的任务与到期里程碑，并展示逾期文案', async () => {
    mockMyTasks.mockResolvedValue([task]);
    mockMyMilestones.mockResolvedValue([milestone]);

    render(
      <MemoryRouter>
        <MyProjectWorkloadCard />
      </MemoryRouter>
    );

    expect(await screen.findByText('迁移任务一')).toBeInTheDocument();
    expect(screen.getByText('上线评审')).toBeInTheDocument();
    expect(screen.getByText('逾期 2 天')).toBeInTheDocument();
    expect(screen.getByText('进行中')).toBeInTheDocument();
    expect(mockMyTasks).toHaveBeenCalledWith({ limit: 5 });
    expect(mockMyMilestones).toHaveBeenCalledWith({ dueWithinDays: 30, limit: 5 });
  });

  it('单侧失败局部降级：一侧成功仍正常展示', async () => {
    mockMyTasks.mockRejectedValue(new Error('boom'));
    mockMyMilestones.mockResolvedValue([milestone]);

    render(
      <MemoryRouter>
        <MyProjectWorkloadCard />
      </MemoryRouter>
    );

    expect(await screen.findByText('上线评审')).toBeInTheDocument();
    expect(screen.queryByText('项目待办加载失败')).not.toBeInTheDocument();
    expect(screen.getByText('暂无未完成任务')).toBeInTheDocument();
  });

  it('双侧失败显示本卡片错误态', async () => {
    mockMyTasks.mockRejectedValue(new Error('x'));
    mockMyMilestones.mockRejectedValue(new Error('y'));

    render(
      <MemoryRouter>
        <MyProjectWorkloadCard />
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText('项目待办加载失败')).toBeInTheDocument());
  });

  it('空态：无任务且无到期里程碑', async () => {
    mockMyTasks.mockResolvedValue([]);
    mockMyMilestones.mockResolvedValue([]);

    render(
      <MemoryRouter>
        <MyProjectWorkloadCard />
      </MemoryRouter>
    );

    expect(await screen.findByText('暂无项目待办')).toBeInTheDocument();
  });

  it('里程碑到期文案：今天到期 / N 天后到期', () => {
    expect(milestoneDueLabel({ ...milestone, overdue: false, daysLeft: 0 })).toBe('今天到期');
    expect(milestoneDueLabel({ ...milestone, overdue: false, daysLeft: 5 })).toBe('5 天后到期');
    expect(milestoneDueLabel({ ...milestone, overdue: true, daysLeft: -3 })).toBe('逾期 3 天');
  });
});