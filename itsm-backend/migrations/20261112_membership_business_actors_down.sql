-- 20261112_membership_business_actors (down)
-- 回滚 Phase 4 第二批 A：问题 / 变更 / 服务请求的 membership 引用。
-- 先删复合外键与索引，再删列；user_id 旧列不受影响。

DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT * FROM (VALUES
            ('problems', 'fk_problems_created_by_membership_tenant'),
            ('problems', 'fk_problems_assignee_membership_tenant'),
            ('changes', 'fk_changes_created_by_membership_tenant'),
            ('changes', 'fk_changes_assignee_membership_tenant'),
            ('service_requests', 'fk_service_requests_requester_membership_tenant'),
            ('service_requests', 'fk_service_requests_processor_membership_tenant')
        ) AS v(tbl, conname)
    LOOP
        IF EXISTS (
            SELECT 1 FROM pg_constraint WHERE conname = r.conname
        ) THEN
            EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', r.tbl, r.conname);
        END IF;
    END LOOP;
END $$;

DROP INDEX IF EXISTS idx_problems_created_by_membership;
DROP INDEX IF EXISTS idx_problems_assignee_membership;
DROP INDEX IF EXISTS idx_changes_created_by_membership;
DROP INDEX IF EXISTS idx_changes_assignee_membership;
DROP INDEX IF EXISTS idx_service_requests_requester_membership;
DROP INDEX IF EXISTS idx_service_requests_processor_membership;

ALTER TABLE problems
    DROP COLUMN IF EXISTS created_by_membership_id,
    DROP COLUMN IF EXISTS assignee_membership_id;
ALTER TABLE changes
    DROP COLUMN IF EXISTS created_by_membership_id,
    DROP COLUMN IF EXISTS assignee_membership_id;
ALTER TABLE service_requests
    DROP COLUMN IF EXISTS requester_membership_id,
    DROP COLUMN IF EXISTS processor_membership_id;
