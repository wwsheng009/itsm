// Package helpindex 提供帮助文档的内嵌索引与只读检索（H0-03 Help Agent 数据面）。
//
// 数据来源：itsm-frontend/scripts/gen-page-manifest.mjs 生成的三份 JSON（真源 docs/help/**）：
//   - help-index.json：文档 路径/标题/page_keys/正文（frontmatter 已剥离）；
//   - page-manifest.json：页面清单（pattern/entrypoint/scene/status/docs）；
//   - doc-ledger.json：覆盖率账本（coverage/totals）。
//
// 口径：只读、零文件系统依赖（go:embed）；未知 pageKey / 空查询按空结果处理（调用方自行降级），
// 不在此层做权限判断（RBAC 由路由/工具门禁负责）。
package helpindex

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

//go:embed help-index.json
var helpIndexJSON []byte

//go:embed page-manifest.json
var pageManifestJSON []byte

//go:embed doc-ledger.json
var docLedgerJSON []byte

// Doc 是一篇帮助文档（正文为 frontmatter 剥离后的 Markdown）。
type Doc struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	PageKeys []string `json:"pageKeys"`
	Text     string   `json:"text"`
}

// PageInfo 是一个页面的帮助覆盖信息。
type PageInfo struct {
	PageKey    string `json:"pageKey"`
	Title      string `json:"title"`
	Pattern    string `json:"pattern"`
	Entrypoint string `json:"entrypoint"`
	// Status：ok | missing | stale（来自 ledger；未知页面为 unknown）
	Status string   `json:"status"`
	Docs   []string `json:"docs"`
}

// OrphanDoc 是账本中的 orphan 文档（声明的 page_key 不在页面注册表内；用于覆盖率看板提示）。
type OrphanDoc struct {
	Doc         string   `json:"doc"`
	UnknownKeys []string `json:"unknownKeys"`
}

// SearchResult 是一次检索命中。
type SearchResult struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	PageKeys []string `json:"pageKeys"`
	Snippet  string   `json:"snippet"`
	Score    int      `json:"score"`
}

// Index 是加载后的帮助索引（进程内单例由 Load 保证）。
type Index struct {
	GeneratedAt string
	docs        []Doc
	docByPath   map[string]Doc
	pages       []PageInfo
	pageByKey   map[string]PageInfo
	ledger      ledgerFile
}

type rawHelpIndex struct {
	GeneratedAt string `json:"generatedAt"`
	Docs        []Doc  `json:"docs"`
}

type manifestFile struct {
	GeneratedAt string `json:"generatedAt"`
	Pages       []struct {
		PageKey    string   `json:"pageKey"`
		Pattern    string   `json:"pattern"`
		Title      string   `json:"title"`
		Entrypoint string   `json:"entrypoint"`
		Status     string   `json:"status"`
		Docs       []string `json:"docs"`
	} `json:"pages"`
}

type ledgerFile struct {
	GeneratedAt string  `json:"generatedAt"`
	Coverage    float64 `json:"coverage"`
	Totals      struct {
		Pages   int `json:"pages"`
		OK      int `json:"ok"`
		Missing int `json:"missing"`
		Stale   int `json:"stale"`
		Orphan  int `json:"orphan"`
	} `json:"totals"`
	OrphanDocs []OrphanDoc `json:"orphanDocs"`
}

var (
	once  sync.Once
	index *Index
)

// Load 返回进程内帮助索引（解析失败时返回空索引，工具面按"无命中"降级）。
func Load() *Index {
	once.Do(func() {
		index = buildIndex()
	})
	return index
}

func buildIndex() *Index {
	idx := &Index{docByPath: map[string]Doc{}, pageByKey: map[string]PageInfo{}}

	var help rawHelpIndex
	if err := json.Unmarshal(helpIndexJSON, &help); err != nil {
		return idx
	}
	idx.GeneratedAt = help.GeneratedAt
	idx.docs = help.Docs
	for _, doc := range help.Docs {
		idx.docByPath[doc.Path] = doc
	}

	var manifest manifestFile
	if err := json.Unmarshal(pageManifestJSON, &manifest); err != nil {
		return idx
	}
	for _, page := range manifest.Pages {
		info := PageInfo{
			PageKey:    page.PageKey,
			Title:      page.Title,
			Pattern:    page.Pattern,
			Entrypoint: page.Entrypoint,
			Status:     page.Status,
			Docs:       page.Docs,
		}
		idx.pages = append(idx.pages, info)
		idx.pageByKey[info.PageKey] = info
	}

	_ = json.Unmarshal(docLedgerJSON, &idx.ledger)
	return idx
}

// Search 按关键词检索帮助文档；pageKey 非空时限定该页面文档；limit 缺省 5、上限 20。
func (i *Index) Search(query, pageKey string, limit int) []SearchResult {
	if i == nil {
		return nil
	}
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return nil
	}
	terms := strings.Fields(q)
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	results := make([]SearchResult, 0, 8)
	for _, doc := range i.docs {
		if pageKey != "" && !containsString(doc.PageKeys, pageKey) {
			continue
		}
		lowerTitle := strings.ToLower(doc.Title)
		lowerText := strings.ToLower(doc.Text)
		lowerKeys := strings.ToLower(strings.Join(doc.PageKeys, " "))

		score := 0
		for _, term := range terms {
			score += termScore(term, lowerTitle, lowerText, lowerKeys)
		}
		if score == 0 {
			continue
		}
		results = append(results, SearchResult{
			Path:     doc.Path,
			Title:    doc.Title,
			PageKeys: doc.PageKeys,
			Snippet:  snippet(doc.Text, terms[0]),
			Score:    score,
		})
	}

	sort.SliceStable(results, func(a, b int) bool {
		if results[a].Score != results[b].Score {
			return results[a].Score > results[b].Score
		}
		return results[a].Path < results[b].Path
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// DocsForPage 返回页面覆盖信息与其关联的全部文档（未知页面 Status=unknown）。
func (i *Index) DocsForPage(pageKey string) (PageInfo, []Doc) {
	if i == nil {
		return PageInfo{PageKey: pageKey, Status: "unknown"}, nil
	}
	info, ok := i.pageByKey[pageKey]
	if !ok {
		return PageInfo{PageKey: pageKey, Status: "unknown", Docs: []string{}}, nil
	}
	docs := make([]Doc, 0, len(info.Docs))
	for _, path := range info.Docs {
		if doc, exists := i.docByPath[path]; exists {
			docs = append(docs, doc)
		}
	}
	return info, docs
}

// ReadDoc 按相对路径读取文档明文。
func (i *Index) ReadDoc(path string) (Doc, bool) {
	if i == nil {
		return Doc{}, false
	}
	doc, ok := i.docByPath[path]
	return doc, ok
}

// Gaps 返回未覆盖（missing/stale）的页面清单。
func (i *Index) Gaps() []PageInfo {
	if i == nil {
		return nil
	}
	gaps := make([]PageInfo, 0)
	for _, page := range i.pages {
		if page.Status == "ok" {
			continue
		}
		gaps = append(gaps, page)
	}
	return gaps
}

// Pages 返回全部页面覆盖信息（稳定顺序：注册表声明顺序；副本，调用方可安全排序/过滤）。
func (i *Index) Pages() []PageInfo {
	if i == nil {
		return nil
	}
	out := make([]PageInfo, len(i.pages))
	copy(out, i.pages)
	return out
}

// OrphanDocs 返回账本 orphan 文档（声明了不在注册表内的 page_key）。
func (i *Index) OrphanDocs() []OrphanDoc {
	if i == nil {
		return nil
	}
	out := make([]OrphanDoc, len(i.ledger.OrphanDocs))
	copy(out, i.ledger.OrphanDocs)
	return out
}

// Coverage 返回（覆盖率, 账本总计），与 Doc Ledger 同源。
func (i *Index) Coverage() (float64, map[string]int) {
	if i == nil {
		return 0, map[string]int{}
	}
	return i.ledger.Coverage, map[string]int{
		"pages":   i.ledger.Totals.Pages,
		"ok":      i.ledger.Totals.OK,
		"missing": i.ledger.Totals.Missing,
		"stale":   i.ledger.Totals.Stale,
		"orphan":  i.ledger.Totals.Orphan,
	}
}

// termScore 单词语义：标题/页面键命中权重高，正文命中权重低；
// 整词未命中且为多字 CJK 词（中文无空格查询）时退化为 2-gram 匹配（上限 4）——
// 仅对 CJK 生效：英文/混合词的 2-gram（如 "st"）会在英文文本里大面积误命中。
func termScore(term, lowerTitle, lowerText, lowerKeys string) int {
	score := 0
	if strings.Contains(lowerTitle, term) {
		score += 3
	}
	if strings.Contains(lowerKeys, term) {
		score += 3
	}
	if strings.Contains(lowerText, term) {
		score++
	}
	if score > 0 {
		return score
	}
	runes := []rune(term)
	if len(runes) < 2 || !containsCJK(runes) {
		return 0
	}
	hits := 0
	for i := 0; i+1 < len(runes); i++ {
		gram := string(runes[i : i+2])
		if strings.Contains(lowerTitle, gram) || strings.Contains(lowerText, gram) {
			hits++
		}
	}
	if hits > 4 {
		hits = 4
	}
	return hits
}

// containsCJK 判定词中是否含 CJK 字符（2-gram 回退的适用前提）。
func containsCJK(runes []rune) bool {
	for _, r := range runes {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// snippet 截取首个命中词附近的上下文（按 rune 截断，避免切断多字节字符）。
func snippet(text string, term string) string {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, term)
	if idx < 0 {
		return truncateRunes(strings.TrimSpace(text), 120)
	}
	start := idx - 40
	if start < 0 {
		start = 0
	}
	end := idx + len(term) + 80
	if end > len(text) {
		end = len(text)
	}
	return strings.TrimSpace(text[start:end])
}

func truncateRunes(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
