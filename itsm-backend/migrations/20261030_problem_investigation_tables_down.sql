-- 20261030 回滚：删除问题调查域 4 张表（含数据；子表先删，避免外键依赖）
DROP TABLE IF EXISTS problem_investigation_steps;
DROP TABLE IF EXISTS problem_solutions;
DROP TABLE IF EXISTS problem_root_cause_analyses;
DROP TABLE IF EXISTS problem_investigations;
