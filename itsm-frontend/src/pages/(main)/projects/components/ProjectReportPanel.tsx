import { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Descriptions, Select, Space, Tag, message } from 'antd';
import { Copy, Download, FileText, RefreshCw } from 'lucide-react';
import { projectService } from '@/lib/services/project-service';
import type { ProjectReport, ReportCadence, ReportTemplate } from '@/lib/services/project-service';
import { ProjectHealthTag } from './ProjectHealthTag';

const CADENCE_LABEL: Record<ReportCadence, string> = {
  weekly: '周报',
  biweekly: '双周报',
  monthly: '月报',
};

interface ProjectReportPanelProps {
  projectId: number;
}

/**
 * 项目状态报告面板（PE-17）：模板/周期数据驱动，报告由事实数据实时派生；
 * 先导出 Markdown（复制/下载），AI 增强后置 P2。
 */
export function ProjectReportPanel({ projectId }: ProjectReportPanelProps) {
  const [templates, setTemplates] = useState<ReportTemplate[]>([]);
  const [templateId, setTemplateId] = useState<number>(0);
  const [templateLoading, setTemplateLoading] = useState(false);
  const [loading, setLoading] = useState(false);
  const [report, setReport] = useState<ProjectReport | null>(null);

  const fetchTemplates = useCallback(async () => {
    setTemplateLoading(true);
    try {
      const result = await projectService.listReportTemplates({ status: 'active', pageSize: 100 });
      setTemplates(result.items);
    } catch (error) {
      console.error('Failed to fetch report templates:', error);
    } finally {
      setTemplateLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchTemplates();
  }, [fetchTemplates]);

  const generate = async () => {
    setLoading(true);
    try {
      const result = await projectService.generateProjectReport(projectId, templateId || undefined);
      setReport(result);
      message.success('报告已生成（数据实时派生，未落快照）');
    } catch (error) {
      console.error('Generate report failed:', error);
      message.error('生成报告失败');
    } finally {
      setLoading(false);
    }
  };

  const copyMarkdown = async () => {
    if (!report) return;
    try {
      await navigator.clipboard.writeText(report.markdown);
      message.success('Markdown 已复制');
    } catch (error) {
      console.error('Copy report failed:', error);
      message.error('复制失败（浏览器未授权剪贴板）');
    }
  };

  const downloadMarkdown = () => {
    if (!report) return;
    const blob = new Blob([report.markdown], { type: 'text/markdown;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `${report.data.projectCode || 'project'}-report.md`;
    link.click();
    URL.revokeObjectURL(url);
  };

  const ownerTeam = report?.data.ownerTeam;
  const variance = report?.data.variance;
  const milestoneRate = report ? Math.round(report.data.milestoneStats.rate * 1000) / 10 : 0;

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Space wrap>
        <Select
          data-testid="report-template-select"
          style={{ minWidth: 240 }}
          value={templateId}
          onChange={setTemplateId}
          loading={templateLoading}
          options={[
            { value: 0, label: '默认章节' },
            ...templates.map((tpl) => ({ value: tpl.id, label: `${tpl.name}（${CADENCE_LABEL[tpl.cadence]}）` })),
          ]}
        />
        <Button
          type="primary"
          icon={<FileText size={16} />}
          loading={loading}
          onClick={generate}
          data-testid="report-generate"
        >
          生成报告
        </Button>
        <Button icon={<RefreshCw size={16} />} loading={templateLoading} onClick={fetchTemplates}>
          刷新模板
        </Button>
        {report && (
          <>
            <Button icon={<Copy size={16} />} onClick={copyMarkdown}>
              复制 Markdown
            </Button>
            <Button icon={<Download size={16} />} onClick={downloadMarkdown} data-testid="report-download">
              下载 .md
            </Button>
          </>
        )}
      </Space>

      {!report && (
        <Alert
          type="info"
          showIcon
          message="选择报告模板（或默认章节）后生成：任务/里程碑/阶段门/基线偏差由事实数据实时派生，不落报告快照。"
        />
      )}

      {report && (
        <>
          <Descriptions size="small" bordered column={3}>
            <Descriptions.Item label="报告模板">
              {report.data.template
                ? `${report.data.template.name}（${CADENCE_LABEL[report.data.template.cadence]}）`
                : '默认章节'}
            </Descriptions.Item>
            <Descriptions.Item label="归口团队">
              {ownerTeam ? (
                <Space size={4}>
                  <span>{ownerTeam.name}</span>
                  {ownerTeam.deleted && <Tag color="error">已停用</Tag>}
                </Space>
              ) : (
                '-'
              )}
            </Descriptions.Item>
            <Descriptions.Item label="健康度">
              <ProjectHealthTag health={report.data.health} reasons={report.data.healthReasons} />
            </Descriptions.Item>
            <Descriptions.Item label="任务">
              {report.data.taskStats.total} 项（完成 {report.data.taskStats.done} / 未终结 {report.data.taskStats.open}）
            </Descriptions.Item>
            <Descriptions.Item label="任务逾期/临期">
              {report.data.taskStats.overdue} / {report.data.taskStats.dueSoon}
            </Descriptions.Item>
            <Descriptions.Item label="里程碑达成率">{milestoneRate}%</Descriptions.Item>
            <Descriptions.Item label="里程碑逾期">
              {report.data.milestoneStats.overdue} 项
            </Descriptions.Item>
            <Descriptions.Item label="计划偏差">
              {variance
                ? `基线 v${variance.baselineVersion}：变更 ${variance.changed} / 新增 ${variance.added} / 移除 ${variance.removed}`
                : '未冻结基线'}
            </Descriptions.Item>
            <Descriptions.Item label="生成时间">{report.data.generatedAt?.slice(0, 19)}</Descriptions.Item>
          </Descriptions>
          <pre
            data-testid="report-markdown"
            style={{
              margin: 0,
              padding: 16,
              background: '#fafafa',
              border: '1px solid #f0f0f0',
              borderRadius: 8,
              maxHeight: 420,
              overflow: 'auto',
              whiteSpace: 'pre-wrap',
            }}
          >
            {report.markdown}
          </pre>
        </>
      )}
    </Space>
  );
}