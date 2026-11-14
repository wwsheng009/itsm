
import React from 'react';
import { Breadcrumb } from 'antd';
import { Link, Outlet } from 'react-router';
import { Home } from 'lucide-react';

const TicketLayout: React.FC = () => {
  const breadcrumbItems = [
    {
      title: (
        <Link to="/">
          <Home />
        </Link>
      ),
    },
    {
      title: <Link to="/tickets">工单管理</Link>,
    },
  ];

  return (
    <div className="min-h-screen bg-gray-50">
      {/* 面包屑导航 */}
      {/* 页面级 chrome：不能使用 bg-white/surface，否则暗色下会形成第二块主背景。 */}
      <div className="page-chrome border-b">
        <div className="max-w-7xl mx-auto px-6 py-3">
          <Breadcrumb items={breadcrumbItems} />
        </div>
      </div>

      {/* 页面内容 */}
      <Outlet />
    </div>
  );
};

export default TicketLayout;
