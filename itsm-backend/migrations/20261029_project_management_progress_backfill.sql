-- 20261029_project_management_progress_backfill.sql
-- 项目管理 P1：项目进度改为任务叶子节点自动汇总后的存量回填。
--
-- 背景：P0 的 projects.progress 为手填值；P1 起由 project_tasks 派生。
-- 运行时已在任务创建/更新/流转/软删事务内重算；本迁移修复上线前的存量项目。
--
-- 口径（与 handlers/project/repository_impl.go 一致）：
--   - 仅统计未软删、非 cancelled 的叶子任务（parent_task_id 未被其他存活任务引用）；
--   - 叶子任务 progress 算术平均并四舍五入；
--   - completed 项目强制 100%（含无任务/全取消项目）；
--   - 无有效叶子任务的非 completed 项目保留原值（避免 P0 手填进度被误清零）。

WITH leaf AS (
    SELECT t.project_id,
           CASE WHEN t.status = 'done' THEN 100 ELSE t.progress END AS progress
    FROM project_tasks t
    WHERE t.deleted_at IS NULL
      AND t.status <> 'cancelled'
      AND NOT EXISTS (
          SELECT 1
          FROM project_tasks c
          WHERE c.parent_task_id = t.id
            AND c.deleted_at IS NULL
      )
),
agg AS (
    SELECT project_id, CAST(ROUND(AVG(progress)) AS INTEGER) AS progress
    FROM leaf
    GROUP BY project_id
)
UPDATE projects p
SET progress = agg.progress
FROM agg
WHERE p.id = agg.project_id
  AND p.deleted_at IS NULL
  AND p.progress IS DISTINCT FROM agg.progress;

-- 结项项目统一 100%（覆盖无任务/全部取消的边界）。
UPDATE projects p
SET progress = 100
WHERE p.deleted_at IS NULL
  AND p.status = 'completed'
  AND p.progress IS DISTINCT FROM 100;
