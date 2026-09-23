# 附件生命周期清理演练记录（BE-8）

> 文档类型：演练记录（阶段性报告）
> 演练日期：2026-09-22
> 关联任务：`docs/plan/generic-attachment-richtext-control-plan.md` §7 BE-8（生命周期与级联清理）
> 关联配置：`docs/install.md` §3.1「生命周期清理」
> 目标读者：后端、测试、SRE

---

## 1. 范围与验收口径

BE-8 的验收标准是三条：

| # | 验收点 | 本次演练结论 |
|:--|:---|:---|
| 1 | 软删 + 过期物理文件可回收 | ✅ 本地自动化演练通过（保留期到期后物理文件与元数据行都被回收，`freed_bytes` 与 `file_size` 一致） |
| 2 | 宿主删除按策略级联且**不误删被引用文件** | ✅ 本地自动化演练通过（宿主删除 → 级联软删；仍被引用项跳过并保持 `active`；引用解除后才进入回收序列） |
| 3 | 有单测与演练记录 | ✅ 单测 10 项全绿（§3）；目标环境演练步骤与判定口径已固化（§4、§5），目标环境实操待环境窗口 |

清理语义（与方案 §5.3 一致）：删除附件只做软删（`status=deleted`，保留物理文件）；清理任务只回收「软删且 `deleted_at <= now - retention`」的记录，处置顺序为**先删物理文件、后删元数据行**，失败自动留待下轮重试；引用复核失败或命中引用一律跳过。

## 2. 环境与前置条件

| 项 | 本次（本地自动化演练） | 目标环境演练（待执行） |
|:---|:---|:---|
| 运行方式 | `go test`（enttest + sqlite3，临时目录作为存储根） | 后端服务 + PostgreSQL + 本地存储目录 |
| 数据 | 用例内构造（真实服务层 API 上传 + 事件表直写构造历史软删数据） | 脱敏副本或预发数据；**不指向生产主库** |
| 配置 | 用例内显式传参（`AttachmentCleanupOptions{TenantID, Retention, BatchSize, DryRun}`） | `attachment.cleanup_enabled=true` + `cleanup_purge_enabled=false`（先演练） |
| 日志 | `zaptest` 捕获 | 后端日志关键字 `attachment cleanup` |

> 生产库候选集核对口径见 §4.2；本地演练用 sqlite3 仅为快速回归，SQL 方言差异由既有 `./migration/...` 门禁与目标环境演练兜底。

## 3. 演练一：本地自动化演练（已完成）

命令：

```bash
cd itsm-backend
go test ./service/ -run 'TestAttachmentCleanup|TestAttachmentCascade|TestAttachmentGetHidesSoftDeletedMetadata' -count=1 -v
```

实测结果（2026-09-22，Exit code 0，`ok itsm-backend/service 7.294s`）：

```text
--- PASS: TestAttachmentCleanupPurgesExpiredUnreferenced (1.22s)   # 过期未引用 → 回收物理文件 + 元数据行
--- PASS: TestAttachmentCleanupKeepsWithinRetention (0.51s)        # 保留期内 → 不动
--- PASS: TestAttachmentCleanupSkipsStillReferenced (0.42s)        # 仍被宿主正文引用 → 跳过且保持 deleted
--- PASS: TestAttachmentCleanupDryRunKeepsEverything (0.56s)       # dry-run → 只统计，文件与行都不动
--- PASS: TestAttachmentCleanupTenantScoped (0.62s)                # 租户隔离 → 不越界清理其它租户
--- PASS: TestAttachmentCleanupToleratesMissingPhysicalFile (0.32s) # 文件已丢失 → 视为成功（幂等）
--- PASS: TestAttachmentCleanupRespectsBatchSize (0.77s)           # 分批 → 单轮只处理 BatchSize 条
--- PASS: TestAttachmentCascadeHostDeletionLifecycle (0.61s)       # 主链路：宿主删除 → 级联软删 → 引用保护 → 回收
--- PASS: TestAttachmentCascadeRejectsUnregisteredHost (0.40s)     # 未注册宿主的级联请求 → 拒绝
--- PASS: TestAttachmentGetHidesSoftDeletedMetadata (0.47s)        # A3 元数据读取与 A4 一致：软删记录 404
```

关键断言（对应 §1 三条验收）：

- **回收**：`CleanupExpired` 返回 `Purged=1` 且 `countFilesUnder(root)==0`，`Attachment` 行 `ent.IsNotFound`；`FreedBytes` 等于记录 `file_size`。
- **不误删**：用例先把附件软删、再把其 ID 写回工单正文（`data-attachment-id`），清理后记录仍在且 `status='deleted'`，结果为 `SkippedReferenced=1`；宿主删除链路中，被评论引用的 `comment_attachment` 同样跳过并保持 `active`，此时**物理文件保留**。
- **级联**：`TestAttachmentCascadeHostDeletionLifecycle` 串起「工单软删 → `CascadeHostDeletion` → 保留期到期 → `CleanupExpired` → 文件数为 0」，即宿主删除不会被清理任务绕过；未被引用的附件随宿主删除进入回收序列。

## 4. 演练二：目标环境 dry-run（发布前必做）

### 4.1 步骤

1. 修改部署配置（或环境变量），**只开任务、不开落删**：

   ```yaml
   attachment:
     cleanup_enabled: true
     cleanup_purge_enabled: false   # 演练模式：不删任何数据
     retention_days: 30
     cleanup_interval_minutes: 360
     cleanup_batch_size: 200
   ```

2. 重启后端，确认启动日志：

   ```text
   attachment cleanup task started  retention_days=30 interval_minutes=360 batch_size=200 purge_enabled=false
   ```

3. 首轮自动执行后，逐租户核对摘要日志（dry-run 下 `Scanned>0` 时必打）：

   ```text
   attachment cleanup completed  tenant_id=1 dry_run=true summary=scanned=… purged=… skipped_referenced=… failed=… freed_bytes=… dry_run=true
   ```

4. 若有 `attachment cleanup: skipped, still referenced` 告警，逐条确认告警中的 `attachment_id` / `biz_type` / `biz_id` / `usage` / `reason` 是否符合预期（被正文或评论引用）。

### 4.2 SQL 核对（PostgreSQL，只读）

```sql
-- ① 候选总量：应等于日志 scanned 之和（按租户）
SELECT tenant_id, count(*) AS candidates, coalesce(sum(file_size),0) AS bytes
FROM attachments
WHERE status = 'deleted'
  AND deleted_at IS NOT NULL
  AND deleted_at <= now() - interval '30 days'   -- 与 retention_days 对齐
GROUP BY tenant_id ORDER BY candidates DESC;

-- ② 仍被引用的候选（dry-run 清单里应体现为 skip_referenced）
SELECT a.id, a.tenant_id, a.biz_type, a.biz_id, a.usage, a.deleted_at
FROM attachments a
WHERE a.status = 'deleted'
  AND a.deleted_at <= now() - interval '30 days'
  AND a.usage = 'inline_image'
  AND EXISTS (SELECT 1 FROM tickets t WHERE t.id = a.biz_id AND t.tenant_id = a.tenant_id
                AND t.description LIKE '%data-attachment-id="' || a.id || '"%')
ORDER BY a.id LIMIT 50;

-- ③ 处置明细抽查（落删后）：对应行应已消失、文件应不存在
SELECT count(*) FROM attachments WHERE id = ANY($ids);
```

### 4.3 判定标准

- dry-run 摘要与 SQL ① 的候选量一致（允许存在「重启后新增软删」的少量漂移，需能在 `Items` 清单中逐条对上）。
- ② 中至少一条被引用候选出现在 `skip_referenced` 明细里（否则说明引用复核未生效，**阻断落删**）。
- `failed=0`；若 `failed>0`，按 `Items[].reason`（`reference_check_failed` / `storage_delete_failed` / `row_delete_failed`）定位，处置完再重跑一轮 dry-run。

## 5. 演练三：落删与回滚（dry-run 判定通过后）

1. **备份**：数据库快照 + 存储根目录快照（清理是「先删文件后删行」，物理文件删除不可逆）。
2. 开落删：`attachment.cleanup_purge_enabled: true`，重启后确认 `purge_enabled=true`，观察 `attachment cleanup completed`（`dry_run=false`）与 `Attachment purged` 明细日志。
3. 核对：SQL ③ 行数为 0、存储目录对应文件消失、`freed_bytes` 与备份中对应 `file_size` 之和一致。
4. **回滚**：把 `cleanup_enabled` 改回 `false` 并重启（任务不再注册；已软删记录不动）。若需恢复误删记录，从第 1 步快照恢复；保留期内（默认 30 天）未到期的软删记录不受清理影响。

## 6. 结论与后续

- **结论**：BE-8 的三条验收在本地自动化演练层面全部成立；dry-run 优先、引用优先、先文件后记录、单条失败不阻断等安全语义均有断言钉住。
- **阻塞问题**：无代码级阻塞。目标环境演练因当前环境无可用 PostgreSQL / 脱敏副本而未执行（与 BE-1 迁移演练同因）。
- **后续建议**：
  1. 发布前在预发或脱敏副本上执行 §4，产出真实 `summary` 日志与 SQL 对账结果，追加到本文档 §7。
  2. 首次生产落删建议把 `cleanup_batch_size` 调到较小值（如 50）分批观察，确认无异常后再回到默认 200。
  3. 观察期内关注 `attachment cleanup failed` 告警；若持续 `storage_delete_failed`，先排查存储权限/挂载，不要关闭引用复核绕过。

## 7. 目标环境演练留痕（待补）

| 日期 | 环境 | 配置 | dry-run 摘要 | SQL 对账 | 结论 | 执行人 |
|:---|:---|:---|:---|:---|:---|:---|
| — | — | — | — | — | 待执行 | — |
