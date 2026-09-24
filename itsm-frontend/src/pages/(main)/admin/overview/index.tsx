
/**
 * Admin Overview Page - redirects to /admin
 * This page exists to handle the /admin/overview route defined in sidebar menu
 */

import { Navigate } from 'react-router';

export default function AdminOverviewPage() {
  return <Navigate to="/admin" replace />;
}
