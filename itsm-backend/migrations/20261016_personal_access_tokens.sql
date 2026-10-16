-- 20261016 个人访问令牌（PAT）：
--   1) audit_logs 增加 token_id 归因列（列由 ent Schema.Create 先行创建，本文件为幂等兜底）
--   2) 索引：令牌来源过滤与按令牌检索（audit_logs 此前无 (tenant_id, source) 索引，
--      source 过滤会退化为扫描，见方案 R-8 / 实施文档 T2）
-- 幂等范式照抄 migrations/20261014f_ticket_type_distribution.sql。
DO $$
BEGIN
  IF to_regclass('public.audit_logs') IS NULL THEN
    RAISE NOTICE 'skip 20261016: audit_logs not found';
  ELSE
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name = 'audit_logs' AND column_name = 'token_id') THEN
      ALTER TABLE audit_logs ADD COLUMN token_id INTEGER;
      RAISE NOTICE '20261016: audit_logs.token_id added';
    ELSE
      RAISE NOTICE '20261016: audit_logs.token_id already exists';
    END IF;
    CREATE INDEX IF NOT EXISTS idx_audit_token ON audit_logs (tenant_id, token_id, created_at);
    CREATE INDEX IF NOT EXISTS idx_audit_source_created ON audit_logs (tenant_id, source, created_at);
    RAISE NOTICE '20261016: audit indexes ensured';
  END IF;
END $$;
