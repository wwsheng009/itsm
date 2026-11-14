
import React from 'react';
import { Column } from '@ant-design/charts';
import { Users } from 'lucide-react';
import type { TeamWorkload } from '../types/dashboard.types';
import { DashboardChartCard } from './DashboardChartCard';
import { useChartTheme } from '@/lib/design-system/chart-theme';

const TeamWorkloadChart: React.FC<{ data: TeamWorkload[] }> = React.memo(({ data }) => {
  const chart = useChartTheme();
  const config = {
    theme: chart.isDark ? 'classicDark' : 'classic',
    data: data.map(item => ({
      assignee: item.assignee,
      ticketCount: item.ticketCount,
      completionRate: item.completionRate,
    })),
    xField: 'ticketCount',
    yField: 'assignee',
    seriesField: 'assignee',
    color: chart.palette,
    barStyle: {
      radius: [0, 8, 8, 0],
    },
    label: {
      position: 'right' as const,
      style: {
        fill: chart.axisText,
        fontSize: 12,
      },
      formatter: (datum: { ticketCount: number }) => `${datum.ticketCount}个`,
    },
    tooltip: {
      formatter: (datum: { ticketCount: number; completionRate: number }) => ({
        name: '工单数',
        value: `${datum.ticketCount}个 (完成率: ${datum.completionRate}%)`,
      }),
    },
    legend: false,
    responsive: true,
  };

  const totalTickets = data.reduce((sum, item) => sum + item.ticketCount, 0);
  const avgCompletion = data.reduce((sum, item) => sum + item.completionRate, 0) / data.length;

  return (
    <DashboardChartCard
      title="团队工作负载"
      subtitle="各成员工单处理情况"
      icon={<Users style={{ width: 20, height: 20 }} />}
      iconColor="#3b82f6"
      extra={
        <div style={{ textAlign: 'right' }}>
          <div style={{ fontSize: 12, color: 'var(--color-text-tertiary)' }}>平均完成率</div>
          <div style={{ fontSize: 16, fontWeight: 600, color: 'var(--color-primary-text)' }}>
            {avgCompletion.toFixed(1)}%
          </div>
        </div>
      }
    >
      <div style={{ height: '280px' }}>
        <Column {...config} />
      </div>
    </DashboardChartCard>
  );
});

TeamWorkloadChart.displayName = 'TeamWorkloadChart';

export default TeamWorkloadChart;
