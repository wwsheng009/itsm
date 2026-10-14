package sla

import (
	"context"
	"errors"
	"testing"

	"itsm-backend/common"
	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// IM-40（2026-10-14 验收修复）：DeleteDefinition 对域外 id 返回 NotFound 业务错误，
// 且目标行不受影响；存在行删除路径行为不变（成功）。
func TestDeleteDefinition_CrossTenantNotFound(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:del_def_guard?mode=memory&cache=shared&_fk=1")
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	repo := NewEntRepository(client)

	mk := func(name string) int {
		def, err := client.SLADefinition.Create().
			SetName(name).
			SetResponseTime(30).
			SetResolutionTime(60).
			SetIsActive(true).
			SetTenantID(1).
			Save(ctx)
		require.NoError(t, err)
		return def.ID
	}

	// 同租户删除 → 成功
	idOK := mk("guard-ok")
	require.NoError(t, repo.DeleteDefinition(ctx, idOK, 1))

	// 域外删除（tenant 2 语境删 tenant 1 的行）→ NotFound 业务错误
	idGuard := mk("guard-cross")
	err := repo.DeleteDefinition(ctx, idGuard, 2)
	require.Error(t, err)
	var be *common.BusinessError
	require.True(t, errors.As(err, &be), "应为 BusinessError，实际: %v", err)
	assert.Equal(t, common.NotFoundCode, be.Code)

	// 目标行未被删除
	_, err = client.SLADefinition.Get(ctx, idGuard)
	require.NoError(t, err, "域外删除不得影响目标行")
}
