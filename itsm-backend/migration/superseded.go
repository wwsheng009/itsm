package migration

import (
	"context"
	"fmt"
	"strings"
)

// supersededRule 描述一条历史迁移的收养规则。
type supersededRule struct {
	// Replacement 为等价替代版本；为空表示无需替代（效果由 ent 基线/播种覆盖）。
	Replacement string
	// AdoptWhen 为条件 SQL（返回 boolean）。非空时仅当求值为 true 才收养
	// （用于「历史 schema 前提已不存在」的迁移）；为空时以 Replacement 是否存在为准。
	AdoptWhen string
}

// supersededMigrations 登记「在新库上必然失败、且已被 forward fix 迁移或播种等价覆盖」的历史版本。
//
// 背景（2026-10-12，FDB-B1 全新库实测）：20260501_rbac_endpoint_acls 面向历史单数表名
// （role / role_permission / permission_definition）与 default 租户 id=1 口径编写，在
// 全新库（ent 复数表名、default 租户非 1）上执行必然失败；而 RunMigrations 在首个失败
// 迁移处返回错误，导致其后全部文件系统迁移（quota / 审计 / 模板分发等）被静默跳过。
// 同类问题还存在于 20260616_security_role_permissions（依赖已删除的
// role_permissions.created_at/updated_at 旧列，其数据效果已由 seeder 角色权限播种覆盖）。
//
// 语义：adopt, don't replay —— 以空 checksum 补记入账（不执行），替代版本正常执行。
// 既有安装上已按原 checksum 记账的版本不受影响（recordMigrationAdopted 存在即跳过），
// 其 checksum 校验语义保持原样。
var supersededMigrations = map[string]supersededRule{
	"20260501_rbac_endpoint_acls": {
		Replacement: "20261012_rbac_endpoint_acls_fresh_schema",
	},
	"20260616_security_role_permissions": {
		AdoptWhen: "SELECT NOT EXISTS (SELECT 1 FROM information_schema.columns " +
			"WHERE table_name='role_permissions' AND column_name='created_at')",
	},
}

// reconcileSupersededMigrations 在迁移执行前收养被替代的历史版本。
// 幂等可重入：已记账（含既有安装 checksum 记账）→ 跳过；未记账 → 空 checksum 补记。
func (m *Migrator) reconcileSupersededMigrations(ctx context.Context, available []Migration) error {
	byVersion := make(map[string]Migration, len(available))
	for _, mig := range available {
		byVersion[mig.Version] = mig
	}
	for version, rule := range supersededMigrations {
		mig, ok := byVersion[version]
		if !ok {
			continue
		}
		if rule.Replacement != "" {
			if _, ok := byVersion[rule.Replacement]; !ok {
				// 替代版本不在可用集合（如旧版本部署）→ 不收养，保持原重放语义。
				continue
			}
		}
		if rule.AdoptWhen != "" {
			var adopt bool
			if err := m.db.QueryRowContext(ctx, rule.AdoptWhen).Scan(&adopt); err != nil {
				return fmt.Errorf("evaluate superseded condition for %s: %w", version, err)
			}
			if !adopt {
				continue
			}
		}
		recorded, err := recordMigrationAdopted(ctx, m.db, version, mig.Description, mig.RollbackSQL, "superseded")
		if err != nil {
			return err
		}
		if recorded && m.logger != nil {
			m.logger.Infow("superseded migration recorded (adopted, not replayed)",
				"version", version, "replacement", rule.Replacement)
		}
	}
	return nil
}

// legacyIncompatibleCutoff：该日期之前的文件系统迁移属「发现机制上线前」存量脚本，
// 在全新 ent 基线上可能引用已重命名/删除的表列（FDB-B1 实测：20260616_security_role_permissions
// 的 role_permissions.created_at、20260620_create_marketplace 的 created_at NOT NULL 违约等）。
const legacyIncompatibleCutoff = "20260901"

// legacyIncompatibleSQLStates：可安全判为「历史 schema 不兼容」的 SQLSTATE。
//   42P01 undefined_table / 42703 undefined_column / 23502 not_null_violation /
//   23505 unique_violation（重放历史脚本时基线已含同约束/数据）/ 42702 ambiguous_column /
//   P0001 raise_exception（历史脚本按固定 ID/旧数据形态的前置断言，如 parent /tickets (id=2) not found）
var legacyIncompatibleSQLStates = []string{"42P01", "42703", "23502", "23505", "42702", "P0001"}

// isLegacyVersion 判断是否为「发现机制上线前」的历史版本：无日期前缀（如
// add_missing_indexes）或日期 ≤ legacyIncompatibleCutoff。
func isLegacyVersion(version string) bool {
	if version == "" {
		return false
	}
	if version[0] < '0' || version[0] > '9' {
		return true
	}
	return version <= legacyIncompatibleCutoff
}

// isLegacySchemaError 判断迁移失败是否为「历史 SQL 与当前 schema 不兼容」类错误。
func isLegacySchemaError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range legacyIncompatibleSQLStates {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return strings.Contains(msg, "does not exist") || strings.Contains(msg, "violates not-null constraint")
}

// adoptLegacyIncompatible 在历史迁移因新 schema 不兼容失败时收养该版本（记录不重放），
// 避免 RunMigrations 在首个失败处中止其后全部迁移（F4 同类问题，2026-10-12）。
// 返回 (adopted, err)：adopted=false 表示不满足收养条件（调用方按原错误处理）。
func (m *Migrator) adoptLegacyIncompatible(ctx context.Context, mig Migration, applyErr error) (bool, error) {
	if !isLegacyVersion(mig.Version) || !isLegacySchemaError(applyErr) {
		return false, nil
	}
	recorded, err := recordMigrationAdopted(ctx, m.db, mig.Version, mig.Description, mig.RollbackSQL, "legacy-incompatible")
	if err != nil {
		return false, err
	}
	return recorded, nil
}
