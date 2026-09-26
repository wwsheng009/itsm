import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, theme } from 'antd';
import { Check, Copy } from 'lucide-react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeSanitize from 'rehype-sanitize';

/**
 * AI 回答的 Markdown 渲染层（ChatGPT / DeepSeek 风格）。
 *
 * 设计要点：
 *  - 解析链与先例 `components/knowledge/ArticleDetail.tsx` 同口径：
 *    `react-markdown@9` + `remark-gfm@4`（表格/任务列表/删除线）+ `rehype-sanitize@6`（防 XSS）。
 *  - 块级代码：`<pre>` 容器 + 右上角「复制」按钮 + 语言标签（从 `language-xx` 类名解析）。
 *  - 行内 code：胶囊样式（浅底 + 圆角 + 等宽字体），由作用域内 CSS 的 `:not(pre) > code` 命中。
 *  - 列表：Tailwind preflight 会把 `ol / ul` 的 `list-style` 清成 none，必须在此显式补回
 *    （与 `styles/globals.css` 的 `.ticket-rich-text` 同口径），否则列表退化成「缩进的段落」。
 *  - 主题：取色一律走 antd `theme.useToken()`，不新增全局 CSS 文件；作用域样式以
 *    `<style>` 片段挂在 `.ai-md` 作用域下，随组件实例卸载自动移除。
 *  - 流式：`streaming` 为 true 时在内容末尾渲染自建 CSS 动画光标（`.ai-md-caret`）。
 */

type AntdToken = ReturnType<typeof theme.useToken>['token'];

const SCOPE = 'ai-md';
const MONO_FONT =
  "ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace";

/** 从 React 子节点递归抽取纯文本（复制按钮取原文 / 代码块语言解析）。 */
const extractText = (node: React.ReactNode): string => {
  if (node === null || node === undefined || typeof node === 'boolean') return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(extractText).join('');
  if (React.isValidElement(node)) {
    return extractText((node.props as { children?: React.ReactNode }).children);
  }
  return '';
};

/** 作用域样式：所有尺寸/取色集中在此，避免大面积硬编码颜色。 */
const buildScopedCss = (token: AntdToken): string => `
.${SCOPE} { color: ${token.colorText}; font-size: 14px; line-height: 1.7; word-break: break-word; overflow-wrap: anywhere; }
.${SCOPE} > *:first-child { margin-top: 0; }
.${SCOPE} > *:not(.${SCOPE}-caret):last-child { margin-bottom: 0; }
.${SCOPE} p { margin: 0 0 8px; }
.${SCOPE} h1, .${SCOPE} h2, .${SCOPE} h3, .${SCOPE} h4, .${SCOPE} h5, .${SCOPE} h6 {
  margin: 12px 0 8px; font-weight: 600; line-height: 1.45; color: ${token.colorTextHeading};
}
.${SCOPE} h1 { font-size: 20px; }
.${SCOPE} h2 { font-size: 18px; }
.${SCOPE} h3 { font-size: 16px; }
.${SCOPE} h4 { font-size: 14px; }
.${SCOPE} h5, .${SCOPE} h6 { font-size: 13px; }
.${SCOPE} ul, .${SCOPE} ol { margin: 0 0 8px; padding-left: 22px; }
/* preflight 清零了 ol/ul 的 list-style：不补回则无项目符号 / 编号。 */
.${SCOPE} ul { list-style: disc; }
.${SCOPE} ol { list-style: decimal; }
.${SCOPE} li { margin: 2px 0; }
.${SCOPE} li > p { margin: 0 0 4px; }
.${SCOPE} strong { font-weight: 600; color: ${token.colorTextHeading}; }
.${SCOPE} :not(pre) > code {
  background: ${token.colorFillTertiary}; border: 1px solid ${token.colorBorderSecondary};
  border-radius: 4px; padding: 1px 5px; font-size: 0.88em; font-family: ${MONO_FONT};
}
.${SCOPE} pre { margin: 0; background: transparent; }
.${SCOPE} pre code {
  display: block; padding: 0; border: none; background: transparent;
  font-size: 13px; line-height: 1.6; font-family: ${MONO_FONT}; white-space: pre;
}
.${SCOPE} a { color: ${token.colorLink}; text-decoration: none; }
.${SCOPE} a:hover { text-decoration: underline; }
.${SCOPE} blockquote {
  margin: 8px 0; padding: 2px 0 2px 12px;
  border-left: 3px solid ${token.colorBorder}; color: ${token.colorTextSecondary};
}
.${SCOPE} blockquote p { margin: 0 0 4px; }
.${SCOPE} hr { border: none; border-top: 1px solid ${token.colorBorderSecondary}; margin: 12px 0; }
.${SCOPE} img { max-width: 100%; }
.${SCOPE}-tablewrap { overflow-x: auto; margin: 8px 0; }
.${SCOPE} table { border-collapse: collapse; width: 100%; font-size: 13px; }
.${SCOPE} th, .${SCOPE} td {
  border: 1px solid ${token.colorBorderSecondary}; padding: 6px 10px; text-align: left; vertical-align: top;
}
.${SCOPE} th { background: ${token.colorFillTertiary}; font-weight: 600; }
.${SCOPE}-caret {
  display: inline-block; width: 2px; height: 1em; margin-left: 2px; vertical-align: -0.12em;
  background: currentColor; animation: ${SCOPE}-blink 1s steps(2, start) infinite;
}
@keyframes ${SCOPE}-blink { 0%, 49% { opacity: 1; } 50%, 100% { opacity: 0; } }
`;

/** 代码块容器：语言标签 + 复制按钮（失败静默降级）+ 横向可滚动的 `<pre>`。 */
const CodeBlock: React.FC<{
  language: string;
  code: string;
  token: AntdToken;
  children?: React.ReactNode;
}> = ({ language, code, token, children }) => {
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<number | null>(null);

  useEffect(
    () => () => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current);
    },
    []
  );

  const handleCopy = useCallback(async () => {
    try {
      // jsdom/旧浏览器可能没有 clipboard：静默降级，不打断阅读。
      if (!navigator.clipboard?.writeText) return;
      await navigator.clipboard.writeText(code);
      setCopied(true);
      if (timerRef.current !== null) window.clearTimeout(timerRef.current);
      timerRef.current = window.setTimeout(() => {
        setCopied(false);
        timerRef.current = null;
      }, 1600);
    } catch {
      // 写入被拒绝（权限/非安全上下文）：保持原样静默降级。
    }
  }, [code]);

  return (
    <div
      style={{
        margin: '10px 0',
        border: `1px solid ${token.colorBorderSecondary}`,
        borderRadius: 10,
        background: token.colorFillTertiary,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 8,
          padding: '2px 6px 2px 12px',
        }}
      >
        <span style={{ fontSize: 11, color: token.colorTextSecondary, fontFamily: MONO_FONT }}>
          {language || 'code'}
        </span>
        <Button
          type="text"
          size="small"
          aria-label="复制代码"
          icon={copied ? <Check size={12} /> : <Copy size={12} />}
          onClick={handleCopy}
          style={{ height: 22, padding: '0 6px', fontSize: 12, color: token.colorTextSecondary }}
        >
          {copied ? '已复制' : '复制'}
        </Button>
      </div>
      <pre style={{ padding: '2px 14px 12px', overflowX: 'auto' }}>{children}</pre>
    </div>
  );
};

/** 语言标签：`language-ts` → `ts`；无语言信息回退 `code`。 */
const parseLanguage = (className?: string): string => {
  const matched = /language-([\w+#.-]+)/.exec(className || '');
  return matched?.[1] || 'code';
};

export interface MarkdownMessageProps {
  content: string;
  streaming?: boolean;
  className?: string;
}

const MarkdownMessage: React.FC<MarkdownMessageProps> = ({ content, streaming, className }) => {
  const { token } = theme.useToken();
  const css = useMemo(() => buildScopedCss(token), [token]);

  const components = useMemo<Components>(
    () => ({
      // 块级代码：从已渲染的 <code className="language-xx"> 子元素上取语言与原文。
      pre: ({ children }) => {
        const first = React.Children.toArray(children)[0];
        const codeProps = React.isValidElement(first)
          ? (first.props as { className?: string; children?: React.ReactNode })
          : undefined;
        const code = extractText(codeProps?.children ?? children).replace(/\n$/, '');
        return (
          <CodeBlock language={parseLanguage(codeProps?.className)} code={code} token={token}>
            {children}
          </CodeBlock>
        );
      },
      // 外链一律新开标签页（sanitize 只放行 href，target/rel 由这里补）。
      a: ({ node: _node, ...props }) => (
        <a {...props} target="_blank" rel="noopener noreferrer" />
      ),
      // 表格：外层横向滚动容器（窄屏不撑破消息列）。
      table: ({ node: _node, ...props }) => (
        <div className={`${SCOPE}-tablewrap`}>
          <table {...props} />
        </div>
      ),
    }),
    [token]
  );

  return (
    <div className={className ? `${SCOPE} ${className}` : SCOPE}>
      <style>{css}</style>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeSanitize]}
        components={components}
      >
        {content}
      </ReactMarkdown>
      {streaming ? <span className={`${SCOPE}-caret`} aria-hidden="true" /> : null}
    </div>
  );
};

export default MarkdownMessage;
