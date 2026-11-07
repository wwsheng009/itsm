package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// 本文件提供「内置 BPMN 模板的权威键集合 + 确定性摘要」。
//
// ## 来源与边界（移植声明）
//
// 移植自上游 `heidsoft/itsm` 的 `service/bpmn_template_digest.go`（2026-11-03，仅标准库），
// 移植批次：`docs/plan/upstream-absorption-plan-2026-11-06.md` P2。
// **本地已具备前置**：`service/bpmn_template_service.go:24-25` 定义
// `//go:embed bpmn/*.bpmn` 的 `bpmnTemplates embed.FS`（本地 20 个 .bpmn 模板），
// 因此本文件无需新增 embed 声明、无需新增依赖、无需迁移。
// **本批未接线**：摘要目前只作为可调用的确定值存在，初始化组件 checksum/版本账本
// 是否纳入它，须另立 PR 决策（会改变账本 checksum，属行为变更）。
//
// 上游此文件**没有测试**；本地补 `bpmn_template_digest_test.go` 作可打破的行为锁。

// BuiltinProcessTemplateKeys returns the process definition keys that can
// actually be deployed, derived from the embedded BPMN templates (the same set
// LoadAndDeployTemplates writes into process_definitions). It is the single
// authority for "does this key have a template carrier"; callers must not
// maintain a second list of deployable keys.
func BuiltinProcessTemplateKeys() (map[string]bool, error) {
	entries, err := fs.ReadDir(bpmnTemplates, "bpmn")
	if err != nil {
		return nil, fmt.Errorf("read embedded bpmn templates: %w", err)
	}
	keys := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".bpmn" {
			continue
		}
		keys[strings.TrimSuffix(entry.Name(), ".bpmn")] = true
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("embedded bpmn templates are empty")
	}
	return keys, nil
}

// EmbeddedBPMNDigest returns a deterministic digest over the embedded BPMN
// template files. Initialization component checksums include it so editing a
// template is visible to the version ledger without a hand-maintained bump.
//
// 口径（与上游一致，勿擅自改）：把 `bpmn/` 下所有**非目录**条目按文件名排序，
// 逐条写入 `name\0len\0content`；因此非 `.bpmn` 文件若存在也会计入摘要。
func EmbeddedBPMNDigest() (string, error) {
	entries, err := fs.ReadDir(bpmnTemplates, "bpmn")
	if err != nil {
		return "", fmt.Errorf("read embedded bpmn templates: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	digest := sha256.New()
	for _, name := range names {
		content, err := bpmnTemplates.ReadFile("bpmn/" + name)
		if err != nil {
			return "", fmt.Errorf("read embedded bpmn template %s: %w", name, err)
		}
		fmt.Fprintf(digest, "%s\x00%d\x00", name, len(content))
		digest.Write(content)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
