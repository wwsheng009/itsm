-- 回滚：删除模板分发来源追踪表（仅本批新增表；不影响 workflow_templates 既有数据）。
DROP TABLE IF EXISTS workflow_template_distributions;
