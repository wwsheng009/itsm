-- 20260926 knowledge_articles 增加 content_type（正文类型显式化，幂等可重跑）。
--
-- 取值：text / markdown / html / rich_text；空串 = 历史数据，渲染端按内容形态兜底判定。
--
-- 背景：知识库正文长期是单字段混存——早期 Markdown、后来的富文本 HTML、
-- 导入的纯文本共用 content，落库时没有格式标记，前端只能靠 <p>/<h2>/<table>
-- 等块级标签启发式猜测。猜错的直接后果是 Markdown 文章把「## 标题」「| 表格 |」
-- 整段当正文暴露，或把 HTML 当 Markdown 转义。该列让渲染端按显式类型分发，
-- 存量数据保持空串以沿用原有兜底行为，不产生渲染回归。
--
-- 依赖 knowledge_articles 已由 ent schema 基线创建。

BEGIN;

ALTER TABLE knowledge_articles ADD COLUMN IF NOT EXISTS content_type VARCHAR(20) NOT NULL DEFAULT '';

COMMIT;
