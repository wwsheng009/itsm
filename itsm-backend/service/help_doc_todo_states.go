package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// H1-01/H1-03/H1-04 条件项闭环：文档待办「认领/忽略」状态持久化。
//
// 数据表 ai_doc_todo_states（migrations/20261018n_ai_doc_todo_states.sql +
// database/rls/migrations/017_ai_doc_todo_states_policies.sql）：
//   - 每租户每 pageKey 至多一行；state ∈ {claimed, ignored}；
//   - 「恢复待办」= 删除该行（缺省无行即 open，避免存空状态制造噪声）；
//   - updated_by 记录最近操作人（用户 id）。
//
// 消费方：GET /ai/help/todos 合并状态（未装配 → states.available=false，按 open 缺省渲染）；
// 写路径：PUT /ai/help/todos/state（ai:write，handler 层校验请求形状）。

const (
	// HelpDocTodoStateOpen 待处理（未认领未忽略；表中无行即此态）。
	HelpDocTodoStateOpen = "open"
	// HelpDocTodoStateClaimed 已认领（有人负责出稿/修订）。
	HelpDocTodoStateClaimed = "claimed"
	// HelpDocTodoStateIgnored 已忽略（显式静默，运营决定不处理）。
	HelpDocTodoStateIgnored = "ignored"

	helpDocTodoPageKeyMaxLen = 128
)

var (
	// ErrHelpDocTodoStateUnavailable 仓储未装配（db=nil 的测试/降级态）。
	ErrHelpDocTodoStateUnavailable = errors.New("help doc todo state repository not initialized")
	// ErrHelpDocTodoStateInvalid 非法输入（租户/页面键/状态不满足约束）。
	ErrHelpDocTodoStateInvalid = errors.New("help doc todo state invalid")
)

// HelpDocTodoState 是一条待办运营状态（pageKey 维度）。
type HelpDocTodoState struct {
	PageKey   string `json:"pageKey"`
	State     string `json:"state"`
	UpdatedBy int    `json:"updatedBy,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// HelpDocTodoStateService 待办状态读写（租户为隐式条件；RLS 兜底隔离）。
type HelpDocTodoStateService struct {
	db     *sql.DB
	logger *zap.SugaredLogger
}

// NewHelpDocTodoStateService 构造待办状态服务。
func NewHelpDocTodoStateService(db *sql.DB, logger *zap.SugaredLogger) *HelpDocTodoStateService {
	return &HelpDocTodoStateService{db: db, logger: logger}
}

// NormalizeHelpDocTodoState 归一化状态输入（忽略大小写与首尾空白），并返回是否合法。
func NormalizeHelpDocTodoState(state string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case HelpDocTodoStateOpen:
		return HelpDocTodoStateOpen, true
	case HelpDocTodoStateClaimed:
		return HelpDocTodoStateClaimed, true
	case HelpDocTodoStateIgnored:
		return HelpDocTodoStateIgnored, true
	default:
		return "", false
	}
}

// ValidateHelpDocTodoPageKey 校验页面键（非空、列宽内；与 ai_doc_todo_states.page_key 一致）。
func ValidateHelpDocTodoPageKey(pageKey string) (string, bool) {
	key := strings.TrimSpace(pageKey)
	if key == "" || len(key) > helpDocTodoPageKeyMaxLen {
		return "", false
	}
	return key, true
}

// ListStates 读取租户全部持久化状态（pageKey → 状态；无行租户返回空 map，非 nil）。
func (s *HelpDocTodoStateService) ListStates(ctx context.Context, tenantID int) (map[string]HelpDocTodoState, error) {
	if s == nil || s.db == nil {
		return nil, ErrHelpDocTodoStateUnavailable
	}
	if tenantID <= 0 {
		return nil, fmt.Errorf("%w: tenant id required", ErrHelpDocTodoStateInvalid)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT page_key, state, updated_by, updated_at
		FROM ai_doc_todo_states
		WHERE tenant_id = $1
		ORDER BY page_key ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list help doc todo states: %w", err)
	}
	defer rows.Close()

	states := make(map[string]HelpDocTodoState)
	for rows.Next() {
		var (
			item      HelpDocTodoState
			updatedAt time.Time
		)
		if err := rows.Scan(&item.PageKey, &item.State, &item.UpdatedBy, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan help doc todo state: %w", err)
		}
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		states[item.PageKey] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate help doc todo states: %w", err)
	}
	return states, nil
}

// SetState 写入/清除单页待办状态：
//   - claimed / ignored → UPSERT（幂等，重复认领刷新操作人与时间）；
//   - open → 删除行（回到缺省态；重复恢复同样幂等）。
//
// 返回落库后的状态（open 无 updatedAt/updatedBy）。
func (s *HelpDocTodoStateService) SetState(ctx context.Context, tenantID, userID int, pageKey, state string) (*HelpDocTodoState, error) {
	if s == nil || s.db == nil {
		return nil, ErrHelpDocTodoStateUnavailable
	}
	key, ok := ValidateHelpDocTodoPageKey(pageKey)
	if !ok || tenantID <= 0 || userID <= 0 {
		return nil, fmt.Errorf("%w: tenant id, user id and page key required", ErrHelpDocTodoStateInvalid)
	}
	normalized, ok := NormalizeHelpDocTodoState(state)
	if !ok {
		return nil, fmt.Errorf("%w: state must be claimed|ignored|open", ErrHelpDocTodoStateInvalid)
	}

	if normalized == HelpDocTodoStateOpen {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM ai_doc_todo_states WHERE tenant_id = $1 AND page_key = $2`,
			tenantID, key); err != nil {
			return nil, fmt.Errorf("clear help doc todo state: %w", err)
		}
		if s.logger != nil {
			s.logger.Infow("help doc todo state cleared", "tenantID", tenantID, "pageKey", key, "userID", userID)
		}
		return &HelpDocTodoState{PageKey: key, State: HelpDocTodoStateOpen}, nil
	}

	var (
		out       HelpDocTodoState
		updatedAt time.Time
	)
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO ai_doc_todo_states (tenant_id, page_key, state, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (tenant_id, page_key)
		DO UPDATE SET state = EXCLUDED.state, updated_by = EXCLUDED.updated_by, updated_at = NOW()
		RETURNING state, updated_by, updated_at`,
		tenantID, key, normalized, userID).Scan(&out.State, &out.UpdatedBy, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert help doc todo state: %w", err)
	}
	out.PageKey = key
	out.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if s.logger != nil {
		s.logger.Infow("help doc todo state set", "tenantID", tenantID, "pageKey", key, "state", out.State, "userID", userID)
	}
	return &out, nil
}
