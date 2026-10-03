-- IP-P2-3 收尾（2026-10-03）：messages.tenant_id 置 NOT NULL（单独迁移）。
--
-- 前置：scripts/msp/verify-messages-tenant-backfill.sql 三节归零（联调库 2026-10-03
-- 实测 ①=0 ②=0 ③=0）；写入侧已收口（handlers/ai/repository_impl.go：ctx → 会话派生，
-- 冲突/缺失 fail-closed）。
--
-- 幂等：先再回填（会话可派生的行），仍存在空值则显式报错阻断——不带病收紧。

UPDATE messages m
SET tenant_id = c.tenant_id
FROM conversations c
WHERE c.id = m.conversation_id
  AND m.tenant_id IS NULL
  AND c.tenant_id IS NOT NULL;

DO $$
DECLARE
  null_rows bigint;
BEGIN
  SELECT count(*) INTO null_rows FROM messages WHERE tenant_id IS NULL;
  IF null_rows > 0 THEN
    RAISE EXCEPTION 'messages.tenant_id 仍有 % 行空值：请先人工处置（会话无租户/游离消息）后重跑本迁移', null_rows;
  END IF;
END $$;

ALTER TABLE messages ALTER COLUMN tenant_id SET NOT NULL;
