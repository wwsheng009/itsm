/**
 * V0-03 变更影响分析任务卡（plan 产物，Inline 优先）。
 *
 * 口径（方案 §7 V0-03：变更影响分析任务卡（plan 产物）｜复用 plan 工具、artifact、dry-run）：
 * - 直达执行：`POST /agent/tools/execute` 调 `analyze_ci_impact_plan`（plan/只读 → 无审批；**零业务写入**）；
 * - 目标 CI：ChangeApi 的 CMDB 影响摘要给出受影响 CI 列表，一键选择；无列表时可手动输入 CI ID；
 * - 产物：artifact（analysis）在面板内直接预览（目标/检查清单/备注/artifactId），可「采纳计划」（复制，不落业务字段）；
 * - 度量（验收：计划采纳率、分析耗时）：task_started/task_completed(durationMs) + inline_preview + inline_applied(adopt)。
 * - D1-02 动作桥：页面注册 `impact_plan` handler，点击建议卡在**页内**触发本面板生成（不再发送提示词）。
 */
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, List, Space, Tag, Typography, message } from 'antd';
import { ClipboardCopy, GitBranch, RefreshCw, Sparkles } from 'lucide-react';

import { aiExecuteTool } from '@/lib/api/ai-api';
import { ChangeApi } from '@/lib/api/change-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

const { Text, Paragraph } = Typography;

export interface ChangeAIInlinePlanCardProps {
  changeId: number;
  changeTitle?: string;
  /** 由动作桥 handler 递增的非 0 值变化 → 触发一次生成（任务卡直达）。 */
  triggerNonce?: number;
}

interface PlanContent {
  target?: { type?: string; id?: number; name?: string };
  change?: string;
  summary?: string;
  checklist?: string[];
  note?: string;
}

interface PlanOutput {
  artifactId?: number;
  kind?: string;
  title?: string;
  content?: PlanContent;
  evidence?: Record<string, unknown>;
}

export const CI_IMPACT_PLAN_TOOL = 'analyze_ci_impact_plan';

export const ChangeAIInlinePlanCard: React.FC<ChangeAIInlinePlanCardProps> = ({
  changeId,
  changeTitle,
  triggerNonce,
}) => {
  const [ciOptions, setCiOptions] = useState<number[]>([]);
  const [ciId, setCiId] = useState<number | undefined>(undefined);
  const [targetsLoaded, setTargetsLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [output, setOutput] = useState<PlanOutput | null>(null);
  const handledNonceRef = useRef(0);

  // 受影响 CI 列表（best-effort：失败则保留手动输入路径）。
  useEffect(() => {
    let cancelled = false;
    const loadTargets = async () => {
      try {
        const summary = await ChangeApi.getCMDBImpactSummary(changeId);
        if (cancelled) return;
        const ids = Array.isArray(summary?.affectedCIs) ? summary.affectedCIs : [];
        setCiOptions(ids);
        if (ids.length > 0) setCiId(current => current ?? ids[0]);
      } catch {
        if (!cancelled) setCiOptions([]);
      } finally {
        if (!cancelled) setTargetsLoaded(true);
      }
    };
    void loadTargets();
    return () => {
      cancelled = true;
    };
  }, [changeId]);

  const runPlan = useCallback(async () => {
    if (!ciId || ciId <= 0) {
      message.warning('请先选择或输入目标配置项（CI）ID');
      return;
    }
    trackAssistantEvent('task_started', {
      pageKey: 'change.detail',
      targetType: 'change',
      targetId: changeId,
      taskCardId: 'impact_plan',
      toolName: CI_IMPACT_PLAN_TOOL,
    });
    setLoading(true);
    setError(null);
    const startedAt = Date.now();
    try {
      const result = await aiExecuteTool(CI_IMPACT_PLAN_TOOL, {
        ciId,
        change: changeTitle ?? '',
        summary: `变更 #${changeId} 影响分析（Inline 任务卡）`,
      });
      const planOutput = (result?.data ?? {}) as PlanOutput;
      setOutput(planOutput);
      trackAssistantEvent('task_completed', {
        pageKey: 'change.detail',
        targetType: 'change',
        targetId: changeId,
        taskCardId: 'impact_plan',
        toolName: CI_IMPACT_PLAN_TOOL,
        durationMs: Date.now() - startedAt,
        outcomeType: 'artifact',
        extra: { task: 'ci_impact_plan', artifactId: planOutput.artifactId },
      });
      trackAssistantEvent('inline_preview', {
        pageKey: 'change.detail',
        targetType: 'change',
        targetId: changeId,
        extra: { panel: 'ci_impact_plan', ref: 'agent.tools.execute' },
      });
    } catch (err) {
      const msg = (err as Error)?.message || '影响分析生成失败，请稍后重试';
      setError(msg);
      trackAssistantEvent('task_failed', {
        pageKey: 'change.detail',
        targetType: 'change',
        targetId: changeId,
        taskCardId: 'impact_plan',
        toolName: CI_IMPACT_PLAN_TOOL,
        extra: { task: 'ci_impact_plan' },
      });
      trackAssistantEvent('error_shown', {
        pageKey: 'change.detail',
        errorCode: 'ci_impact_plan_failed',
      });
    } finally {
      setLoading(false);
    }
  }, [changeId, changeTitle, ciId]);

  // 动作桥直达：triggerNonce 变化（且已完成基线加载）触发一次生成。
  useEffect(() => {
    if (!triggerNonce || triggerNonce <= 0) return;
    if (triggerNonce === handledNonceRef.current) return;
    if (!targetsLoaded) return;
    handledNonceRef.current = triggerNonce;
    void runPlan();
  }, [triggerNonce, targetsLoaded, runPlan]);

  const handleAdopt = useCallback(async () => {
    if (!output) return;
    const lines = [
      `【变更影响分析草案 #${output.artifactId ?? '-'}】`,
      output.content?.target?.name
        ? `目标 CI：#${output.content.target.id ?? ''} ${output.content.target.name}`
        : '',
      output.content?.change ? `变更：${output.content.change}` : '',
      output.content?.summary ? `摘要：${output.content.summary}` : '',
      ...(output.content?.checklist ?? []).map((item, index) => `${index + 1}. ${item}`),
      output.content?.note ?? '',
    ].filter(Boolean);
    const text = lines.join('\n');
    try {
      await navigator.clipboard?.writeText(text);
      message.success('计划已复制（采纳动作不写入业务数据）');
    } catch {
      message.warning('复制失败，请手动选择产物文本');
    }
    trackAssistantEvent('inline_applied', {
      pageKey: 'change.detail',
      targetType: 'change',
      targetId: changeId,
      toolName: CI_IMPACT_PLAN_TOOL,
      outcomeType: 'artifact',
      extra: { panel: 'ci_impact_plan', action: 'adopt', artifactId: output.artifactId },
    });
  }, [changeId, output]);

  const target = output?.content?.target;
  const checklist = output?.content?.checklist ?? [];

  return (
    <Card
      size="small"
      data-testid="change-ai-plan-card"
      className="border-indigo-100"
      title={
        <Space>
          <GitBranch className="w-4 h-4 text-indigo-600" />
          <span className="font-medium text-indigo-700">AI 变更影响分析（plan 产物）</span>
          <Text type="secondary" style={{ fontSize: 12, fontWeight: 400 }}>
            只读分析，不写业务数据
          </Text>
        </Space>
      }
    >
      <Space wrap style={{ marginBottom: 8 }}>
        <Text type="secondary" style={{ fontSize: 12 }}>
          目标 CI：
        </Text>
        {ciOptions.map(id => (
          <Tag.CheckableTag
            key={id}
            checked={ciId === id}
            onChange={() => setCiId(id)}
            data-testid={`ci-option-${id}`}
          >
            #{id}
          </Tag.CheckableTag>
        ))}
        <input
          data-testid="ci-target-input"
          type="number"
          min={1}
          value={ciId ?? ''}
          placeholder="CI ID"
          onChange={event => {
            const value = Number(event.target.value);
            setCiId(Number.isFinite(value) && value > 0 ? value : undefined);
          }}
          style={{
            width: 90,
            padding: '2px 8px',
            border: '1px solid #d9d9d9',
            borderRadius: 6,
            fontSize: 12,
          }}
        />
        <Button
          type="primary"
          ghost
          size="small"
          icon={<Sparkles className="w-3 h-3" />}
          loading={loading}
          onClick={() => void runPlan()}
          data-testid="generate-impact-plan"
        >
          生成影响分析计划
        </Button>
        {output ? (
          <Button
            size="small"
            icon={<RefreshCw className="w-3 h-3" />}
            loading={loading}
            onClick={() => void runPlan()}
            data-testid="refresh-impact-plan"
          >
            重新生成
          </Button>
        ) : null}
        <Button
          size="small"
          icon={<ClipboardCopy className="w-3 h-3" />}
          disabled={!output}
          onClick={() => void handleAdopt()}
          data-testid="adopt-impact-plan"
        >
          采纳计划
        </Button>
      </Space>

      {error ? (
        <Alert type="warning" showIcon message={error} data-testid="impact-plan-error" />
      ) : null}

      {output ? (
        <div data-testid="impact-plan-output">
          <Space size={6} wrap style={{ marginBottom: 4 }}>
            <Text strong style={{ fontSize: 13 }}>
              {output.title || '配置项变更影响分析草案'}
            </Text>
            {target?.name ? <Tag>{`目标：${target.name}`}</Tag> : null}
            {output.artifactId ? <Tag color="geekblue">{`产物 #${output.artifactId}`}</Tag> : null}
          </Space>
          {checklist.length > 0 ? (
            <List
              size="small"
              dataSource={checklist}
              renderItem={(item: string) => <List.Item>{item}</List.Item>}
            />
          ) : null}
          {output.content?.note ? (
            <Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>
              {output.content.note}
            </Paragraph>
          ) : null}
        </div>
      ) : null}
    </Card>
  );
};

export default ChangeAIInlinePlanCard;