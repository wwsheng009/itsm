package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// H1-01/H1-03/H1-04 条件项闭环：文档待办状态持久化（归一化/校验/读写/降级）。

func newHelpDocTodoStateService(t *testing.T) (*HelpDocTodoStateService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewHelpDocTodoStateService(db, nil), mock
}

func TestNormalizeHelpDocTodoState(t *testing.T) {
	cases := map[string]string{
		"claimed":   HelpDocTodoStateClaimed,
		" Claimed ": HelpDocTodoStateClaimed,
		"ignored":   HelpDocTodoStateIgnored,
		"IGNORED":   HelpDocTodoStateIgnored,
		"open":      HelpDocTodoStateOpen,
	}
	for in, want := range cases {
		got, ok := NormalizeHelpDocTodoState(in)
		assert.True(t, ok, "input %q should be valid", in)
		assert.Equal(t, want, got)
	}
	for _, bad := range []string{"", "done", "closed", "pending", "  "} {
		_, ok := NormalizeHelpDocTodoState(bad)
		assert.False(t, ok, "input %q should be invalid", bad)
	}
}

func TestValidateHelpDocTodoPageKey(t *testing.T) {
	key, ok := ValidateHelpDocTodoPageKey("  dashboard  ")
	require.True(t, ok)
	assert.Equal(t, "dashboard", key)

	_, ok = ValidateHelpDocTodoPageKey("")
	assert.False(t, ok)
	_, ok = ValidateHelpDocTodoPageKey("   ")
	assert.False(t, ok)
	_, ok = ValidateHelpDocTodoPageKey(strings.Repeat("x", helpDocTodoPageKeyMaxLen+1))
	assert.False(t, ok)

	key, ok = ValidateHelpDocTodoPageKey(strings.Repeat("x", helpDocTodoPageKeyMaxLen))
	assert.True(t, ok)
	assert.Len(t, key, helpDocTodoPageKeyMaxLen)
}

func TestListHelpDocTodoStates_ScansRows(t *testing.T) {
	svc, mock := newHelpDocTodoStateService(t)
	updated := time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("FROM ai_doc_todo_states")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "state", "updated_by", "updated_at"}).
			AddRow("dashboard", HelpDocTodoStateClaimed, int64(9), updated).
			AddRow("ghost.page", HelpDocTodoStateIgnored, int64(4), updated))

	states, err := svc.ListStates(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, HelpDocTodoStateClaimed, states["dashboard"].State)
	assert.Equal(t, 9, states["dashboard"].UpdatedBy)
	assert.Equal(t, "2026-10-18T03:00:00Z", states["dashboard"].UpdatedAt)
	assert.Equal(t, HelpDocTodoStateIgnored, states["ghost.page"].State)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListHelpDocTodoStates_EmptyUnavailableAndInvalid(t *testing.T) {
	svc, mock := newHelpDocTodoStateService(t)
	mock.ExpectQuery(regexp.QuoteMeta("FROM ai_doc_todo_states")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"page_key", "state", "updated_by", "updated_at"}))

	states, err := svc.ListStates(context.Background(), 7)
	require.NoError(t, err)
	assert.NotNil(t, states, "空结果应为非 nil map（前端零状态渲染闭环）")
	assert.Empty(t, states)
	assert.NoError(t, mock.ExpectationsWereMet())

	// 未装配 → 明确错误（调用方降级 states.available=false）。
	_, err = NewHelpDocTodoStateService(nil, nil).ListStates(context.Background(), 7)
	assert.True(t, errors.Is(err, ErrHelpDocTodoStateUnavailable))

	// 非法租户 → 校验错误（零库访问）。
	_, err = svc.ListStates(context.Background(), 0)
	assert.True(t, errors.Is(err, ErrHelpDocTodoStateInvalid))
}

func TestSetHelpDocTodoState_UpsertClaimed(t *testing.T) {
	svc, mock := newHelpDocTodoStateService(t)
	updated := time.Date(2026, 10, 18, 4, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO ai_doc_todo_states")).
		WithArgs(int64(7), "dashboard", HelpDocTodoStateClaimed, int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"state", "updated_by", "updated_at"}).
			AddRow(HelpDocTodoStateClaimed, int64(9), updated))

	state, err := svc.SetState(context.Background(), 7, 9, " dashboard ", " Claimed ")
	require.NoError(t, err)
	assert.Equal(t, "dashboard", state.PageKey)
	assert.Equal(t, HelpDocTodoStateClaimed, state.State)
	assert.Equal(t, 9, state.UpdatedBy)
	assert.Equal(t, "2026-10-18T04:00:00Z", state.UpdatedAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSetHelpDocTodoState_OpenDeletesRow(t *testing.T) {
	svc, mock := newHelpDocTodoStateService(t)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM ai_doc_todo_states")).
		WithArgs(int64(7), "dashboard").
		WillReturnResult(sqlmock.NewResult(0, 1))

	state, err := svc.SetState(context.Background(), 7, 9, "dashboard", "open")
	require.NoError(t, err)
	assert.Equal(t, "dashboard", state.PageKey)
	assert.Equal(t, HelpDocTodoStateOpen, state.State)
	assert.Empty(t, state.UpdatedBy)
	assert.Empty(t, state.UpdatedAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSetHelpDocTodoState_ValidationAndUnavailable(t *testing.T) {
	svc, mock := newHelpDocTodoStateService(t)
	cases := map[string]func() error{
		"missing tenant": func() error {
			_, err := svc.SetState(context.Background(), 0, 9, "dashboard", "claimed")
			return err
		},
		"missing user": func() error {
			_, err := svc.SetState(context.Background(), 7, 0, "dashboard", "claimed")
			return err
		},
		"missing page key": func() error {
			_, err := svc.SetState(context.Background(), 7, 9, "  ", "claimed")
			return err
		},
		"unknown state": func() error {
			_, err := svc.SetState(context.Background(), 7, 9, "dashboard", "done")
			return err
		},
	}
	for name, call := range cases {
		assert.True(t, errors.Is(call(), ErrHelpDocTodoStateInvalid), name)
	}
	assert.NoError(t, mock.ExpectationsWereMet(), "非法输入不得触达数据库")

	_, err := NewHelpDocTodoStateService(nil, nil).SetState(context.Background(), 7, 9, "dashboard", "claimed")
	assert.True(t, errors.Is(err, ErrHelpDocTodoStateUnavailable))
}
