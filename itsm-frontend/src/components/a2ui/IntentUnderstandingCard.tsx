/**
 * V1-03 口语↔术语确认卡：「我理解成：网络故障（高）→ 确认 / 修改」。
 *
 * 口径（方案 §7 V1-03 / §11.4）：
 * - AI 先把员工口语映射为可核对的 ITSM 术语（分类/优先级），并展示证据片段
 *   （「连不上网」→ 网络故障、「很急」→ 高）与未识别降级提示；
 * - **确认或修改后才进入建单表单生成**；用户修正的值直接作为表单初值，
 *   写边界不变式不变（表单提交前用户仍可继续修改，绝不自动提交）；
 * - 埋点复用既有事件字典（零新增）：`confirmation_shown` / `confirmation_accepted` /
 *   `confirmation_rejected`（reasonCode=modify）+ 字段纠正 `ai_create_field_corrected`
 *   （与 V0-02 误分类率代理同口径，card 级纠正在此计入）。
 */
import React, { useEffect, useState } from 'react';
import { Button, Card, Select, Space, Tag, Typography } from 'antd';
import { Sparkles } from 'lucide-react';
import type { TicketIntentInterpretation } from '@/lib/api/ai-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

/** 任务卡标识（埋点 taskCardId；与 A2UI 建单的 ai_create_ticket 区分）。 */
export const INTENT_INTERPRETATION_TASK_ID = 'intent_interpretation';

const TYPE_OPTIONS = [
  { value: 'hardware', label: '硬件故障' },
  { value: 'software', label: '软件问题' },
  { value: 'network', label: '网络故障' },
  { value: 'access', label: '账号权限' },
  { value: 'other', label: '其他' },
];

const PRIORITY_OPTIONS = [
  { value: 'low', label: '低' },
  { value: 'medium', label: '中' },
  { value: 'high', label: '高' },
  { value: 'urgent', label: '紧急' },
];

export interface IntentUnderstandingChanges {
  type?: string;
  priority?: string;
}

export interface IntentUnderstandingCardProps {
  /** 后端理解结果（POST /ai/tickets/interpret）。 */
  interpretation: TicketIntentInterpretation;
  /** 用户确认原样理解 → 继续生成建单表单。 */
  onConfirm: () => void;
  /** 用户修改后再继续（仅回传发生变化的字段；无变化等价确认）。 */
  onModify?: (changes: IntentUnderstandingChanges) => void;
  /** 埋点页面键。 */
  pageKey?: string;
  /** 生成中禁用交互。 */
  loading?: boolean;
}

export function IntentUnderstandingCard({
  interpretation,
  onConfirm,
  onModify,
  pageKey,
  loading = false,
}: IntentUnderstandingCardProps) {
  const [editing, setEditing] = useState(false);
  const [draftType, setDraftType] = useState(interpretation.fields.type);
  const [draftPriority, setDraftPriority] = useState(interpretation.fields.priority);

  // 新一次理解（组件 key 变化或 props 更新）时同步草稿值。
  useEffect(() => {
    setDraftType(interpretation.fields.type);
    setDraftPriority(interpretation.fields.priority);
    setEditing(false);
  }, [interpretation]);

  // 卡片出现即上报一次（展示口径：确认卡曝光）。
  useEffect(() => {
    trackAssistantEvent('confirmation_shown', {
      pageKey,
      taskCardId: INTENT_INTERPRETATION_TASK_ID,
      extra: { kind: 'interpretation', matched: interpretation.matched },
    });
  }, [pageKey, interpretation]);

  const handleConfirm = () => {
    trackAssistantEvent('confirmation_accepted', {
      pageKey,
      taskCardId: INTENT_INTERPRETATION_TASK_ID,
      extra: { kind: 'interpretation' },
    });
    onConfirm();
  };

  const handleStartModify = () => {
    trackAssistantEvent('confirmation_rejected', {
      pageKey,
      taskCardId: INTENT_INTERPRETATION_TASK_ID,
      reasonCode: 'modify',
      extra: { kind: 'interpretation' },
    });
    setEditing(true);
  };

  const handleSubmitModify = () => {
    const changes: IntentUnderstandingChanges = {};
    if (draftType !== interpretation.fields.type) changes.type = draftType;
    if (draftPriority !== interpretation.fields.priority) changes.priority = draftPriority;

    (Object.keys(changes) as Array<'type' | 'priority'>).forEach(field => {
      trackAssistantEvent('ai_create_field_corrected', {
        pageKey,
        taskCardId: INTENT_INTERPRETATION_TASK_ID,
        field,
        extra: { from: interpretation.fields[field], to: changes[field] },
      });
    });

    if (Object.keys(changes).length === 0) {
      handleConfirm();
      return;
    }
    if (onModify) {
      onModify(changes);
    } else {
      onConfirm();
    }
  };

  return (
    <Card
      size="small"
      className="my-3 border-blue-200 bg-blue-50/40"
      data-testid="intent-understanding-card"
    >
      <div className="flex items-start gap-2">
        <Sparkles className="w-4 h-4 text-blue-500 mt-1 shrink-0" />
        <div className="flex-1 min-w-0">
          <div className="text-sm text-gray-700">
            {interpretation.matched ? '我理解成：' : '没识别到明确术语，先按默认理解：'}
            <Typography.Text strong>{interpretation.summary}</Typography.Text>
          </div>

          {interpretation.terms.length > 0 ? (
            <div className="mt-2 flex flex-wrap gap-1">
              {interpretation.terms.map(term => (
                <Tag key={term.field} color="blue" className="m-0">
                  {`「${term.surface}」→ ${term.term}`}
                </Tag>
              ))}
            </div>
          ) : null}

          {editing ? (
            <div className="mt-3">
              <Space size="small" wrap>
                <span className="text-gray-500 text-sm">分类</span>
                <Select
                  size="small"
                  style={{ width: 140 }}
                  value={draftType}
                  onChange={setDraftType}
                  options={TYPE_OPTIONS}
                  aria-label="分类"
                />
                <span className="text-gray-500 text-sm">优先级</span>
                <Select
                  size="small"
                  style={{ width: 110 }}
                  value={draftPriority}
                  onChange={setDraftPriority}
                  options={PRIORITY_OPTIONS}
                  aria-label="优先级"
                />
              </Space>
              <Space size="small" className="mt-2">
                <Button type="primary" size="small" onClick={handleSubmitModify} loading={loading}>
                  继续
                </Button>
                <Button
                  size="small"
                  type="text"
                  onClick={() => setEditing(false)}
                  disabled={loading}
                >
                  返回
                </Button>
              </Space>
            </div>
          ) : (
            <Space size="small" className="mt-3">
              <Button type="primary" size="small" onClick={handleConfirm} loading={loading}>
                确认
              </Button>
              <Button size="small" onClick={handleStartModify} disabled={loading}>
                修改
              </Button>
            </Space>
          )}

          <div className="text-xs text-gray-400 mt-2">
            确认或修改后才会生成建单表单，提交前仍可继续修改；不会自动提交。
          </div>
        </div>
      </div>
    </Card>
  );
}

export default IntentUnderstandingCard;