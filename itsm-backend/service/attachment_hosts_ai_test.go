package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"itsm-backend/ent/enttest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T5-03a：AI 对话附件宿主（ai_chat / bizId=userID）的存在性语义。
func TestDefaultAttachmentHostAIChat(t *testing.T) {
	client := enttest.Open(t, "sqlite3", fmt.Sprintf("file:attachment_hosts_ai_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	tn, user := seedHost(t, client, "ai-host")
	other, _ := seedHost(t, client, "ai-host-other")

	host := defaultAttachmentHosts()[AttachmentBizTypeAIChat]
	require.NotNil(t, host)

	ok, err := host.Exists(ctx, client, tn.ID, user.ID)
	require.NoError(t, err)
	assert.True(t, ok, "同租户用户可作为 AI 附件宿主")

	ok, err = host.Exists(ctx, client, other.ID, user.ID)
	require.NoError(t, err)
	assert.False(t, ok, "跨租户必须不可见")

	ok, err = host.Exists(ctx, client, tn.ID, 999999)
	require.NoError(t, err)
	assert.False(t, ok, "不存在用户必须拒绝")
}
