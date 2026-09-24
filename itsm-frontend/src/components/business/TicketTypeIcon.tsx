
/**
 * 工单类型图标：供类型选择弹层、创建页头部摘要等复用。
 *
 * 图标 key 与后端 TicketType.icon 字段约定保持一致（见 ticket_type_dto.go 预设值），
 * 未命中时回退为 FileText。
 */

import React from 'react';
import {
  Container,
  Database,
  Download,
  Monitor,
  User,
  Code,
  Globe,
  Shield,
  Boxes,
  Folder,
  Key,
  FileText,
} from 'lucide-react';

export interface TicketTypeIconProps {
  icon?: string;
  className?: string;
}

export const TicketTypeIcon: React.FC<TicketTypeIconProps> = ({
  icon,
  className = 'w-5 h-5',
}) => {
  switch (icon) {
    case 'Container':
      return <Container className={className} />;
    case 'Database':
      return <Database className={className} />;
    case 'Download':
      return <Download className={className} />;
    case 'Desktop':
      return <Monitor className={className} />;
    case 'User':
      return <User className={className} />;
    case 'Code':
      return <Code className={className} />;
    case 'Global':
      return <Globe className={className} />;
    case 'Safety':
      return <Shield className={className} />;
    case 'Appstore':
      return <Boxes className={className} />;
    case 'Project':
      return <Folder className={className} />;
    case 'Key':
      return <Key className={className} />;
    default:
      return <FileText className={className} />;
  }
};

export default TicketTypeIcon;
