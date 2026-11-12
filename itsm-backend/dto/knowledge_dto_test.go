package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 回归：空知识列表在 JSON 中必须是 []，不能因 nil slice 变成 null。
func TestKnowledgeArticleListResponseMarshalsEmptySlicesAsArrays(t *testing.T) {
	serverTime := time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)
	data, err := json.Marshal(KnowledgeArticleListResponse{ServerTime: serverTime})
	require.NoError(t, err)

	var payload struct {
		Items      json.RawMessage `json:"items"`
		Articles   json.RawMessage `json:"articles"`
		ServerTime time.Time       `json:"serverTime"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	require.JSONEq(t, `[]`, string(payload.Items))
	require.JSONEq(t, `[]`, string(payload.Articles))
	require.Equal(t, serverTime, payload.ServerTime)
}

// articles 是 items 的兼容别名；只给其中一边赋值时也必须保持两边一致。
func TestKnowledgeArticleListResponseKeepsAliasInSync(t *testing.T) {
	data, err := json.Marshal(KnowledgeArticleListResponse{
		Articles: []KnowledgeArticleResponse{{ID: 7, Title: "KB"}},
	})
	require.NoError(t, err)

	var payload struct {
		Items    []KnowledgeArticleResponse `json:"items"`
		Articles []KnowledgeArticleResponse `json:"articles"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Len(t, payload.Items, 1)
	require.Len(t, payload.Articles, 1)
	require.Equal(t, 7, payload.Items[0].ID)
	require.Equal(t, 7, payload.Articles[0].ID)
}
