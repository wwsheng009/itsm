/**
 * T5-02 输入坞智能辅助菜单（`/` 快捷指令 / `@` 当前页对象引用）。
 *
 * 纯展示组件：数据与键盘导航由 `use-input-dock-assist` 提供；
 * 绝对定位于输入容器上方（父容器需 `position: relative`）。
 * 无匹配/无可引用对象时以「提示行」形态保留菜单框架（Esc 可关闭）。
 */
import React from 'react';
import { theme } from 'antd';

export type InputDockAssistMode = 'slash' | 'reference';

export interface InputDockAssistMenuItem {
  key: string;
  label: string;
  description?: string;
}

export interface InputDockAssistMenuProps {
  mode: InputDockAssistMode;
  items: InputDockAssistMenuItem[];
  activeIndex: number;
  /** items 为空时的提示文案。 */
  hint: string;
  onSelect: (index: number) => void;
  onHighlight: (index: number) => void;
}

export const INPUT_DOCK_ASSIST_MENU_TESTID: Record<InputDockAssistMode, string> = {
  slash: 'assistant-slash-menu',
  reference: 'assistant-reference-menu',
};

export const INPUT_DOCK_ASSIST_ITEM_TESTID = 'assistant-input-assist-item';
export const INPUT_DOCK_ASSIST_HINT_TESTID = 'assistant-input-assist-hint';

const MENU_TITLES: Record<InputDockAssistMode, string> = {
  slash: '快捷指令',
  reference: '引用当前页对象',
};

export const InputDockAssistMenu: React.FC<InputDockAssistMenuProps> = ({
  mode,
  items,
  activeIndex,
  hint,
  onSelect,
  onHighlight,
}) => {
  const { token } = theme.useToken();
  const title = MENU_TITLES[mode];

  return (
    <div
      role="listbox"
      aria-label={title}
      data-testid={INPUT_DOCK_ASSIST_MENU_TESTID[mode]}
      style={{
        position: 'absolute',
        left: 0,
        right: 0,
        bottom: 'calc(100% + 8px)',
        zIndex: 1050,
        padding: 4,
        borderRadius: 12,
        border: `1px solid ${token.colorBorderSecondary}`,
        background: token.colorBgElevated,
        boxShadow: token.boxShadowSecondary,
        maxHeight: 220,
        overflowY: 'auto',
      }}
    >
      <div style={{ padding: '4px 8px', fontSize: 12, color: token.colorTextTertiary }}>
        {title}
      </div>
      {items.length === 0 ? (
        <div
          data-testid={INPUT_DOCK_ASSIST_HINT_TESTID}
          style={{ padding: '6px 8px', fontSize: 13, color: token.colorTextSecondary }}
        >
          {hint}
        </div>
      ) : (
        items.map((item, index) => (
          <div
            key={item.key}
            role="option"
            aria-selected={index === activeIndex}
            data-testid={INPUT_DOCK_ASSIST_ITEM_TESTID}
            onMouseEnter={() => onHighlight(index)}
            // 避免点击时抢走文本域焦点（点击后仍需聚焦回输入框）。
            onMouseDown={event => event.preventDefault()}
            onClick={() => onSelect(index)}
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 2,
              padding: '6px 8px',
              borderRadius: 8,
              cursor: 'pointer',
              background: index === activeIndex ? token.colorFillSecondary : 'transparent',
            }}
          >
            <span style={{ fontSize: 13, color: token.colorText }}>{item.label}</span>
            {item.description ? (
              <span style={{ fontSize: 12, color: token.colorTextTertiary }}>
                {item.description}
              </span>
            ) : null}
          </div>
        ))
      )}
    </div>
  );
};

export default InputDockAssistMenu;
