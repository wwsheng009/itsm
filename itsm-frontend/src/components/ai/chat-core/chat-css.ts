/**
 * D0-01 chat-core：聊天作用域样式（由 AIChat.tsx 逐字搬运；页面与 Dock 共用）。
 */
import { theme } from 'antd';

type AntdToken = ReturnType<typeof theme.useToken>['token'];

/** 会话列表 / 消息操作区的少量 hover 反馈（作用域卡在本组件内，不动全局样式）。 */
export const buildChatCss = (token: AntdToken): string => `
.ai-chat-conv { border-radius: 8px; transition: background-color .15s ease; }
.ai-chat-conv:hover { background: ${token.colorFillTertiary}; }
.ai-chat-conv[data-selected='true'] { background: ${token.colorPrimaryBg}; }
.ai-chat-actions { opacity: .5; transition: opacity .15s ease; }
.ai-chat-item:hover .ai-chat-actions, .ai-chat-actions:focus-within { opacity: 1; }
.ai-chat-suggest { text-align: left; height: auto; white-space: normal; padding: 12px 14px; }
.ai-chat-suggest:hover { border-color: ${token.colorPrimary}; color: ${token.colorPrimary}; }

/* 全屏锁生效期间（见 lockChatPageLayout）：内容区不留底部内边距、隐藏全局页脚。
   两者合计 64px，会让聊天卡片与视口下边缘之间残留一条空白带。
   这里用作用域 CSS 而不是内联样式：MainLayout 会在断点切换时重写 .main-content 的
   padding 简写，内联覆盖会被 React 的重渲染清掉；CSS 规则不会。 */
html[data-ai-chat] #main-content > .main-content { padding-bottom: 0 !important; }
html[data-ai-chat] #main-content ~ footer { display: none !important; }
`;
