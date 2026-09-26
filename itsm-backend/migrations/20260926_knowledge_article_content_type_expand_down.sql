-- 20260926 knowledge_articles.content_type 回滚（幂等）。
-- 回滚后正文类型退化为按内容形态兜底判定，渲染行为与引入该列之前一致。

BEGIN;

ALTER TABLE knowledge_articles DROP COLUMN IF EXISTS content_type;

COMMIT;
