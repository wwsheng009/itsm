import { useNavigate } from 'react-router';
import { Result, Button } from 'antd';
import { LayoutDashboard, ArrowLeft } from 'lucide-react';

/**
 * 403 无权限页面（IP-P0-8）：分组守卫/页面级权限的统一落点，替代内联 Result 白屏。
 */
export default function Forbidden() {
  const navigate = useNavigate();

  return (
    <div className="min-h-[60vh] flex items-center justify-center">
      <Result
        status="403"
        title="403"
        subTitle="抱歉，您没有访问该页面的权限。如需访问，请联系管理员调整角色或权限。"
        extra={[
          <Button
            key="dashboard"
            type="primary"
            icon={<LayoutDashboard />}
            onClick={() => navigate('/dashboard')}
          >
            返回仪表盘
          </Button>,
          <Button key="back" icon={<ArrowLeft />} onClick={() => navigate(-1)}>
            返回上一页
          </Button>,
        ]}
      />
    </div>
  );
}
