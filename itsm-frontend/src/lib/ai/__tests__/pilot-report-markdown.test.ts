/**
 * V0-04 试点对照报告 Markdown 导出单测（口径全部来自服务端报告）。
 */
import { buildPilotReportMarkdown } from '../pilot-report-markdown';
import type { PilotReport } from '@/lib/api/ai-api';

const report: PilotReport = {
  tenantId: 1,
  current: { days: 7, since: '2026-10-11T12:00:00Z', until: '2026-10-18T12:00:00Z' },
  baseline: { days: 7, since: '2026-10-04T12:00:00Z', until: '2026-10-11T12:00:00Z' },
  generatedAt: '2026-10-18T12:00:00Z',
  sections: [
    {
      key: 'ticket_inline',
      pilot: 'V0-01',
      title: '工单 Inline 处理',
      met: true,
      reasons: ['达标：主指标相对基线改善 ≥30%'],
      metrics: [
        {
          key: 'inline_adoption_rate',
          title: 'Inline 采纳率',
          unit: 'rate',
          direction: 'up',
          kind: 'primary',
          baseline: 0.5,
          current: 0.8,
          delta: 0.3,
          improvement: 0.6,
          comparable: true,
          improved: true,
          sample: 100,
        },
      ],
    },
    {
      key: 'change_impact_plan',
      pilot: 'V0-03',
      title: '变更影响分析',
      met: false,
      reasons: ['计划采纳率：样本不足'],
      metrics: [
        {
          key: 'plan_adoption_rate',
          title: '计划采纳率',
          unit: 'rate',
          direction: 'up',
          kind: 'primary',
          baseline: 0,
          current: 0,
          delta: 0,
          improvement: 0,
          comparable: false,
          improved: false,
          sample: 0,
        },
      ],
    },
  ],
  negative: [
    {
      key: 'suggestion_ignore_rate',
      title: '建议忽略率',
      unit: 'rate',
      direction: 'down',
      kind: 'negative',
      baseline: 0.3,
      current: 0.2,
      delta: -0.1,
      improvement: 0.333,
      comparable: true,
      improved: true,
      sample: 100,
    },
  ],
  gate: {
    passed: false,
    pilotsMet: 1,
    required: 2,
    reasons: ['达标试点 1/3 < 2：未达放量门槛'],
    thresholds: ['§11.5：≥2 项试点相对基线改善 ≥30%', '§11.5：负向指标不恶化'],
  },
  notes: ['本期窗口无埋点样本：请确认试点已上线'],
};

describe('buildPilotReportMarkdown（V0-04）', () => {
  it('包含窗口/门槛结论/逐段指标/负向指标/阈值与说明', () => {
    const markdown = buildPilotReportMarkdown(report);

    expect(markdown).toContain('# 试点对照报告（V0-01～V0-03 vs 基线）');
    expect(markdown).toContain('本期窗口：最近 7 天');
    expect(markdown).toContain('基线窗口：其前 7 天');
    expect(markdown).toContain('未达标（先迭代试点，不扩面）');
    expect(markdown).toContain('达标试点 1/2');
    // 指标表：基线/本期/改善（带符号）/判定。
    expect(markdown).toContain('| Inline 采纳率 | 主 | 50.0% | 80.0% | +60.0% | 达标 |');
    expect(markdown).toContain('| 计划采纳率 | 主 | 0.0% | 0.0% | 不可比（样本不足/基线为 0） | 样本不足 |');
    // 负向指标段与阈值、说明。
    expect(markdown).toContain('## 负向指标（不恶化）');
    expect(markdown).toContain('| 建议忽略率 | 负向 | 30.0% | 20.0% | +33.3% | 达标 |');
    expect(markdown).toContain('## 门槛口径');
    expect(markdown).toContain('§11.5：≥2 项试点相对基线改善 ≥30%');
    expect(markdown).toContain('## 数据说明');
    expect(markdown).toContain('本期窗口无埋点样本');
  });

  it('时延指标按 ms 呈现（四舍五入）', () => {
    const withLatency = {
      ...report,
      sections: [
        {
          ...report.sections[0],
          metrics: [
            {
              ...report.sections[0].metrics[0],
              key: 'ai_create_latency_p95_ms',
              title: '建单时长 P95',
              unit: 'ms' as const,
              baseline: 30000.4,
              current: 12000.6,
              improvement: 0.6,
            },
          ],
        },
      ],
    };
    const markdown = buildPilotReportMarkdown(withLatency as PilotReport);
    expect(markdown).toContain('| 建单时长 P95 | 主 | 30000 ms | 12001 ms | +60.0% | 达标 |');
  });
});