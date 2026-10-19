package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	connectorVector "itsm-backend/connector/vector"
	"itsm-backend/internal/helpindex"
)

// T5-09b：帮助文档（docs/help 真源）的向量索引与语义检索。
//
// 设计取舍：
//   - 文档内容随应用内嵌，无需构建期调用外部 embedding；
//   - 首次帮助检索时按租户懒索引，成功后进程内不再重复；
//   - connector store 与 legacy pgvector 双写，任一不可用由调用方回退词法检索；
//   - 向量对象类型固定 `help_doc`，source 保存文档相对路径，避免把 URI 放进 object_id。

const HelpVectorObjectType = "help_doc"

// ErrHelpSemanticUnavailable 表示当前部署未配置 embedding/vector（调用方应回退词法）。
var ErrHelpSemanticUnavailable = errors.New("help semantic index unavailable")

// EnsureHelpDocsIndexed 按租户懒构建帮助文档向量索引。
func (r *RAGService) EnsureHelpDocsIndexed(ctx context.Context, tenantID int) error {
	if r == nil || tenantID <= 0 || r.embedder == nil || !r.useVector ||
		(r.vectorStore == nil && r.vectors == nil) {
		return ErrHelpSemanticUnavailable
	}
	r.helpIndexMu.Lock()
	defer r.helpIndexMu.Unlock()
	if r.helpIndexed != nil {
		if _, ok := r.helpIndexed[tenantID]; ok {
			return nil
		}
	}

	docs := helpindex.Load().Docs()
	if len(docs) == 0 {
		return ErrHelpSemanticUnavailable
	}
	for _, doc := range docs {
		text := strings.TrimSpace(doc.Title + "\n" + doc.Text)
		if text == "" {
			continue
		}
		embedding, err := r.embedder.Embed(text)
		if err != nil {
			return fmt.Errorf("embed help doc %s: %w", doc.Path, err)
		}
		objectID := helpDocObjectID(doc.Path)
		if r.vectorStore != nil {
			err = r.vectorStore.Insert(ctx, connectorVector.InsertRequest{Chunks: []connectorVector.ChunkInput{{
				ID:      fmt.Sprintf("tenant:%d:%s:%d", tenantID, HelpVectorObjectType, objectID),
				Content: doc.Text,
				Vector:  embedding,
				Metadata: map[string]interface{}{
					"tenantID":   tenantID,
					"objectType": HelpVectorObjectType,
					"source":     doc.Path,
					"title":      doc.Title,
				},
			}}})
			if err != nil {
				return fmt.Errorf("connector help insert %s: %w", doc.Path, err)
			}
		}
		if r.vectors != nil {
			if err := r.vectors.Upsert(ctx, tenantID, HelpVectorObjectType, objectID, embedding, doc.Text, doc.Path); err != nil {
				return fmt.Errorf("legacy help upsert %s: %w", doc.Path, err)
			}
		}
	}
	if r.helpIndexed == nil {
		r.helpIndexed = make(map[int]struct{})
	}
	r.helpIndexed[tenantID] = struct{}{}
	return nil
}

// SearchHelpDocs 语义检索帮助文档；调用方负责把结果与词法结果合并。
func (r *RAGService) SearchHelpDocs(
	ctx context.Context,
	tenantID int,
	query, pageKey string,
	limit int,
) ([]helpindex.SearchResult, error) {
	if r == nil || tenantID <= 0 || r.embedder == nil || !r.useVector ||
		(r.vectorStore == nil && r.vectors == nil) {
		return nil, ErrHelpSemanticUnavailable
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	embedding, err := r.embedder.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed help query: %w", err)
	}

	seen := make(map[string]struct{})
	results := make([]helpindex.SearchResult, 0, limit)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		doc, ok := helpindex.Load().ReadDoc(path)
		if !ok {
			return
		}
		if pageKey != "" && !helpContainsString(doc.PageKeys, pageKey) {
			return
		}
		seen[path] = struct{}{}
		results = append(results, helpindex.SearchResult{
			Path:     doc.Path,
			Title:    doc.Title,
			PageKeys: doc.PageKeys,
			Snippet:  helpSnippet(doc.Text, query),
			Source:   "semantic",
		})
	}

	if r.vectorStore != nil {
		response, err := r.vectorStore.Search(ctx, connectorVector.SearchRequest{
			Vector: embedding,
			Query:  query,
			TopK:   limit * 3,
			Filter: map[string]interface{}{"tenantID": tenantID, "objectType": HelpVectorObjectType},
		})
		if err == nil {
			for _, item := range response.Results {
				add(metadataString(item.Metadata, "source"))
				if len(results) >= limit {
					return results, nil
				}
			}
		}
	}
	if r.vectors != nil {
		rows, err := r.vectors.SearchTopKByTypeResults(ctx, tenantID, HelpVectorObjectType, embedding, limit*3)
		if err == nil {
			for _, item := range rows {
				add(item.Source)
				if len(results) >= limit {
					return results, nil
				}
			}
		}
	}
	return results, nil
}

// MergeHelpSearchResults 合并词法/语义结果：保留词法排序，按路径去重后用语义补齐。
func MergeHelpSearchResults(
	lexical, semantic []helpindex.SearchResult,
	limit int,
) []helpindex.SearchResult {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	out := make([]helpindex.SearchResult, 0, limit)
	seen := make(map[string]struct{}, len(lexical)+len(semantic))
	for _, group := range [][]helpindex.SearchResult{lexical, semantic} {
		for _, item := range group {
			if _, ok := seen[item.Path]; ok {
				continue
			}
			seen[item.Path] = struct{}{}
			out = append(out, item)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func helpDocObjectID(path string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(path))
	value := int(h.Sum32() & 0x7fffffff)
	if value == 0 {
		return 1
	}
	return value
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}

func helpContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func helpSnippet(text, query string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	startRune := 0
	lowerQueryRunes := []rune(strings.ToLower(strings.TrimSpace(query)))
	if len(lowerQueryRunes) > 0 {
		lowerRunes := []rune(strings.ToLower(text))
		for i := 0; i+len(lowerQueryRunes) <= len(lowerRunes); i++ {
			match := true
			for j := range lowerQueryRunes {
				if lowerRunes[i+j] != lowerQueryRunes[j] {
					match = false
					break
				}
			}
			if match {
				startRune = i
				break
			}
		}
	}
	if startRune >= len(runes) {
		startRune = 0
	}
	const width = 180
	end := startRune + width
	if end > len(runes) {
		end = len(runes)
	}
	prefix := ""
	if startRune > 0 {
		prefix = "…"
	}
	suffix := ""
	if end < len(runes) {
		suffix = "…"
	}
	return prefix + string(runes[startRune:end]) + suffix
}
