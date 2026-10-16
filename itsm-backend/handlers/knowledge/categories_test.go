package knowledge

import (
	"context"
	"testing"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestValidateCategory_UnionPolicy（2026-10-16 修复回归）：
//
// 背景：此前 handler 链路（CreateArticle→repo.Create）完全跳过分类校验，
// ent service 的静态白名单只被单测引用，任意分类可落库；且 GET /categories
// （8 默认 ∪ 已用）与白名单是两套集合，UI 下拉值创建必 400。
//
// 修复后允许集合 = 静态白名单(12) ∪ 分类接口返回值（8 默认 ∪ 租户已用）；
// 任意自造分类与空分类必须拒绝，且「已用分类」按租户隔离。
func TestValidateCategory_UnionPolicy(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:knowledge_categories?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	svc := NewService(NewEntRepository(client), zaptest.NewLogger(t).Sugar())

	// 1) 默认分类与白名单（未建任何文章时也可用）
	require.NoError(t, svc.ValidateCategory(ctx, 7, "最佳实践"), "默认分类应可用（来自 GET /categories）")
	require.NoError(t, svc.ValidateCategory(ctx, 7, "账号管理"), "白名单中文值应可用")
	require.NoError(t, svc.ValidateCategory(ctx, 7, "troubleshooting"), "白名单英文值应可用")
	require.NoError(t, svc.ValidateCategory(ctx, 7, " 用户指南 "), "白名单值应容忍首尾空白")

	// 2) 任意自造分类 / 空分类拒绝
	require.Error(t, svc.ValidateCategory(ctx, 7, "随便分类X"))
	require.Error(t, svc.ValidateCategory(ctx, 7, "   "))

	// 3) 租户已用分类（不属默认/白名单）也可继续使用
	_, err := svc.repo.Create(ctx, &Article{
		Title: "legacy", Content: "c", Category: "服务指南", AuthorID: 1, TenantID: 7,
	})
	require.NoError(t, err)
	require.NoError(t, svc.ValidateCategory(ctx, 7, "服务指南"))

	// 4) 已用分类按租户隔离：租户 8 不得使用租户 7 的已用分类
	require.Error(t, svc.ValidateCategory(ctx, 8, "服务指南"))
}
