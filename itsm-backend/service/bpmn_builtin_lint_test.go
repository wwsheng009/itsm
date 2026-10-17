package service

import (
	"io/fs"
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
