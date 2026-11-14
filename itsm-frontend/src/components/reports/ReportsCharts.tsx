
import React from 'react';
import {
  LineChart,
  Line,
  BarChart,
  Bar,
  PieChart,
  Pie,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  Cell,
} from 'recharts';
import { Table, Empty, Spin } from 'antd';
import type { AnalyticsDataPoint } from '@/lib/api/ticket-analytics-api';
import { useChartTheme } from '@/lib/design-system/chart-theme';

// Recharts 工具提示类型
interface CustomTooltipPayload {
  name: string;
  value: number;
  color: string;
  dataKey: string;
}

interface CustomTooltipProps {
  active?: boolean;
  payload?: CustomTooltipPayload[];
  label?: string;
}

// 饼图标签类型
interface PieChartLabelEntry {
  innerRadius: number;
  outerRadius: number;
  cx: number;
  cy: number;
  midAngle: number;
  percent?: number;
  name?: string;
  value?: number;
}

interface ReportsChartsProps {
  data: AnalyticsDataPoint[];
  loading?: boolean;
  chartType?: 'line' | 'bar' | 'pie' | 'area' | 'table';
  height?: number;
}

const ReportsCharts: React.FC<ReportsChartsProps> = ({
  data,
  loading = false,
  chartType = 'bar',
  height = 400,
}) => {
  const chart = useChartTheme();
  const COLORS = chart.palette;

  // 表格列定义
  const tableColumns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (text: string) => <span className="font-medium">{text}</span>,
    },
    {
      title: '数值',
      dataIndex: 'value',
      key: 'value',
      render: (value: number) => (
        <span className="text-blue-600 font-semibold">{value.toLocaleString()}</span>
      ),
    },
    {
      title: '数量',
      dataIndex: 'count',
      key: 'count',
      render: (count?: number) => (count ? count.toLocaleString() : '-'),
    },
    {
      title: '平均时间',
      dataIndex:'avgTime',
      key:'avgTime',
      render: (avgTime?: number) => (avgTime ? `${avgTime.toFixed(2)}小时` : '-'),
    },
  ];

  // 自定义Tooltip
  const CustomTooltip = ({ active, payload, label }: CustomTooltipProps) => {
    if (active && payload && payload.length) {
      return (
        <div
          style={{
            background: chart.tooltipBg,
            border: `1px solid ${chart.tooltipBorder}`,
            borderRadius: 8,
            boxShadow: '0 8px 24px rgba(0, 0, 0, 0.18)',
            padding: 12,
            color: chart.tooltipText,
          }}
        >
          <p className="font-medium" style={{ color: chart.tooltipText }}>{`${label}`}</p>
          {payload.map((entry, index) => (
            <p key={index} style={{ color: chart.tooltipText }}>
              {`${entry.name}: ${entry.value.toLocaleString()}`}
            </p>
          ))}
        </div>
      );
    }
    return null;
  };

  // 饼图自定义标签
  const renderCustomLabel = (entry: any) => {
    const RADIAN = Math.PI / 180;
    const radius = entry.innerRadius + (entry.outerRadius - entry.innerRadius) * 0.5;
    const x = entry.cx + radius * Math.cos(-entry.midAngle * RADIAN);
    const y = entry.cy + radius * Math.sin(-entry.midAngle * RADIAN);

    return (
      <text
        x={x}
        y={y}
        fill="white"
        className="recharts-pie-label-text text-xs font-medium"
        textAnchor={x > entry.cx ? 'start' : 'end'}
        dominantBaseline="central"
      >
        {`${entry.percent ? (entry.percent * 100).toFixed(0) : 0}%`}
      </text>
    );
  };

  if (loading) {
    return (
      <div className="flex flex-col justify-center items-center" style={{ height }}>
        <Spin size="large" />
        <span className="mt-2 text-gray-500">加载图表数据...</span>
      </div>
    );
  }

  if (!data || data.length === 0) {
    return (
      <div className="flex justify-center items-center" style={{ height }}>
        <Empty description="暂无数据" />
      </div>
    );
  }

  // 渲染图表
  const renderChart = () => {
    switch (chartType) {
      case 'line':
        return (
          <ResponsiveContainer width="100%" height={height}>
            <LineChart data={data} margin={{ top: 5, right: 30, left: 20, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke={chart.grid} />
              <XAxis
                dataKey="name"
                tick={{ fill: chart.axisText }}
                axisLine={{ stroke: chart.axisLine }}
                tickLine={{ stroke: chart.axisLine }}
              />
              <YAxis
                tick={{ fill: chart.axisText }}
                axisLine={{ stroke: chart.axisLine }}
                tickLine={{ stroke: chart.axisLine }}
              />
              <Tooltip content={<CustomTooltip />} />
              <Legend wrapperStyle={{ color: chart.legendText }} />
              <Line
                type="monotone"
                dataKey="value"
                stroke={chart.palette[0]}
                strokeWidth={2}
                dot={{ fill: chart.palette[0], r: 4 }}
                activeDot={{ r: 6 }}
              />
              {data.some(item => item.avgTime) && (
                <Line
                  type="monotone"
                  dataKey="avgTime"
                  stroke={chart.palette[1]}
                  strokeWidth={2}
                  name="平均时间(小时)"
                />
              )}
            </LineChart>
          </ResponsiveContainer>
        );

      case 'area':
        return (
          <ResponsiveContainer width="100%" height={height}>
            <AreaChart data={data} margin={{ top: 5, right: 30, left: 20, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke={chart.grid} />
              <XAxis
                dataKey="name"
                tick={{ fill: chart.axisText }}
                axisLine={{ stroke: chart.axisLine }}
                tickLine={{ stroke: chart.axisLine }}
              />
              <YAxis
                tick={{ fill: chart.axisText }}
                axisLine={{ stroke: chart.axisLine }}
                tickLine={{ stroke: chart.axisLine }}
              />
              <Tooltip content={<CustomTooltip />} />
              <Legend wrapperStyle={{ color: chart.legendText }} />
              <Area
                type="monotone"
                dataKey="value"
                stroke={chart.palette[0]}
                fill={chart.palette[0]}
                fillOpacity={0.6}
              />
            </AreaChart>
          </ResponsiveContainer>
        );

      case 'pie':
        return (
          <ResponsiveContainer width="100%" height={height}>
            <PieChart>
              <Pie
                data={data}
                cx="50%"
                cy="50%"
                labelLine={false}
                label={renderCustomLabel}
                outerRadius={120}
                fill={chart.palette[0]}
                dataKey="value"
              >
                {data.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
                ))}
              </Pie>
              <Tooltip content={<CustomTooltip />} />
              <Legend wrapperStyle={{ color: chart.legendText }} />
            </PieChart>
          </ResponsiveContainer>
        );

      case 'table':
        return (
          <Table
            columns={tableColumns}
            dataSource={data}
            scroll={{ x: 'max-content' }}
            pagination={false}
            size="middle"
            rowKey={record => record.name}
          />
        );

      default: // bar
        return (
          <ResponsiveContainer width="100%" height={height}>
            <BarChart data={data} margin={{ top: 20, right: 30, left: 20, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke={chart.grid} />
              <XAxis
                dataKey="name"
                tick={{ fill: chart.axisText }}
                axisLine={{ stroke: chart.axisLine }}
                tickLine={{ stroke: chart.axisLine }}
              />
              <YAxis
                tick={{ fill: chart.axisText }}
                axisLine={{ stroke: chart.axisLine }}
                tickLine={{ stroke: chart.axisLine }}
              />
              <Tooltip content={<CustomTooltip />} />
              <Legend wrapperStyle={{ color: chart.legendText }} />
              <Bar dataKey="value" fill={chart.palette[0]} radius={[4, 4, 0, 0]}>
                {data.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
                ))}
              </Bar>
              {data.some(item => item.avgTime) && (
                <Bar
                  dataKey="avg_time"
                  fill={chart.palette[1]}
                  name="平均时间(小时)"
                  radius={[4, 4, 0, 0]}
                />
              )}
            </BarChart>
          </ResponsiveContainer>
        );
    }
  };

  return <div className="reports-charts">{renderChart()}</div>;
};

export default ReportsCharts;
