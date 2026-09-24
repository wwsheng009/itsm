
/**
 * SLA Dashboard page - redirects to /sla for now
 * The actual SLA dashboard is at /sla
 */

import { Navigate } from 'react-router';

export default function SLADashboardPage() {
  return <Navigate to="/sla" replace />;
}
