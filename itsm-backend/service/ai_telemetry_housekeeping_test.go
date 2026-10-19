package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// O0-02 条件项：埋点保留期清理单测（sqlmock，不依赖真实数据库）。
//
// 覆盖口径：
//   - 缺省值回落（90 天 / 24h / 5000 行）与自定义保留期 cutoff；
//   - 批量循环扫空 / 单批未满即停 / 批次硬上限；
//   - 幂等（无匹配 0 行）、中途失败返回部分进度、RowsAffected 不可用保守终止；
//   - Start 启动即跑一轮且随 ctx 取消退出。

// purgeStmtPattern 精确匹配删除语句（含 occurred_at < cutoff 的保留期条件与最旧优先分批）。
var purgeStmtPattern = regexp.QuoteMeta(assistantTelemetryPurgeStmt)

// cutoffMatcher 断言第一个参数是期望的 cutoff 时刻（容差内）。
type cutoffMatcher struct {
	want time.Time
	tol  time.Duration
}

func (m cutoffMatcher) Match(v driver.Value) bool {
	got, ok := v.(time.Time)
	if !ok {
		return false
	}
	diff := got.Sub(m.want)
	if diff < 0 {
		diff = -diff
	}
	return diff <= m.tol
}

func newAITelemetryHousekeeping(t *testing.T, retentionDays, intervalMinutes, batchSize int) (*AITelemetryHousekeepingService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewAITelemetryHousekeepingService(db, zap.NewNop().Sugar(), retentionDays, intervalMinutes, batchSize), mock
}

func TestAITelemetryHousekeeping_Defaults(t *testing.T) {
	svc := NewAITelemetryHousekeepingService(nil, nil, 0, 0, 0)
	assert.Equal(t, assistantTelemetryDefaultRetentionDays, svc.RetentionDays())
	assert.Equal(t, 24*time.Hour, svc.Interval())
	assert.Equal(t, assistantTelemetryDefaultCleanupBatchSize, svc.batchSize)
	assert.Equal(t, assistantTelemetryDefaultCleanupMaxBatches, svc.maxBatches)

	custom := NewAITelemetryHousekeepingService(nil, nil, 7, 60, 50)
	assert.Equal(t, 7, custom.RetentionDays())
	assert.Equal(t, time.Hour, custom.Interval())
	assert.Equal(t, 50, custom.batchSize)
}

func TestAITelemetryHousekeeping_NilDB(t *testing.T) {
	svc := NewAITelemetryHousekeepingService(nil, nil, 90, 60, 100)
	n, err := svc.RunOnce(context.Background())
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, ErrAssistantTelemetryUnavailable)

	// 空服务/nil 接收者：Start 直接返回，不 panic。
	var nilSvc *AITelemetryHousekeepingService
	nilSvc.Start(context.Background())
	svc.Start(context.Background())
}

func TestAITelemetryHousekeeping_RunOnceBatchesUntilDrained(t *testing.T) {
	svc, mock := newAITelemetryHousekeeping(t, 90, 60, 5000)

	mock.ExpectExec(purgeStmtPattern).
		WithArgs(cutoffMatcher{want: time.Now().AddDate(0, 0, -90), tol: 2 * time.Minute}, 5000).
		WillReturnResult(sqlmock.NewResult(0, 5000))
	mock.ExpectExec(purgeStmtPattern).
		WithArgs(cutoffMatcher{want: time.Now().AddDate(0, 0, -90), tol: 2 * time.Minute}, 5000).
		WillReturnResult(sqlmock.NewResult(0, 300))

	n, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 5300, n)
	// 两批扫空即停：无第三次执行（ExpectationsWereMet 全满足即为证据）。
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAITelemetryHousekeeping_RunOnceEmptyIsIdempotent(t *testing.T) {
	svc, mock := newAITelemetryHousekeeping(t, 90, 60, 5000)

	for i := 0; i < 2; i++ {
		mock.ExpectExec(purgeStmtPattern).
			WithArgs(sqlmock.AnyArg(), 5000).
			WillReturnResult(sqlmock.NewResult(0, 0))
	}

	first, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, first)
	second, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, second)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAITelemetryHousekeeping_RunOnceCustomRetentionCutoff(t *testing.T) {
	svc, mock := newAITelemetryHousekeeping(t, 7, 60, 100)

	mock.ExpectExec(purgeStmtPattern).
		WithArgs(cutoffMatcher{want: time.Now().AddDate(0, 0, -7), tol: 2 * time.Minute}, 100).
		WillReturnResult(sqlmock.NewResult(0, 3))

	n, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAITelemetryHousekeeping_RunOnceStopsAtBatchCap(t *testing.T) {
	svc, mock := newAITelemetryHousekeeping(t, 90, 60, 2)
	svc.maxBatches = 3

	// 每批都满（2 行）→ 达到硬上限停止，不再发起第 4 次。
	for i := 0; i < 3; i++ {
		mock.ExpectExec(purgeStmtPattern).
			WithArgs(sqlmock.AnyArg(), 2).
			WillReturnResult(sqlmock.NewResult(0, 2))
	}

	n, err := svc.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAITelemetryHousekeeping_RunOnceErrorStopsLoop(t *testing.T) {
	svc, mock := newAITelemetryHousekeeping(t, 90, 60, 5000)

	mock.ExpectExec(purgeStmtPattern).
		WithArgs(sqlmock.AnyArg(), 5000).
		WillReturnError(errors.New("db down"))

	n, err := svc.RunOnce(context.Background())
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Contains(t, err.Error(), "purge assistant telemetry events")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAITelemetryHousekeeping_RunOncePartialProgressOnMidwayError(t *testing.T) {
	svc, mock := newAITelemetryHousekeeping(t, 90, 60, 5000)

	mock.ExpectExec(purgeStmtPattern).
		WithArgs(sqlmock.AnyArg(), 5000).
		WillReturnResult(sqlmock.NewResult(0, 5000))
	mock.ExpectExec(purgeStmtPattern).
		WithArgs(sqlmock.AnyArg(), 5000).
		WillReturnError(errors.New("connection reset"))

	n, err := svc.RunOnce(context.Background())
	require.Error(t, err)
	// 第一批已提交的进度不丢弃。
	assert.Equal(t, 5000, n)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// rowsAffectedErr 模拟驱动不支持 RowsAffected 的情形。
type rowsAffectedErr struct{}

func (rowsAffectedErr) LastInsertId() (int64, error) { return 0, errors.New("unsupported") }
func (rowsAffectedErr) RowsAffected() (int64, error) { return 0, errors.New("unsupported") }

func TestAITelemetryHousekeeping_RunOnceRowsAffectedUnavailable(t *testing.T) {
	svc, mock := newAITelemetryHousekeeping(t, 90, 60, 5000)

	mock.ExpectExec(purgeStmtPattern).
		WithArgs(sqlmock.AnyArg(), 5000).
		WillReturnResult(rowsAffectedErr{})

	n, err := svc.RunOnce(context.Background())
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Contains(t, err.Error(), "rows affected")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAITelemetryHousekeeping_StartRunsImmediatelyAndStopsOnCancel(t *testing.T) {
	svc, mock := newAITelemetryHousekeeping(t, 90, 60, 100)

	mock.ExpectExec(purgeStmtPattern).
		WithArgs(sqlmock.AnyArg(), 100).
		WillReturnResult(sqlmock.NewResult(0, 0))

	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)

	// 启动即跑一轮：2s 内应满足期望（间隔 60min，不会触发第二轮）。
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := mock.ExpectationsWereMet(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Start 未在启动后立即执行一轮清理")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	time.Sleep(150 * time.Millisecond)
	// 取消后无额外执行、无未满足期望。
	assert.NoError(t, mock.ExpectationsWereMet())
}
