package migration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// OB-11 回归（2026-10-12，238 全新库生产部署发现）：
// 迁移中 `INSERT INTO process_bindings` 的流程定义 key 必须指向内置 BPMN 模板目录
// （service/bpmn/*.bpmn，go:embed）中真实存在的流程定义，或出现在 prune 迁移
// （20261013_prune_unbacked_process_bindings.sql）的清理清单里；
// 否则全新库 init 会在 workflow-core 校验（verifyWorkflowTemplates：绑定必须指向
// 存在的流程定义）失败 → 事务回滚 → compose 依赖链中止（站点 502）。
//
// 新增迁移若需要引入新绑定，必须同时提供对应模板（或显式加入 prune 清单并说明原因）。
func TestMigrationProcessBindingsReferenceBackedKeys(t *testing.T) {
	// 1) 内置模板 key 集合（文件名去扩展名）。
	tplDir := filepath.Join("..", "service", "bpmn")
	entries, err := os.ReadDir(tplDir)
	if err != nil {
		t.Fatalf("读取模板目录失败: %v", err)
	}
	templates := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".bpmn") {
			continue
		}
		templates[strings.TrimSuffix(e.Name(), ".bpmn")] = true
	}
	if len(templates) == 0 {
		t.Fatal("未找到任何内置 BPMN 模板（测试失效）")
	}

	// 2) prune 清单（与 20261013 迁移保持一致）。
	pruned := map[string]bool{
		"change_emergency_flow": true,
		"incident_general_flow": true,
		"release_test_flow":     true,
		"expense_approval_flow": true,
	}

	// 3) 扫描迁移 SQL：绑定 key 是插入值里唯一以 `_flow` 结尾的字符串字面量。
	keyRe := regexp.MustCompile(`'([a-z][a-z0-9_]*_flow)'`)
	files, err := filepath.Glob(filepath.Join("..", "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations 失败: %v", err)
	}
	scanned := 0
	for _, f := range files {
		base := filepath.Base(f)
		if strings.Contains(base, "_down") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", base, err)
		}
		text := string(data)
		if !strings.Contains(strings.ToLower(text), "process_bindings") {
			continue
		}
		for _, m := range keyRe.FindAllStringSubmatch(text, -1) {
			key := m[1]
			scanned++
			if templates[key] || pruned[key] {
				continue
			}
			t.Errorf("%s: process_bindings 引用未内置模板且未 prune 的 key: %s", base, key)
		}
	}
	if scanned == 0 {
		t.Fatal("未扫描到 process_bindings 的 *_flow 键（测试失效，请检查正则/文件）")
	}
}
