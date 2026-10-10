
import React from 'react';
import { App, Drawer, Typography, Button } from 'antd';
import { useNavigate } from 'react-router';
import { Bell, CheckCheck, ArrowRight, Ticket, AlertTriangle, Zap } from 'lucide-react';
import type { TicketNotification } from '@/lib/api/ticket-notification-api';
import { DESIGN } from '@/design-system/tokens';
import { checkNotificationTarget } from '@/lib/utils/notification-target';
import { useI18n } from '@/lib/i18n/useI18n';

const { Text, Title } = Typography;

interface NotificationDrawerProps {
  open: boolean;
  onClose: () => void;
  notifications: TicketNotification[];
  unreadCount: number;
  onMarkAsRead: (id: number) => void;
  onMarkAllAsRead: () => void;
  onViewAll: () => void;
  loading?: boolean;
  /** 标记已读写面（notification:write）；false 时隐藏「全部已读」并跳过点击自动已读。 */
  canMarkRead?: boolean;
}

export const NotificationDrawer: React.FC<NotificationDrawerProps> = ({
  open,
  onClose,
  notifications,
  unreadCount,
  onMarkAsRead,
  onMarkAllAsRead,
  onViewAll,
  canMarkRead = false,
}) => {
  const { t } = useI18n();
  const { message } = App.useApp();
  const navigate = useNavigate();

  // 点击通知：删除单据兜底（目标 404 时提示不跳转）+ 标记已读 + 按 actionUrl 直达
  //（仅允许站内路径，防开放重定向）。
  // provider 侧深链形如 /msp/workbench?ticket=<id>&customerTenantId=<tid> → 工作台自动打开详情。
  const handleItemClick = async (item: TicketNotification) => {
    const url = item.actionUrl;
    if (!url || !url.startsWith('/') || url.startsWith('//')) return;
    const targetStatus = await checkNotificationTarget(url);
    if (targetStatus === 'missing') {
      if (canMarkRead) onMarkAsRead(item.id);
      message.warning(t('notificationDrawer.targetMissing'));
      return;
    }
    if (canMarkRead) onMarkAsRead(item.id);
    onClose();
    navigate(url);
  };

  const getPriorityColor = (priority: string) => {
    switch (priority) {
      case 'urgent':
        return DESIGN.colors.danger;
      case 'high':
        return DESIGN.colors.warning;
      case 'medium':
        return DESIGN.colors.accent;
      default:
        return DESIGN.colors.textMuted;
    }
  };

  const getNotificationIcon = (type: string) => {
    switch (type) {
      case 'ticket':
        return <Ticket size={16} />;
      case 'sla':
        return <AlertTriangle size={16} />;
      default:
        return <Zap size={16} />;
    }
  };

  const getNotificationTitle = (type: string) => {
    switch (type) {
      case 'created':
        return t('notificationDrawer.typeCreated');
      case 'assigned':
        return t('notificationDrawer.typeAssigned');
      case 'status_changed':
        return t('notificationDrawer.typeStatusChanged');
      case 'commented':
        return t('notificationDrawer.typeCommented');
      case 'sla_warning':
        return t('notificationDrawer.typeSlaWarning');
      case 'resolved':
        return t('notificationDrawer.typeResolved');
      case 'closed':
        return t('notificationDrawer.typeClosed');
      default:
        return t('notificationDrawer.typeDefault');
    }
  };

  const NotificationItem = ({ item }: { item: TicketNotification }) => {
    const isRead = item.status === 'read';
    const priority =
      item.type === 'sla_warning' ? 'urgent' : item.type === 'assigned' ? 'high' : 'medium';

    return (
      <div
        onClick={() => void handleItemClick(item)}
        style={{
          padding: '16px',
          borderBottom: `1px solid ${DESIGN.colors.border}`,
          background: isRead ? 'transparent' : `${DESIGN.colors.accent}08`,
          cursor: 'pointer',
          transition: 'all 0.2s',
        }}
        className="notification-item"
      >
        <div style={{ display: 'flex', gap: 12 }}>
          <div
            style={{
              width: 40,
              height: 40,
              borderRadius: DESIGN.radius.md,
              background: `${getPriorityColor(priority)}15`,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: getPriorityColor(priority),
              flexShrink: 0,
            }}
          >
            {getNotificationIcon(item.type)}
          </div>

          <div style={{ flex: 1, minWidth: 0 }}>
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'flex-start',
                marginBottom: 4,
              }}
            >
              <Text
                strong
                style={{
                  fontSize: 14,
                  color: isRead ? DESIGN.colors.textMuted : DESIGN.colors.text,
                }}
              >
                {getNotificationTitle(item.type)}
              </Text>
              {!isRead && (
                <div
                  style={{
                    width: 8,
                    height: 8,
                    borderRadius: '50%',
                    background: DESIGN.colors.accent,
                    flexShrink: 0,
                    marginLeft: 8,
                  }}
                />
              )}
            </div>
            <Text
              style={{
                fontSize: 13,
                color: DESIGN.colors.textMuted,
                display: 'block',
                marginBottom: 4,
              }}
            >
              {item.content}
            </Text>
            <Text style={{ fontSize: 12, color: DESIGN.colors.textMuted }}>
              {item.createdAt ? new Date(item.createdAt).toLocaleString() : ''}
            </Text>
          </div>
        </div>

        <style>{`
          .notification-item:hover {
            background: ${DESIGN.colors.bgSubtle} !important;
          }
        `}</style>
      </div>
    );
  };

  return (
    <Drawer
      title={
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <div
              style={{
                width: 36,
                height: 36,
                borderRadius: DESIGN.radius.md,
                background: `linear-gradient(135deg, ${DESIGN.colors.accent} 0%, #1d4ed8 100%)`,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: '#fff',
              }}
            >
              <Bell size={18} />
            </div>
            <div>
              <Title level={5} style={{ margin: 0, fontSize: 16 }}>
                {t('notificationDrawer.title')}
              </Title>
              <Text style={{ fontSize: 12, color: DESIGN.colors.textMuted }}>
                {unreadCount > 0
                  ? t('notificationDrawer.unreadCount', { count: unreadCount })
                  : t('notificationDrawer.noUnread')}
              </Text>
            </div>
          </div>
          {unreadCount > 0 && canMarkRead && (
            <Button
              type="link"
              icon={<CheckCheck size={16} />}
              onClick={onMarkAllAsRead}
              style={{ color: DESIGN.colors.accent }}
            >
              {t('notificationDrawer.markAllRead')}
            </Button>
          )}
        </div>
      }
      placement="right"
      size="large"
      open={open}
      onClose={onClose}
      styles={{ body: { padding: 0 }, root: { zIndex: 100 } }}
    >
      <div style={{ maxHeight: 'calc(100vh - 120px)', overflow: 'auto' }}>
        {notifications.length === 0 ? (
          <div style={{ padding: '60px 20px', textAlign: 'center' }}>
            <Bell size={48} style={{ color: DESIGN.colors.border, marginBottom: 16 }} />
            <Text style={{ color: DESIGN.colors.textMuted }}>{t('notificationDrawer.noNotifications')}</Text>
          </div>
        ) : (
          notifications.map(item => <NotificationItem key={item.id} item={item} />)
        )}
      </div>

      <div
        style={{
          padding: '16px',
          borderTop: `1px solid ${DESIGN.colors.border}`,
          textAlign: 'center',
        }}
      >
        <Button type="link" onClick={onViewAll} style={{ color: DESIGN.colors.accent }}>
          {t('notificationDrawer.viewAll')} <ArrowRight size={14} />
        </Button>
      </div>
    </Drawer>
  );
};
