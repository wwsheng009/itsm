package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 上游 `bpmn_template_digest.go` 未附测试；本文件为本地补充（P2/P3 要求：
// 移植单元必须有「故意打破实现即变红」的用例，而不是装饰性断言）。
// 前置 `bpmnTemplates` 由本地 `bpmn_template_service.go` 的 go:embed 提供。

func TestBuiltinProcessTemplateKeysMatchEmbeddedFiles(t *testing.T) {
	keys, err := BuiltinProcessTemplateKeys()
	require.NoError(t, err)
	require.NotEmpty(t, keys, "内置模板不得为空集合")

	entries, err := fs.ReadDir(bpmnTemplates, "bpmn")
	require.NoError(t, err)
	expected := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".bpmn" {
			expected[strings.TrimSuffix(e.Name(), ".bpmn")] = true
		}
	}
	assert.Equal(t, expected, keys, "键集合必须严格等于 embed 里的 .bpmn 文件（不许手维护第二份清单）")

	for k := range keys {
		assert.NotContains(t, k, ".bpmn", "键不应带扩展名")
		_, err := fs.Stat(bpmnTemplates, "bpmn/"+k+".bpmn")
		require.NoError(t, err, "每个键都必须有真实模板载体 %s", k)
	}
}

func TestEmbeddedBPMNDigestIsDeterministicHex(t *testing.T) {
	a, err := EmbeddedBPMNDigest()
	require.NoError(t, err)
	b, err := EmbeddedBPMNDigest()
	require.NoError(t, err)
	assert.Equal(t, a, b, "同一份 embed 内容两次调用必须完全一致（版本账本要靠它判变更）")
	assert.Len(t, a, 64, "sha256 十六进制摘要长度固定 64")
	assert.Regexp(t, `^[0-9a-f]+$`, a)
}

// TestEmbeddedBPMNDigestMatchesIndependentCalculation 是「可打破」用例：
// 独立按 name\0len\0content 顺序重算一遍。若实现漏写文件内容、漏写长度、
// 改变排序或未包含非 .bpmn 条目，本用例都会变红。
func TestEmbeddedBPMNDigestMatchesIndependentCalculation(t *testing.T) {
	entries, err := fs.ReadDir(bpmnTemplates, "bpmn")
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	require.NotEmpty(t, names)

	h := sha256.New()
	for _, name := range names {
		content, err := bpmnTemplates.ReadFile("bpmn/" + name)
		require.NoError(t, err)
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(content))
		_, _ = h.Write(content)
	}
	want := hex.EncodeToString(h.Sum(nil))

	got, err := EmbeddedBPMNDigest()
	require.NoError(t, err)
	assert.Equal(t, want, got, "摘要口径必须与上游独立重算一致（含文件名/长度/内容/排序）")

	// 内容必须真的参与摘要：与「只哈希文件名」的值不同，防止实现偷懒后仍碰巧通过。
	nameOnly := sha256.New()
	for _, name := range names {
		fmt.Fprintf(nameOnly, "%s\x00", name)
	}
	assert.NotEqual(t, hex.EncodeToString(nameOnly.Sum(nil)), got, "文件内容必须参与摘要")
}
