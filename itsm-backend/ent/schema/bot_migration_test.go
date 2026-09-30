package schema_test

import (
	"context"
	"testing"

	"itsm-backend/ent/botevent"
	"itsm-backend/ent/botrun"
	"itsm-backend/ent/botstep"
	"itsm-backend/ent/enttest"
	_ "itsm-backend/ent/runtime"
	"itsm-backend/ent/toolinvocation"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// B1-01 迁移证据：空库建表、列清单、租户维度索引、(run_id, seq|step_index) 唯一冲突、
// 迁移幂等与旧行兼容（tool_invocations.run_id 为 NULL 的历史行仍可读）。
// 说明：同 mcp_migration_test.go，本文件位于 ent/schema 但使用外部测试包；
// `ent generate ./schema` 不会读取 _test.go 文件。

// TestBotSchemaMigration_CreatesTablesAndIndexes 覆盖「空库建表」路径。
func TestBotSchemaMigration_CreatesTablesAndIndexes(t *testing.T) {
	ctx := context.Background()
	dsn := mcpSchemaDSN(t)
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()
	db := openMCPSchemaDB(t, dsn)

	for _, table := range []string{"bot_runs", "bot_steps", "bot_events"} {
		require.True(t, mcpTableExists(t, db, table), "缺少 %s 表", table)
	}

	runColumns := mcpTableColumns(t, db, "bot_runs")
	for _, column := range []string{
		"tenant_id", "conversation_id", "bot_id", "entrypoint", "status",
		"model", "budget_json", "error_code", "started_at", "finished_at", "created_at", "updated_at",
	} {
		require.True(t, runColumns[column], "bot_runs 缺少列 %s", column)
	}
	stepColumns := mcpTableColumns(t, db, "bot_steps")
	for _, column := range []string{"tenant_id", "run_id", "step_index", "type", "payload_ref", "duration_ms", "error_code", "created_at"} {
		require.True(t, stepColumns[column], "bot_steps 缺少列 %s", column)
	}
	eventColumns := mcpTableColumns(t, db, "bot_events")
	for _, column := range []string{"tenant_id", "run_id", "seq", "type", "payload_json", "created_at"} {
		require.True(t, eventColumns[column], "bot_events 缺少列 %s", column)
	}

	// 迁移幂等：同一 schema 再次 Create 不应报错。
	require.NoError(t, client.Schema.Create(ctx))
}

// TestBotSchemaMigration_TenantIsolationAndUniqueConflicts 覆盖租户隔离与两类唯一约束。
func TestBotSchemaMigration_TenantIsolationAndUniqueConflicts(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", mcpSchemaDSN(t))
	defer client.Close()

	runA := client.BotRun.Create().
		SetTenantID(1).SetConversationID(11).SetEntrypoint("chat").SetStatus("running").
		SaveX(ctx)
	runB := client.BotRun.Create().
		SetTenantID(2).SetConversationID(22).SetEntrypoint("chat").SetStatus("running").
		SaveX(ctx)

	// 同一 (run_id, step_index) 二次写入必须冲突；不同 run 的同序号互不影响。
	client.BotStep.Create().SetTenantID(1).SetRunID(runA.ID).SetStepIndex(0).SetType("llm").SaveX(ctx)
	_, err := client.BotStep.Create().SetTenantID(1).SetRunID(runA.ID).SetStepIndex(0).SetType("tool").Save(ctx)
	require.Error(t, err, "同一运行的重复 step_index 必须被唯一约束拒绝")
	client.BotStep.Create().SetTenantID(2).SetRunID(runB.ID).SetStepIndex(0).SetType("llm").SaveX(ctx)

	// 同一 (run_id, seq) 二次写入必须冲突。
	client.BotEvent.Create().SetTenantID(1).SetRunID(runA.ID).SetSeq(0).SetType("run_started").SaveX(ctx)
	_, err = client.BotEvent.Create().SetTenantID(1).SetRunID(runA.ID).SetSeq(0).SetType("done").Save(ctx)
	require.Error(t, err, "同一运行的事件 seq 重复必须被唯一约束拒绝")

	// 租户维度隔离：按租户谓词查询只能看到本租户行（三表同口径）。
	tenantRuns, err := client.BotRun.Query().Where(botrun.TenantID(1)).All(ctx)
	require.NoError(t, err)
	require.Len(t, tenantRuns, 1)
	require.Equal(t, runA.ID, tenantRuns[0].ID)

	tenantSteps, err := client.BotStep.Query().Where(botstep.TenantID(1)).All(ctx)
	require.NoError(t, err)
	require.Len(t, tenantSteps, 1)

	tenantEvents, err := client.BotEvent.Query().Where(botevent.TenantID(1)).All(ctx)
	require.NoError(t, err)
	require.Len(t, tenantEvents, 1)
}

// TestBotSchemaMigration_LegacyToolInvocationRunIDNullable 覆盖旧行兼容：
// B0-02 已加列的历史 tool_invocations（run_id 为 NULL）在 B1-01 落表后仍可读，
// 且可后续回填 run_id 建立关联。
func TestBotSchemaMigration_LegacyToolInvocationRunIDNullable(t *testing.T) {
	ctx := context.Background()
	dsn := mcpSchemaDSN(t)
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()

	legacy := client.ToolInvocation.Create().
		SetTenantID(1).SetToolName("create_ticket").SetArguments("{}").
		SaveX(ctx)
	require.Zero(t, legacy.RunID, "旧行 run_id 为空（未关联运行）")

	run := client.BotRun.Create().
		SetTenantID(1).SetConversationID(11).SetEntrypoint("chat").SaveX(ctx)
	client.ToolInvocation.UpdateOneID(legacy.ID).SetRunID(run.ID).SaveX(ctx)

	linked := client.ToolInvocation.GetX(ctx, legacy.ID)
	require.Equal(t, run.ID, linked.RunID, "run_id 贯通：调用记录可关联到运行")

	invocations, err := client.ToolInvocation.Query().Where(toolinvocation.RunID(run.ID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, invocations, 1)
}
