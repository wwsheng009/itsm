import { useNavigate } from 'react-router';

import { AdvancedReportingView } from './advanced-reporting/AdvancedReportingView';
import { useReportSummaries } from './advanced-reporting/hooks/useReportSummaries';

export default function AdvancedReporting() {
  const navigate = useNavigate();
  const state = useReportSummaries();

  return (
    <AdvancedReportingView
      reports={state.reports}
      loading={state.loading}
      error={state.error}
      onReload={state.reload}
      onOpenReport={report => navigate(report.path)}
    />
  );
}
