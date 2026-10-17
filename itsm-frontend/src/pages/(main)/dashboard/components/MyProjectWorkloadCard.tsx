/**
 * MyProjectWorkloadCard 个人工作台「我的项目任务」聚合（PE-16）。
 *
 * 数据来源：
 * - `GET /projects/my-tasks`（跨可见项目的未终结任务，含项目名）；
 * - `GET /projects/my-milestones`（我负责的 planned 里程碑，默认 30 天内含逾期）。
 *
 * 复用 dashboard 数据获取模式：并行拉取 + 局部降级（单侧失败仍展示另一侧；
 * 两侧都失败才显示本卡片错误态），不影响仪表盘其余分区。
 */
import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router';
import { Button, Card, Empty, List, Skeleton, Space, Tag, Tooltip } from 'antd';
import { CalendarClock, ListTodo, RefreshCw } from 'lucide-react';
import { projectService, type MyMilestone, type MyProjectTask } from '@/lib/services/project-service';

const PRIORITY_COLORS: Record<string, string> = {
  low: 'default',
  medium: 'blue',
  high: 'orange',
  critical: 'red',
};

const STATUS_LABELS: Record<string, string> = {
  todo: '待办',
  in_progress: '进行中',
  blocked: '阻塞',
  done: '已完成',
  cancelled: '已取消',
};

/** 里程碑到期文案（逾期 / 今天 / N 天后）。 */
export function milestoneDueLabel(item: MyMilestone): string {
  if (item.overdue) {
    return `逾期 ${Math.abs(item.daysLeft)} 天`;
  }
  if (item.daysLeft <= 0) {
    return '今天到期';
  }
  return `${item.daysLeft} 天后到期`;
}

export const MyProjectWorkloadCard: React.FC = () => {
  const navigate = useNavigate();
  const [tasks, setTasks] = useState<MyProjectTask[]>([]);
  const [milestones, setMilestones] = useState<MyMilestone[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(false);
    const [taskResult, milestoneResult] = await Promise.allSettled([
      projectService.myTasks({ limit: 5 }),
      projectService.myMilestones({ dueWithinDays: 30, limit: 5 }),
    ]);
    if (taskResult.status === 'fulfilled') setTasks(taskResult.value);
    if (milestoneResult.status === 'fulfilled') setMilestones(milestoneResult.value);
    if (taskResult.status === 'rejected' && milestoneResult.status === 'rejected') {
      setError(true);
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const empty = tasks.length === 0 && milestones.length === 0;

  return (
    <Card
      data-testid='my-project-workload-card'
      className='rounded-xl border border-gray-100 shadow-sm'
      title={
        <span className='flex items-center gap-2'>
          <ListTodo className='w-5 h-5 text-blue-500' />
          我的项目任务
        </span>
      }
      extra={
        <Button
          size='small'
          type='text'
          aria-label='刷新项目待办'
          icon={<RefreshCw className={loading ? 'animate-spin' : ''} />}
          onClick={() => void load()}
        >
          刷新
        </Button>
      }
    >
      {loading ? (
        <Skeleton active paragraph={{ rows: 3 }} />
      ) : error ? (
        <Empty description='项目待办加载失败' image={Empty.PRESENTED_IMAGE_SIMPLE} />
      ) : empty ? (
        <Empty description='暂无项目待办' image={Empty.PRESENTED_IMAGE_SIMPLE} />
      ) : (
        <div className='grid grid-cols-1 md:grid-cols-2 gap-4'>
          <div>
            <div className='text-sm text-gray-500 mb-2 flex items-center gap-1'>
              <ListTodo className='w-4 h-4' />
              我的任务（{tasks.length}）
            </div>
            <List
              size='small'
              dataSource={tasks}
              locale={{ emptyText: '暂无未完成任务' }}
              renderItem={item => (
                <List.Item className='!px-0'>
                  <a className='truncate' onClick={() => navigate(`/projects/${item.projectId}`)}>
                    {item.name}
                  </a>
                  <Space size={4}>
                    <Tag color={PRIORITY_COLORS[item.priority] ?? 'default'}>
                      {STATUS_LABELS[item.status] ?? item.status}
                    </Tag>
                    <span className='text-xs text-gray-400'>{item.projectName}</span>
                  </Space>
                </List.Item>
              )}
            />
          </div>
          <div>
            <div className='text-sm text-gray-500 mb-2 flex items-center gap-1'>
              <CalendarClock className='w-4 h-4' />
              里程碑到期（{milestones.length}）
            </div>
            <List
              size='small'
              dataSource={milestones}
              locale={{ emptyText: '30 天内无到期里程碑' }}
              renderItem={item => (
                <List.Item className='!px-0'>
                  <a className='truncate' onClick={() => navigate(`/projects/${item.projectId}`)}>
                    {item.name}
                  </a>
                  <Tooltip title={`${item.projectName} · 计划 ${item.dueDate.slice(0, 10)}`}>
                    <Tag color={item.overdue ? 'red' : item.daysLeft <= 7 ? 'orange' : 'default'}>
                      {milestoneDueLabel(item)}
                    </Tag>
                  </Tooltip>
                </List.Item>
              )}
            />
          </div>
        </div>
      )}
    </Card>
  );
};

export default MyProjectWorkloadCard;