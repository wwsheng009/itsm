/**
 * V0-03 变更影响分析任务卡测试：目标选择/生成产物/采纳/失败/动作桥直达。
 */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import { ChangeAIInlinePlanCard } from '../ChangeAIInlinePlanCard';
import { aiExecuteTool } from '@/lib/api/ai-api';
import { ChangeApi } from '@/lib/api/change-api';
import { trackAssistantEvent } from '@/lib/ai/telemetry';

jest.mock('@/lib/api/change-api', () => ({
  __esModule: true,
  ChangeApi: { getCMDBImpactSummary: jest.fn() },
}));
jest.mock('@/lib/api/ai-api', () => ({
  __esModule: true,
  aiExecuteTool: jest.fn(),
}));
jest.mock('@/lib/ai/telemetry', () => ({
  __esModule: true,
  trackAssistantEvent: jest.fn(),
}));

const mockedImpactSummary = ChangeApi.getCMDBImpactSummary as jest.MockedFunction<
  typeof ChangeApi.getCMDBImpactSummary
>;
const mockedExecuteTool = aiExecuteTool as jest.MockedFunction<typeof aiExecuteTool>;
const mockedTrack = trackAssistantEvent as jest.Mock;

const impactSummary = {
  changeId: 5,
  totalAffectedCIs: 2,
  criticalCICount: 1,
  highRiskDependencyCount: 0,
  openIncidentCount: 0,
  recommendedRiskLevel: 'medium',
  recommendedImpactScope: 'low',
  requiresCAB: false,
  requiresBackoutPlan: false,
  workflowHints: [],
  itilPractices: [],
  affectedCIs: [7, 9],
};

const planOutput = {
  artifactId: 31,
  kind: 'analysis',
  title: '配置项变更影响分析草案',
  content: {
    target: { type: 'ci', id: 7, name: '核心网关' },
    change: '升级网关固件',
    summary: '评估上下游依赖与回滚路径',
    checklist: ['确认上游依赖与下游被依赖服务的责任人', '确认变更窗口与回滚方案'],
    note: '分析草案由模型生成，仅作建议；具体变更评审按既有流程执行。',
  },
  evidence: { ciId: 7, ciName: '核心网关' },
};

beforeEach(() => {
  mockedImpactSummary.mockReset();
  mockedExecuteTool.mockReset();
  mockedTrack.mockReset();
  mockedImpactSummary.mockResolvedValue(impactSummary);
  mockedExecuteTool.mockResolvedValue({ status: 'success', summary: 'tool executed', data: planOutput });
});

const renderCard = (triggerNonce?: number) =>
  render(<ChangeAIInlinePlanCard changeId={5} changeTitle="升级网关固件" triggerNonce={triggerNonce} />);

describe('ChangeAIInlinePlanCard（V0-03 变更影响分析任务卡）', () => {
  it('受影响 CI 默认选中首个并生成 plan 产物（含耗时埋点）', async () => {
    renderCard();

    // 受影响 CI 列表加载 → 首个自动选中。
    await screen.findByTestId('ci-option-7');
    fireEvent.click(screen.getByTestId('generate-impact-plan'));

    const output = await screen.findByTestId('impact-plan-output');
    expect(output).toHaveTextContent('配置项变更影响分析草案');
    expect(output).toHaveTextContent('产物 #31');
    expect(output).toHaveTextContent('确认变更窗口与回滚方案');

    expect(mockedExecuteTool).toHaveBeenCalledWith(
      'analyze_ci_impact_plan',
      expect.objectContaining({ ciId: 7, change: '升级网关固件' })
    );
    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'task_completed',
        expect.objectContaining({
          taskCardId: 'impact_plan',
          toolName: 'analyze_ci_impact_plan',
          durationMs: expect.any(Number),
          outcomeType: 'artifact',
        })
      )
    );
  });

  it('无受影响 CI 时支持手动输入目标 CI', async () => {
    mockedImpactSummary.mockResolvedValue({ ...impactSummary, affectedCIs: [] });
    renderCard();

    await waitFor(() => expect(mockedImpactSummary).toHaveBeenCalledWith(5));
    fireEvent.change(screen.getByTestId('ci-target-input'), { target: { value: '99' } });
    fireEvent.click(screen.getByTestId('generate-impact-plan'));

    await waitFor(() =>
      expect(mockedExecuteTool).toHaveBeenCalledWith(
        'analyze_ci_impact_plan',
        expect.objectContaining({ ciId: 99 })
      )
    );
  });

  it('采纳计划：复制产物并记录 inline_applied(adopt)', async () => {
    renderCard();
    await screen.findByTestId('ci-option-7');
    fireEvent.click(screen.getByTestId('generate-impact-plan'));
    await screen.findByTestId('impact-plan-output');

    fireEvent.click(screen.getByTestId('adopt-impact-plan'));

    await waitFor(() =>
      expect(mockedTrack).toHaveBeenCalledWith(
        'inline_applied',
        expect.objectContaining({
          outcomeType: 'artifact',
          extra: expect.objectContaining({ panel: 'ci_impact_plan', action: 'adopt', artifactId: 31 }),
        })
      )
    );
  });

  it('生成失败：可见错误 + task_failed/error_shown', async () => {
    mockedExecuteTool.mockRejectedValue(new Error('工具执行失败'));
    renderCard();
    await screen.findByTestId('ci-option-7');

    fireEvent.click(screen.getByTestId('generate-impact-plan'));

    expect(await screen.findByTestId('impact-plan-error')).toHaveTextContent('工具执行失败');
    expect(mockedTrack).toHaveBeenCalledWith('task_failed', expect.objectContaining({ toolName: 'analyze_ci_impact_plan' }));
    expect(mockedTrack).toHaveBeenCalledWith(
      'error_shown',
      expect.objectContaining({ errorCode: 'ci_impact_plan_failed' })
    );
  });

  it('动作桥直达：triggerNonce 变化自动生成', async () => {
    const { rerender } = render(
      <ChangeAIInlinePlanCard changeId={5} changeTitle="升级网关固件" triggerNonce={0} />
    );
    await screen.findByTestId('ci-option-7');

    rerender(<ChangeAIInlinePlanCard changeId={5} changeTitle="升级网关固件" triggerNonce={1} />);

    await waitFor(() => expect(mockedExecuteTool).toHaveBeenCalledTimes(1));
    expect(await screen.findByTestId('impact-plan-output')).toBeInTheDocument();
  });
});