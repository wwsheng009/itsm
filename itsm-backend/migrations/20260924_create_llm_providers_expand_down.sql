-- 20260924 回滚 llm_provider_configs / llm_user_preferences（多 LLM Provider 支持与可切换方案 BE-1）。
-- 表内无客户业务数据（仅管理员 LLM 实例配置与个人偏好），允许回滚；执行前请确认已备份。
-- DROP TABLE 会连带删除两表的 RLS 策略与索引；脚本可重复执行。

BEGIN;

DROP TABLE IF EXISTS llm_user_preferences;
DROP TABLE IF EXISTS llm_provider_configs;

COMMIT;
