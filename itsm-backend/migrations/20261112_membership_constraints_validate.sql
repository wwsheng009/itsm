-- 20261112_membership_constraints_validate
-- C 方案 Phase 4 第二批 B：把 membership 复合外键从 NOT VALID 收敛为 VALID。
--
-- 策略：
--   1. 约束缺失时先按既有口径补建（NOT VALID），保证 VALIDATE 有目标；
--   2. 对同租户不匹配的 membership 引用置 NULL（保留 user_id 旧列，读路径回退）；
--      只有“引用不存在 / 指向其他租户”的脏指针会被清理，合法历史行不受影响；
--   3. 逐个 VALIDATE CONSTRAINT，失败会显式报错而不是静默跳过；
--   4. 输出每张表的清理计数，便于发布后巡检。
--
-- 注意：本迁移不修改 audit_logs 的证据字段（action/actor/tenant），
-- 仅清理无法通过租户一致性校验的 membership 指针。

DO $$
DECLARE
    r record;
    cleaned bigint;
    total_cleaned bigint := 0;
BEGIN
    FOR r IN
        SELECT * FROM (VALUES
            -- 工单域（20261111_membership_tenant_identity / actor_references）
            ('tickets', 'fk_tickets_requester_membership_tenant', 'requester_membership_id'),
            ('tickets', 'fk_tickets_assignee_membership_tenant', 'assignee_membership_id'),
            ('tickets', 'fk_tickets_created_by_membership_tenant', 'created_by_membership_id'),
            ('ticket_comments', 'fk_ticket_comments_user_membership_tenant', 'user_membership_id'),
            ('ticket_attachments', 'fk_ticket_attachments_uploader_membership_tenant', 'uploaded_by_membership_id'),
            ('attachments', 'fk_attachments_uploader_membership_tenant', 'uploaded_by_membership_id'),
            ('ticket_ccs', 'fk_ticket_ccs_user_membership_tenant', 'user_membership_id'),
            ('ticket_ccs', 'fk_ticket_ccs_added_by_membership_tenant', 'added_by_membership_id'),
            ('ticket_approvals', 'fk_ticket_approvals_approver_membership_tenant', 'approver_membership_id'),
            ('ticket_approvals', 'fk_ticket_approvals_delegate_membership_tenant', 'delegate_to_membership_id'),
            ('approval_records', 'fk_approval_records_approver_membership_tenant', 'approver_membership_id'),
            ('notifications', 'fk_notifications_user_membership_tenant', 'user_membership_id'),
            ('ticket_notifications', 'fk_ticket_notifications_user_membership_tenant', 'user_membership_id'),
            ('process_approval_decisions', 'fk_process_approval_decisions_actor_membership_tenant', 'actor_membership_id'),
            ('audit_logs', 'fk_audit_logs_membership_tenant', 'membership_id'),
            -- 问题 / 变更 / 服务请求（20261112_membership_business_actors）
            ('problems', 'fk_problems_created_by_membership_tenant', 'created_by_membership_id'),
            ('problems', 'fk_problems_assignee_membership_tenant', 'assignee_membership_id'),
            ('changes', 'fk_changes_created_by_membership_tenant', 'created_by_membership_id'),
            ('changes', 'fk_changes_assignee_membership_tenant', 'assignee_membership_id'),
            ('service_requests', 'fk_service_requests_requester_membership_tenant', 'requester_membership_id'),
            ('service_requests', 'fk_service_requests_processor_membership_tenant', 'processor_membership_id')
        ) AS v(tbl, conname, colname)
    LOOP
        IF NOT EXISTS (
            SELECT 1 FROM pg_constraint WHERE conname = r.conname
        ) THEN
            EXECUTE format(
                'ALTER TABLE %I ADD CONSTRAINT %I FOREIGN KEY (%I, tenant_id) REFERENCES user_tenant_memberships (id, tenant_id) NOT VALID',
                r.tbl, r.conname, r.colname
            );
        END IF;

        EXECUTE format(
            'UPDATE %I t SET %I = NULL ' ||
            'WHERE t.%I IS NOT NULL ' ||
            '  AND NOT EXISTS (' ||
            '      SELECT 1 FROM user_tenant_memberships m ' ||
            '      WHERE m.id = t.%I AND m.tenant_id = t.tenant_id' ||
            '  )',
            r.tbl, r.colname, r.colname, r.colname
        );
        GET DIAGNOSTICS cleaned = ROW_COUNT;
        IF cleaned > 0 THEN
            total_cleaned := total_cleaned + cleaned;
            RAISE NOTICE 'membership constraint cleanup: table=% column=% invalid_rows=%',
                r.tbl, r.colname, cleaned;
        END IF;

        EXECUTE format('ALTER TABLE %I VALIDATE CONSTRAINT %I', r.tbl, r.conname);
    END LOOP;

    RAISE NOTICE 'membership constraints validated: total_invalid_rows_cleaned=%', total_cleaned;
END $$;
