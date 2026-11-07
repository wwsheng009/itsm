package service

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuiltinBPMNTemplatesLint 内置 BPMN 模板全量 lint：
// error 级问题 = 部署后被门禁拦截（deployTemplate 同源实现），必须为 0。
// 该测试覆盖 service/bpmn/*.bpmn（含 PE-12 新增 project_initiation_flow / project_closure_flow）。
func TestBuiltinBPMNTemplatesLint(t *testing.T) {
	lint := NewBPMNLintService()
	checked := 0
	err := fs.WalkDir(bpmnTemplates, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".bpmn") {
			return nil
		}
		data, readErr := bpmnTemplates.ReadFile(path)
		require.NoError(t, readErr, "读取内置模板 %s 失败", path)
		result, lintErr := lint.LintBPMNXML(data)
		require.NoError(t, lintErr, "模板 %s XML 解析失败", path)
		assert.False(t, result.HasErrors, "模板 %s 存在 error 级 lint 问题: %+v", path, result.Issues)
		checked++
		return nil
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, checked, 10, "内置模板数量异常（应至少 10 个）")
}

// TestBuiltinBPMNTemplateKeyRegistryHasNoStaleCases 把 `bpmn_template_service.go`
// 里手写的 key→名称映射（switch case）挂到 BuiltinProcessTemplateKeys() 这个
// 唯一权威集合上：任何 case 引用了已被删除/改名的模板，CI 必须变红。
//
// 背景：本地 embed 有 20 个 .bpmn，而命名 switch 只有 11 个 case（其余走 default
// 用文件名兜底，属设计允许）；真正的风险方向是「case 指向不再存在的模板」——
// 这正是上游 BuiltinProcessTemplateKeys() 注释所说的「不许维护第二份清单」。
// 此用例是测试期接线，零运行时行为变化。
func TestBuiltinBPMNTemplateKeyRegistryHasNoStaleCases(t *testing.T) {
	src, err := os.ReadFile("bpmn_template_service.go")
	require.NoError(t, err, "测试工作目录应为 service 包目录")
	re := regexp.MustCompile(`(?m)^\s*case "([a-z0-9_]+)"\s*:`)
	matches := re.FindAllStringSubmatch(string(src), -1)
	require.NotEmpty(t, matches, "应能解析到命名映射的 case 列表（避免正则失效导致空断言假通过）")

	keys, err := BuiltinProcessTemplateKeys()
	require.NoError(t, err)
	for _, m := range matches {
		assert.True(t, keys[m[1]],
			"命名映射 case %q 已无 embed 模板载体（第二份清单漂移，需删 case 或补回模板）", m[1])
	}

	digest, err := EmbeddedBPMNDigest()
	require.NoError(t, err)
	assert.Len(t, digest, 64, "模板摘要应随预检可见，且为 sha256 hex")
}
