/**
 * V0-04 试点对照报告 Markdown 导出（复用 I2-02 的 downloadTextFile 落盘）。
 *
 * 口径全部来自服务端报告（不重算）：窗口、门槛结论、逐指标基线/本期/改善、负向指标与阈值说明。
 */
import type { PilotMetric, PilotReport } from '@/lib/api/ai-api';

const fmtValue = (metric: PilotMetric): string => {
  if (metric.unit === 'rate') return `${(metric.current * 100).toFixed(1)}%`;
  return `${Math.round(metric.current)} ms`;
};

const fmtBaseline = (metric: PilotMetric): string => {
  if (metric.unit === 'rate') return `${(metric.baseline * 100).toFixed(1)}%`;
  return `${Math.round(metric.baseline)} ms`;
};

const fmtImprovement = (metric: PilotMetric): string => {
  if (!metric.comparable) return '不可比（样本不足/基线为 0）';
  const sign = metric.improvement >= 0 ? '+' : '';
  return `${sign}${(metric.improvement * 100).toFixed(1)}%`;
};

const metricLine = (metric: PilotMetric): string =>
  `| ${metric.title} | ${metric.kind === 'primary' ? '主' : metric.kind === 'negative' ? '负向' : '次'} | ${fmtBaseline(metric)} | ${fmtValue(metric)} | ${fmtImprovement(metric)} | ${metric.comparable ? (metric.improved ? '达标' : '未达标') : '样本不足'} |`;

export function buildPilotReportMarkdown(report: PilotReport): string {
  const lines: string[] = [];
  lines.push('# 试点对照报告（V0-01～V0-03 vs 基线）');
  lines.push('');
  lines.push(`- 生成时间：${report.generatedAt}`);
  lines.push(`- 本期窗口：最近 ${report.current.days} 天（${report.current.since} ~ ${report.current.until}）`);
  lines.push(`- 基线窗口：其前 ${report.baseline.days} 天（${report.baseline.since} ~ ${report.baseline.until}）`);
  lines.push(`- 放量门槛：${report.gate.passed ? '**达标（可放量）**' : '未达标（先迭代试点，不扩面）'}（达标试点 ${report.gate.pilotsMet}/${report.gate.required}）`);
  lines.push('');
  if (report.gate.thresholds.length > 0) {
    lines.push('## 门槛口径');
    for (const threshold of report.gate.thresholds) lines.push(`- ${threshold}`);
    lines.push('');
  }
  lines.push('## 结论');
  for (const reason of report.gate.reasons) lines.push(`- ${reason}`);
  lines.push('');

  for (const section of report.sections) {
    lines.push(`## ${section.pilot} · ${section.title}（${section.met ? '达标' : '未达标'}）`);
    lines.push('');
    lines.push('| 指标 | 类型 | 基线 | 本期 | 改善 | 判定 |');
    lines.push('|---|---|---|---|---|---|');
    for (const metric of section.metrics) lines.push(metricLine(metric));
    lines.push('');
    if (section.reasons && section.reasons.length > 0) {
      for (const reason of section.reasons) lines.push(`> ${reason}`);
      lines.push('');
    }
  }

  lines.push('## 负向指标（不恶化）');
  lines.push('');
  lines.push('| 指标 | 类型 | 基线 | 本期 | 改善 | 判定 |');
  lines.push('|---|---|---|---|---|---|');
  for (const metric of report.negative) lines.push(metricLine(metric));
  lines.push('');

  if (report.notes && report.notes.length > 0) {
    lines.push('## 数据说明');
    for (const note of report.notes) lines.push(`- ${note}`);
  }
  return lines.join('\n');
}