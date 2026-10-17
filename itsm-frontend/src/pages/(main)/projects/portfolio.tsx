import { PageContainer } from '@/components/common/PageContainer';
import { Button } from 'antd';
import { useNavigate } from 'react-router';
import ProjectPortfolioMetrics from './components/ProjectPortfolioMetrics';

/**
 * 项目组合视图与度量页（P2/PE-24）：聚合分布 + 四项度量 + 明细对账。
 * 权威设计：docs/project-management/plan/p2-portfolio-metrics-design.md。
 */
export default function ProjectPortfolioPage() {
  const navigate = useNavigate();
  return (
    <PageContainer
      header={{
        title: '组合视图与度量',
        breadcrumb: { items: [{ title: '首页' }, { title: '项目管理' }, { title: '组合视图' }] },
      }}
      extra={
        <Button onClick={() => navigate('/projects/reports')} data-testid="goto-reports">
          报表中心
        </Button>
      }
    >
      <ProjectPortfolioMetrics />
    </PageContainer>
  );
}