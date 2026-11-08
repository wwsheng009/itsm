-- 客户主数据 P1 回滚（会删除关系网与项目客户链接）

DROP TABLE IF EXISTS project_customer_links;
DROP TABLE IF EXISTS customer_relationships;
DROP INDEX IF EXISTS uq_project_tenant_id;
