package service

import (
	"context"
	"testing"

	connectorVector "itsm-backend/connector/vector"
	"itsm-backend/internal/helpindex"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

type helpVectorStoreFake struct {
	chunks []connectorVector.ChunkInput
}

func (f *helpVectorStoreFake) Search(_ context.Context, req connectorVector.SearchRequest) (connectorVector.SearchResponse, error) {
	results := make([]connectorVector.SearchResult, 0, len(f.chunks))
	for _, chunk := range f.chunks {
		if req.Filter != nil {
			if objectType, _ := req.Filter["objectType"].(string); objectType != "" {
				if chunk.Metadata["objectType"] != objectType {
					continue
				}
			}
		}
		results = append(results, connectorVector.SearchResult{
			ID: chunk.ID, Content: chunk.Content, Score: 1, Metadata: chunk.Metadata,
		})
		if req.TopK > 0 && len(results) >= req.TopK {
			break
		}
	}
	return connectorVector.SearchResponse{Results: results, Backend: "fake"}, nil
}

func (f *helpVectorStoreFake) Insert(_ context.Context, req connectorVector.InsertRequest) error {
	f.chunks = append(f.chunks, req.Chunks...)
	return nil
}

func (f *helpVectorStoreFake) Delete(context.Context, []string) error { return nil }
func (f *helpVectorStoreFake) Ping(context.Context) error             { return nil }
func (f *helpVectorStoreFake) Close() error                           { return nil }

func TestHelpDocsSemanticIndexAndSearch(t *testing.T) {
	ctx := context.Background()
	store := &helpVectorStoreFake{}
	rag := NewRAGService(nil, nil, &MockEmbedder{}, zaptest.NewLogger(t).Sugar(), RAGConfig{
		UseKeyword: true,
	})
	rag.SetVectorStore(store)

	require.NoError(t, rag.EnsureHelpDocsIndexed(ctx, 7))
	require.NotEmpty(t, store.chunks)
	indexed := len(store.chunks)
	require.NoError(t, rag.EnsureHelpDocsIndexed(ctx, 7))
	assert.Equal(t, indexed, len(store.chunks), "同一租户成功后不重复索引")
	assert.Equal(t, HelpVectorObjectType, store.chunks[0].Metadata["objectType"])
	assert.NotEmpty(t, store.chunks[0].Metadata["source"])

	results, err := rag.SearchHelpDocs(ctx, 7, "SLA", "", 5)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.LessOrEqual(t, len(results), 5)
	assert.Equal(t, "semantic", results[0].Source)
	assert.NotEmpty(t, results[0].Title)
	assert.NotEmpty(t, results[0].Snippet)

	info, docs := helpindex.Load().DocsForPage("ticket.detail")
	require.Equal(t, "ok", info.Status)
	filtered, err := rag.SearchHelpDocs(ctx, 7, "SLA", "ticket.detail", 10)
	require.NoError(t, err)
	allowed := make(map[string]struct{}, len(docs))
	for _, doc := range docs {
		allowed[doc.Path] = struct{}{}
	}
	for _, item := range filtered {
		_, ok := allowed[item.Path]
		assert.True(t, ok, "pageKey 过滤不应返回其它页面文档: %s", item.Path)
	}
}

func TestMergeHelpSearchResults_DedupAndLimit(t *testing.T) {
	lexical := []helpindex.SearchResult{
		{Path: "a.md", Title: "A", Source: "lexical"},
		{Path: "b.md", Title: "B", Source: "lexical"},
	}
	semantic := []helpindex.SearchResult{
		{Path: "b.md", Title: "B", Source: "semantic"},
		{Path: "c.md", Title: "C", Source: "semantic"},
	}
	merged := MergeHelpSearchResults(lexical, semantic, 10)
	require.Len(t, merged, 3)
	assert.Equal(t, "a.md", merged[0].Path)
	assert.Equal(t, "lexical", merged[1].Source, "重复路径保留词法排序")
	assert.Equal(t, "c.md", merged[2].Path)
	assert.Len(t, MergeHelpSearchResults(lexical, semantic, 2), 2)
}

func TestHelpDocsSemantic_UnavailableWhenEmbedderMissing(t *testing.T) {
	rag := NewRAGService(nil, nil, nil, zaptest.NewLogger(t).Sugar(), RAGConfig{})
	rag.SetVectorStore(&helpVectorStoreFake{})
	require.ErrorIs(t, rag.EnsureHelpDocsIndexed(context.Background(), 7), ErrHelpSemanticUnavailable)
	_, err := rag.SearchHelpDocs(context.Background(), 7, "SLA", "", 5)
	require.ErrorIs(t, err, ErrHelpSemanticUnavailable)
}
