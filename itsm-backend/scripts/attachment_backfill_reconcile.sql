-- 附件回填对账（P2/DO-1 对账工具，只读；方案 §5.2 + §6.1）
-- 用法：psql -U itsm -d itsm_prod -f scripts/attachment_backfill_reconcile.sql
--   期望：missing_in_attachments = 0 且 field_mismatch = 0 且 id_collision_risk = OK。
--   本脚本不修改任何数据（全部为 SELECT），可随时在生产/开发库重跑。
-- 背景：保 ID 平移是硬要求（历史 data-attachment-id 与 /tickets/:id/attachments/:ref 引用旧 ID）。

WITH legacy AS (
    SELECT id, tenant_id, ticket_id, file_name, file_path, file_url,
           file_size, file_type, mime_type, uploaded_by, created_at
    FROM ticket_attachments
), generic AS (
    SELECT id, tenant_id, biz_type, biz_id, usage, file_name, file_path, file_url,
           file_size, file_type, mime_type, uploaded_by, created_at
    FROM attachments
), checks AS (
    SELECT 'legacy_total' AS metric,
           (SELECT COUNT(*) FROM legacy)::text AS value
    UNION ALL
    -- 历史行在通用表缺失 → 未回填（或按 id 撞车被 DO NOTHING 跳过，需人工确认）
    SELECT 'missing_in_attachments',
           (SELECT COUNT(*) FROM legacy l
             LEFT JOIN generic g ON g.id = l.id AND g.biz_type = 'ticket'
            WHERE g.id IS NULL)::text
    UNION ALL
    -- 同名同义列逐字段比对：任何不等都说明平移过程改写过数据
    SELECT 'field_mismatch',
           (SELECT COUNT(*) FROM legacy l
              JOIN generic g ON g.id = l.id AND g.biz_type = 'ticket'
             WHERE g.biz_id      <> l.ticket_id
                OR g.tenant_id   <> l.tenant_id
                OR g.file_name   <> l.file_name
                OR g.file_path   <> l.file_path
                OR COALESCE(g.file_url, '')  <> COALESCE(l.file_url, '')
                OR g.file_size   <> l.file_size
                OR g.file_type   <> l.file_type
                OR COALESCE(g.mime_type, '') <> COALESCE(l.mime_type, '')
                OR g.uploaded_by <> l.uploaded_by
                OR g.created_at  <> l.created_at
                OR g.usage      <> 'attachment')::text
    UNION ALL
    -- 通用表里 ticket 域的存量（应 >= legacy_total；多出来的是新链路写入）
    SELECT 'attachments_ticket_total',
           (SELECT COUNT(*) FROM generic WHERE biz_type = 'ticket')::text
    UNION ALL
    -- 新链路软删行（status<>'active'）：归一化统计，便于判断清理任务是否有旧数据
    SELECT 'attachments_ticket_inactive',
           (SELECT COUNT(*) FROM attachments WHERE biz_type = 'ticket' AND status <> 'active')::text
    UNION ALL
    -- 序列水位（序列未被调用过时为 NULL）——人工复核用
    SELECT 'sequence_last_value',
           COALESCE((SELECT last_value::text FROM pg_sequences
                      WHERE schemaname = 'public'
                        AND sequencename = (SELECT substring(pg_get_serial_sequence('attachments', 'id') from '[^.]*$'))),
                    'NULL(未调用)')
    UNION ALL
    -- 序列水位必须 >= MAX(id)，否则下一次 nextval 会撞主键（回填后最常见的坑）。
    -- 注意：pg_sequences 不暴露 is_called；需要逐位复核时请直接读序列关系
    -- （`SELECT last_value, is_called FROM public.attachments_id_seq`），
    -- 回填迁移已按「表内有行 → is_called=true」对齐。
    SELECT 'id_collision_risk',
           CASE
               WHEN COALESCE((SELECT last_value FROM pg_sequences
                               WHERE schemaname = 'public'
                                 AND sequencename = (SELECT substring(pg_get_serial_sequence('attachments', 'id') from '[^.]*$'))), 0)
                    >= (SELECT COALESCE(MAX(id), 0) FROM attachments)
               THEN 'OK'
               ELSE 'FAIL: sequence below max(id)'
           END
)
SELECT metric, value FROM checks ORDER BY metric;
