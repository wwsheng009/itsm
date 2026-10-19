/**
 * P2 文档命中率/解决率运营报表 Markdown 导出（报障/归档快照；复用 I2-02 下载工具）。
 */
import type { DocMetricsReport } from '@/lib/api/ai-api';

const fmtRate = (value: number | null | undefined): string =>
  typeof value === 'number' && Number.isFinite(value) ? `${(value * 100).toFixed(1)}%` : '—';

export function buildDocMetricsMarkdown(report: DocMetricsReport): string {
  const { totals, rates } = report;
  const coverage = report.coverage;
  const lines: string[] = [];

  lines.push('# 帮助文档命中率 / 解决率运营报表');
  lines.push('');
  lines.push(`- 窗口：最近 ${report.days} 天${report.generatedAt ? `（生成于 ${report.generatedAt}）` : ''}`);
  lines.push(
    `- 账本覆盖率：${fmtRate(coverage?.coverage)}（页面 ${coverage?.totals?.pages ?? 0} / 缺失 ${
      coverage?.totals?.missing ?? 0
    } / 过时 ${coverage?.totals?.stale ?? 0} / orphan ${coverage?.totals?.orphan ?? 0}）`
  );
  lines.push(`- 遥测数据面：${report.telemetry?.available ? '可用' : '未启用（仅覆盖率）'}`);
  lines.push('');
  lines.push('## 概览');
  lines.push('');
  lines.push('| 指标 | 值 |');
  lines.push('| --- | --- |');
  lines.push(`| 帮助打开（事件） | ${totals.opened} |`);
  lines.push(`| 帮助打开会话（分母） | ${totals.openedSessions} |`);
  lines.push(`| 文档命中率（未产生缺口的会话占比） | ${fmtRate(rates.hitRate)} |`);
  lines.push(`| 解决率（未升级人工率） | ${fmtRate(rates.resolveRate)} |`);
  lines.push(`| 反馈解决率（已解决 / 全部反馈） | ${fmtRate(rates.feedbackResolveRate)} |`);
  lines.push(`| 文档缺口（help_gap） | ${totals.gaps} |`);
  lines.push(`| 缺口会话 | ${totals.gapSessions} |`);
  lines.push(`| 文档草案（doc_draft_created） | ${totals.drafts} |`);
  lines.push(`| 起草成文（doc_draft_published） | ${totals.published} |`);
  lines.push(`| 起草落地率（成文 / 起草动作） | ${fmtRate(rates.draftPublishRate)} |`);
  lines.push(`| 反馈：已解决 / 未解决 | ${totals.feedbackHelpful} / ${totals.feedbackUnhelpful} |`);
  lines.push(`| 升级人工（handoff） | ${totals.handoffs} |`);
  lines.push(`| 升级人工会话 | ${totals.handoffSessions} |`);
  lines.push('');
  lines.push('## 缺口页面 Top（按缺口数降序）');
  lines.push('');
  lines.push('| pageKey | 打开 | 缺口 | 草案 | 成文 | 已解决 | 未解决 |');
  lines.push('| --- | ---: | ---: | ---: | ---: | ---: | ---: |');
  if (report.byPage.length === 0) {
    lines.push('| — | 0 | 0 | 0 | 0 | 0 | 0 |');
  } else {
    for (const row of report.byPage) {
      lines.push(
        `| ${row.pageKey} | ${row.opened} | ${row.gaps} | ${row.drafts} | ${row.published} | ${row.helpful} | ${row.unhelpful} |`
      );
    }
  }
  lines.push('');
  lines.push('## 缺口收敛趋势（按天，UTC）');
  lines.push('');
  lines.push('| 日期 | 打开 | 缺口 | 草案 | 成文 | 已解决 | 未解决 |');
  lines.push('| --- | ---: | ---: | ---: | ---: | ---: | ---: |');
  if (report.trend.length === 0) {
    lines.push('| — | 0 | 0 | 0 | 0 | 0 | 0 |');
  } else {
    for (const point of report.trend) {
      lines.push(
        `| ${point.day} | ${point.opened} | ${point.gaps} | ${point.drafts} | ${point.published} | ${point.helpful} | ${point.unhelpful} |`
      );
    }
  }
  lines.push('');
  lines.push('> 口径：命中率/解决率均为会话级（分母 = 有 help_opened 的会话）；反馈解决率以用户显式反馈为分母；无分母时显示「—」。');
  lines.push('> 起草落地率为窗口级漏斗（成文数 / AI 起草动作数），非逐篇归因；草稿在窗口末尾产生、成文落在下一窗口属已知近似。');
  lines.push('');

  return lines.join('\n');
}