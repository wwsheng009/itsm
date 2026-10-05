import React from 'react';
import { Popconfirm, Switch, Tooltip } from 'antd';

/**
 * 用户启停开关（共享组件，TUM-4）。
 * - 默认点击即切换（与既有 /users 页面行为一致）；
 * - 传入 `disableConfirmDescription` 时，停用前弹出 Popconfirm（后果先行）；
 * - `disabled` + `disabledReason` 时禁用并以 Tooltip 说明原因（如「最后一个管理员不可停用」）。
 */
export interface UserStatusToggleProps {
  checked: boolean;
  loading?: boolean;
  disabled?: boolean;
  disabledReason?: string;
  checkedChildren?: React.ReactNode;
  unCheckedChildren?: React.ReactNode;
  /** 停用确认描述；提供后停用需二次确认（启用仍即时） */
  disableConfirmDescription?: React.ReactNode;
  /** 切换回调：next=true 启用 / false 停用 */
  onToggle: (next: boolean) => void;
}

const UserStatusToggle: React.FC<UserStatusToggleProps> = ({
  checked,
  loading,
  disabled,
  disabledReason,
  checkedChildren,
  unCheckedChildren,
  disableConfirmDescription,
  onToggle,
}) => {
  const needsConfirm = checked && !disabled && Boolean(disableConfirmDescription);

  const switchNode = (
    <Switch
      checked={checked}
      loading={loading}
      disabled={disabled}
      checkedChildren={checkedChildren}
      unCheckedChildren={unCheckedChildren}
      onChange={next => {
        // 停用确认由 Popconfirm 接管，避免一次点击触发两次请求
        if (needsConfirm && next === false) return;
        onToggle(next);
      }}
    />
  );

  const content = needsConfirm ? (
    <Popconfirm
      title="确认停用该用户？"
      description={disableConfirmDescription}
      okText="确认停用"
      cancelText="取消"
      okButtonProps={{ danger: true }}
      onConfirm={() => onToggle(false)}
    >
      {switchNode}
    </Popconfirm>
  ) : (
    switchNode
  );

  if (disabled && disabledReason) {
    return (
      <Tooltip title={disabledReason}>
        <span className="inline-flex">{content}</span>
      </Tooltip>
    );
  }

  return content;
};

export default UserStatusToggle;
