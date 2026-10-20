/**
 * 流式回答降级文案。
 *
 * 预算中断不应表现为空白消息：把已经得到的工具摘要保留下来，并给出可执行的
 * 收敛建议。纯函数便于单测，不依赖任何 UI 组件。
 */
import type { ChatMessage } from './types';

const MAX_EVIDENCE_ITEMS = 3;

/**
 * 为 `budget_exceeded` 生成用户可读的降级正文。
 *
 * 不覆盖已有流式内容；调用方按「已有正文 + 本段」拼接。
 */
export function buildBudgetExceededFallback(
  message: Pick<ChatMessage, 'content' | 'toolEvents'>
): string {
  const evidence: string[] = [];
  const seen = new Set<string>();
  for (const event of message.toolEvents ?? []) {
    if (event.status !== 'done' || !event.summary?.trim()) continue;
    const line = `${event.tool}：${event.summary.trim()}`;
    if (seen.has(line)) continue;
    seen.add(line);
    evidence.push(line);
  }
  const retained = evidence.slice(-MAX_EVIDENCE_ITEMS);

  const lines = [
    message.content.trim()
      ? '本次回答因工具调用预算用尽而中止；以上已生成内容已保留。'
      : '本次问题因工具调用预算用尽，未能生成完整回答。',
    retained.length > 0
      ? `已查到的线索：\n${retained.map(item => `- ${item}`).join('\n')}`
      : '本轮没有可复用的检索结果。',
    '建议：\n- 把问题拆成两个更具体的请求（例如先确认工单类型，再提交或关联资产）。\n- 在请求中补充明确的工单/资产编号，避免使用“第二张”“随便找一个”等指代。\n- 可基于上面的线索继续追问，或重新发送；在 Dock 中也可点击“重试”继续。',
  ];

  return lines.join('\n\n');
}
