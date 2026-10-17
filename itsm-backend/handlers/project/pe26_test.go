package project

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/auditlog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PE-26 采购/资产结项登记；权威设计：docs/project-management/plan/p2-procurement-design.md（D-49~D-53）。

// seedAssetLinkTargets 播种四域台账目标（同租户）并返回 ID。
func seedAssetLinkTargets(t *testing.T, client *ent.Client, tid int) (int, int, int, int) {
	t.Helper()
	ctx := context.Background()
	a, err := client.Asset.Create().
		SetTenantID(tid).SetAssetNumber("AST-PE26-1").SetName("交付笔记本").SetStatus("in-use").Save(ctx)
	require.NoError(t, err)
	l, err := client.AssetLicense.Create().
		SetTenantID(tid).SetName("Office 365").SetLicenseType("subscription").Save(ctx)
	require.NoError(t, err)
	v := seedPE26Vendor(t, client, tid, "云服务供应商", "VND-PE26-1")
	c, err := client.Contract.Create().
		SetTenantID(tid).SetContractNumber("CT-PE26-1").SetTitle("年度运维合同").SetContractType("维护合同").
		SetDescription("示例合同").
		SetStartDate(time.Now()).SetEndDate(time.Now().Add(24 * time.Hour)).Save(ctx)
	require.NoError(t, err)
	return a.ID, l.ID, v.ID, c.ID
}

// seedPE26Vendor 播种供应商（vendor 必填字段较多，集中一处）。
func seedPE26Vendor(t *testing.T, client *ent.Client, tid int, name, code string) *ent.Vendor {
	t.Helper()
	v, err := client.Vendor.Create().
		SetTenantID(tid).SetName(name).SetCode(code).SetVendorType("IT服务").
		SetContactName("联系人").SetContactEmail("vendor@example.com").
		SetContactPhone("13800000000").SetAddress("示例地址").SetWebsite("https://example.com").
		Save(context.Background())
	require.NoError(t, err)
	return v
}

// seedPE26Project 建项目 + 活跃成员，返回（项目, 成员）。
func seedPE26Project(t *testing.T, svc *Service, client *ent.Client, tid int, name string) (*ent.Project, *ent.User) {
	t.Helper()
	ctx := context.Background()
	member := seedUserWithMembership(t, client, tid, name, "home", nil)
	p, err := svc.Create(ctx, &CreateRequest{TenantID: tid, Name: name})
	require.NoError(t, err)
	_, err = svc.AddMember(ctx, tid, p.ID, member.ID, &MemberAddRequest{UserID: member.ID})
	require.NoError(t, err)
	return p, member
}

// TestAssetLinkWorkflow 登记 CRUD：词表/同租户弱引用校验/重复 409/软删后可重登记/终态口径/上限截断（PE-26）。
func TestAssetLinkWorkflow(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	p, member := seedPE26Project(t, svc, client, tid, "pe26-workflow")
	assetID, licenseID, vendorID, contractID := seedAssetLinkTargets(t, client, tid)

	// 空列表：四键摘要恒在（缺省 0）。
	empty, err := svc.ListAssetLinks(ctx, tid, p.ID, member.ID, false)
	require.NoError(t, err)
	assert.Empty(t, empty.Items)
	assert.Equal(t, 0, empty.Total)
	assert.False(t, empty.Truncated)
	for _, k := range assetLinkTypeOrder {
		assert.Equal(t, 0, empty.Summary[k], "摘要键 %s 恒存在", k)
	}

	// 新增（asset）：标签富化 + 摘要。
	list, err := svc.CreateAssetLink(ctx, tid, p.ID, member.ID, false, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeAsset, TargetID: assetID, Note: "交付笔记本",
	})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	item := list.Items[0]
	assert.Equal(t, AssetLinkTypeAsset, item.LinkType)
	assert.Equal(t, assetID, item.TargetID)
	assert.Equal(t, "AST-PE26-1", item.Label)
	assert.Contains(t, item.Sublabel, "交付笔记本")
	assert.False(t, item.TargetMissing)
	assert.Equal(t, 1, list.Summary[AssetLinkTypeAsset])
	assert.Equal(t, 1, list.Total)

	// 其余三类登记。
	for _, tc := range []struct {
		linkType string
		targetID int
		label    string
	}{
		{AssetLinkTypeAssetLicense, licenseID, "Office 365"},
		{AssetLinkTypeVendor, vendorID, "云服务供应商"},
		{AssetLinkTypeContract, contractID, "CT-PE26-1"},
	} {
		res, err := svc.CreateAssetLink(ctx, tid, p.ID, member.ID, false, &AssetLinkCreateRequest{
			LinkType: tc.linkType, TargetID: tc.targetID,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, res.Summary[tc.linkType])
		found := false
		for _, it := range res.Items {
			if it.LinkType == tc.linkType && it.TargetID == tc.targetID {
				assert.Equal(t, tc.label, it.Label)
				found = true
			}
		}
		assert.True(t, found, "类型 %s 应出现在列表中", tc.linkType)
	}
	list, err = svc.ListAssetLinks(ctx, tid, p.ID, member.ID, false)
	require.NoError(t, err)
	assert.Equal(t, 4, list.Total)

	// 重复（未删行唯一）→ ErrAssetLinkDuplicate；软删后可重登记。
	_, err = svc.CreateAssetLink(ctx, tid, p.ID, member.ID, false, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeAsset, TargetID: assetID,
	})
	require.ErrorIs(t, err, ErrAssetLinkDuplicate)

	// 词表/目标校验：非法类型、非正目标、不存在目标、跨租户目标。
	_, err = svc.CreateAssetLink(ctx, tid, p.ID, member.ID, false, &AssetLinkCreateRequest{
		LinkType: "server", TargetID: assetID,
	})
	require.ErrorIs(t, err, ErrAssetLinkInvalidType)
	_, err = svc.CreateAssetLink(ctx, tid, p.ID, member.ID, false, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeAsset, TargetID: 0,
	})
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.CreateAssetLink(ctx, tid, p.ID, member.ID, false, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeAsset, TargetID: 999999,
	})
	require.ErrorIs(t, err, ErrAssetLinkInvalidTarget)
	otherTenant, err := client.Tenant.Create().SetName("PE26 他租户").SetCode("PE26-OTHER").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	foreign, err := client.Asset.Create().
		SetTenantID(otherTenant.ID).SetAssetNumber("AST-OTHER").SetName("他租户资产").Save(ctx)
	require.NoError(t, err)
	_, err = svc.CreateAssetLink(ctx, tid, p.ID, member.ID, false, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeAsset, TargetID: foreign.ID,
	})
	require.ErrorIs(t, err, ErrAssetLinkInvalidTarget, "跨租户目标必须拒绝（D-50）")

	// 备注超长 → ErrValidation。
	_, err = svc.CreateAssetLink(ctx, tid, p.ID, member.ID, false, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeVendor, TargetID: 888888, Note: string(make([]rune, assetLinkNoteMaxRunes+1)),
	})
	require.ErrorIs(t, err, ErrValidation)

	// 软删：列表回落；二次删除 404 语义；重新登记成功（新行）。
	assetLinkID := 0
	for _, it := range list.Items {
		if it.LinkType == AssetLinkTypeAsset {
			assetLinkID = it.ID
		}
	}
	require.NotZero(t, assetLinkID)
	afterDelete, err := svc.DeleteAssetLink(ctx, tid, p.ID, assetLinkID, member.ID, false)
	require.NoError(t, err)
	assert.Equal(t, 3, afterDelete.Total)
	assert.Equal(t, 0, afterDelete.Summary[AssetLinkTypeAsset])
	_, err = svc.DeleteAssetLink(ctx, tid, p.ID, assetLinkID, member.ID, false)
	require.ErrorIs(t, err, ErrAssetLinkNotFound)
	list, err = svc.CreateAssetLink(ctx, tid, p.ID, member.ID, false, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeAsset, TargetID: assetID,
	})
	require.NoError(t, err, "软删后允许重新登记")
	assert.Equal(t, 1, list.Summary[AssetLinkTypeAsset])

	// 可见性：非成员（且非管理）不可见 → ErrNotFound（不泄露存在性）。
	outsider := seedUserWithMembership(t, client, tid, "pe26-out", "home", nil)
	_, err = svc.ListAssetLinks(ctx, tid, p.ID, outsider.ID, false)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = svc.CreateAssetLink(ctx, tid, p.ID, outsider.ID, false, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeVendor, TargetID: vendorID,
	})
	require.ErrorIs(t, err, ErrNotFound)

	// 终态口径（D-53）：cancelled 拒绝写；completed 允许补登记。
	require.NoError(t, client.Project.UpdateOneID(p.ID).SetStatus(StatusCancelled).Exec(ctx))
	_, err = svc.CreateAssetLink(ctx, tid, p.ID, member.ID, true, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeVendor, TargetID: vendorID,
	})
	require.ErrorIs(t, err, ErrValidation)
	_, err = svc.DeleteAssetLink(ctx, tid, p.ID, assetLinkID, member.ID, true)
	require.ErrorIs(t, err, ErrValidation)
	require.NoError(t, client.Project.UpdateOneID(p.ID).SetStatus(StatusCompleted).Exec(ctx))
	postCloseVendor := seedPE26Vendor(t, client, tid, "结项后供应商", "VND-PE26-POST")
	_, err = svc.CreateAssetLink(ctx, tid, p.ID, member.ID, true, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeVendor, TargetID: postCloseVendor.ID,
	})
	require.NoError(t, err, "completed 项目允许补登记（D-53）")

	// 硬上限：201 行 → 截断显式（200 + truncated）。
	p2, _ := seedPE26Project(t, svc, client, tid, "pe26-cap")
	for i := 0; i < assetLinkMaxItems+1; i++ {
		_, err := client.ProjectAssetLink.Create().
			SetTenantID(tid).SetProjectID(p2.ID).
			SetLinkType(AssetLinkTypeVendor).SetTargetID(100000 + i).
			SetCreatedBy(member.ID).Save(ctx)
		require.NoError(t, err)
	}
	capped, err := svc.ListAssetLinks(ctx, tid, p2.ID, member.ID, true)
	require.NoError(t, err)
	assert.True(t, capped.Truncated, "超出硬上限必须显式标记")
	assert.Equal(t, assetLinkMaxItems, capped.Total)
	assert.Len(t, capped.Items, assetLinkMaxItems)
}// TestAssetLinkCandidates 候选选择器：四类型 + 关键词 + 限额 + 词表/可见性（PE-26，D-52）。
func TestAssetLinkCandidates(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	ctx := context.Background()
	p, member := seedPE26Project(t, svc, client, tid, "pe26-candidates")
	assetID, licenseID, vendorID, contractID := seedAssetLinkTargets(t, client, tid)

	// 四类型均可查（标签/辅标签最小字段）。
	for _, tc := range []struct {
		linkType string
		wantID   int
		label    string
	}{
		{AssetLinkTypeAsset, assetID, "AST-PE26-1"},
		{AssetLinkTypeAssetLicense, licenseID, "Office 365"},
		{AssetLinkTypeVendor, vendorID, "云服务供应商"},
		{AssetLinkTypeContract, contractID, "CT-PE26-1"},
	} {
		res, err := svc.ListAssetLinkCandidates(ctx, tid, p.ID, member.ID, false, tc.linkType, "", 0)
		require.NoError(t, err)
		require.NotEmpty(t, res.Items, "类型 %s 应有候选", tc.linkType)
		found := false
		for _, it := range res.Items {
			if it.ID == tc.wantID {
				assert.Equal(t, tc.label, it.Label)
				found = true
			}
		}
		assert.True(t, found, "候选应包含目标 %d", tc.wantID)
	}

	// 关键词：大小写不敏感（ContainsFold）+ 编号/名称命中。
	res, err := svc.ListAssetLinkCandidates(ctx, tid, p.ID, member.ID, false, AssetLinkTypeVendor, "服务", 0)
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Equal(t, vendorID, res.Items[0].ID)

	res, err = svc.ListAssetLinkCandidates(ctx, tid, p.ID, member.ID, false, AssetLinkTypeAssetLicense, "office", 0)
	require.NoError(t, err)
	require.Len(t, res.Items, 1, "关键词匹配大小写不敏感")

	// 限额：limit 收窄 + truncated 显式。
	for i := 0; i < 3; i++ {
		seedPE26Vendor(t, client, tid, fmt.Sprintf("候选供应商%d", i), fmt.Sprintf("VND-PE26-%d", i+2))
	}
	res, err = svc.ListAssetLinkCandidates(ctx, tid, p.ID, member.ID, false, AssetLinkTypeVendor, "候选供应商", 2)
	require.NoError(t, err)
	assert.Len(t, res.Items, 2)
	assert.True(t, res.Truncated)

	// 非法类型 / 不可见项目。
	_, err = svc.ListAssetLinkCandidates(ctx, tid, p.ID, member.ID, false, "bogus", "", 0)
	require.ErrorIs(t, err, ErrAssetLinkInvalidType)
	outsider := seedUserWithMembership(t, client, tid, "pe26-cand-out", "home", nil)
	_, err = svc.ListAssetLinkCandidates(ctx, tid, p.ID, outsider.ID, false, AssetLinkTypeAsset, "", 0)
	require.ErrorIs(t, err, ErrNotFound)

	// 跨租户目标不出现在候选中。
	otherTenant, err := client.Tenant.Create().SetName("PE26 候选他租户").SetCode("PE26-CAND-OTHER").SetStatus("active").Save(ctx)
	require.NoError(t, err)
	_, err = client.Asset.Create().
		SetTenantID(otherTenant.ID).SetAssetNumber("AST-PE26-FOREIGN").SetName("他租户资产").Save(ctx)
	require.NoError(t, err)
	res, err = svc.ListAssetLinkCandidates(ctx, tid, p.ID, member.ID, false, AssetLinkTypeAsset, "FOREIGN", 0)
	require.NoError(t, err)
	assert.Empty(t, res.Items, "候选必须限定同租户（D-52）")
}

// TestAssetLinkClosureIntegration 结项清单集成：明细/摘要/detail 文案 + 门禁语义不变 + 目标缺失标记（PE-26，D-51）。
func TestAssetLinkClosureIntegration(t *testing.T) {
	svc, client, tid := newTestEnv(t)
	installClosurePorts(svc)
	ctx := context.Background()
	p, manager := seedPE26Project(t, svc, client, tid, "pe26-closure")
	assetID, licenseID, _, _ := seedAssetLinkTargets(t, client, tid)

	_, err := svc.CreateAssetLink(ctx, tid, p.ID, manager.ID, true, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeAsset, TargetID: assetID, Note: "两台",
	})
	require.NoError(t, err)
	_, err = svc.CreateAssetLink(ctx, tid, p.ID, manager.ID, true, &AssetLinkCreateRequest{
		LinkType: AssetLinkTypeAssetLicense, TargetID: licenseID,
	})
	require.NoError(t, err)

	// 未确认：待办 + detail 携带登记条数（门禁语义不变：仍为 pending）。
	view, err := svc.GetClosure(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	assert.Equal(t, ClosureItemPending, itemStatus(view, ClosureItemAssets))
	require.Len(t, view.AssetsLinks, 2)
	assert.Equal(t, 2, view.AssetsLinkSummary[AssetLinkTypeAsset]+view.AssetsLinkSummary[AssetLinkTypeAssetLicense])
	assetsDetail := ""
	for _, it := range view.Items {
		if it.Key == ClosureItemAssets {
			assetsDetail = it.Detail
		}
	}
	assert.Contains(t, assetsDetail, "已登记 2 条")
	assert.Contains(t, assetsDetail, "请确认或豁免")

	// 确认后：done + detail 携带登记条数；明细仍可读。
	view, err = svc.ConfirmAssets(ctx, tid, p.ID, manager.ID,
		&ClosureAssetsRequest{Action: "confirm", Note: "已登记台账"}, true)
	require.NoError(t, err)
	assert.Equal(t, ClosureItemDone, itemStatus(view, ClosureItemAssets))
	for _, it := range view.Items {
		if it.Key == ClosureItemAssets {
			assert.Contains(t, it.Detail, "已完成资产/许可登记确认")
			assert.Contains(t, it.Detail, "已登记 2 条")
		}
	}

	// 目标被删除后：登记行保留并标记 targetMissing（不阻断读）。
	require.NoError(t, client.Asset.DeleteOneID(assetID).Exec(ctx))
	view, err = svc.GetClosure(ctx, tid, p.ID, manager.ID, true)
	require.NoError(t, err)
	missing := 0
	for _, it := range view.AssetsLinks {
		if it.LinkType == AssetLinkTypeAsset {
			assert.True(t, it.TargetMissing, "目标缺失应显式标记")
			assert.Empty(t, it.Label)
			missing++
		}
	}
	assert.Equal(t, 1, missing)

	// 门禁语义不变（D-51）：零登记也可 confirm 通过。
	p2, manager2 := seedPE26Project(t, svc, client, tid, "pe26-closure-nolinks")
	view2, err := svc.ConfirmAssets(ctx, tid, p2.ID, manager2.ID,
		&ClosureAssetsRequest{Action: "confirm"}, true)
	require.NoError(t, err)
	assert.Equal(t, ClosureItemDone, itemStatus(view2, ClosureItemAssets))
	assert.Empty(t, view2.AssetsLinks)
}// TestAssetLinkEndpoints HTTP 端点：登记列表/新增/删除/候选 + 审计 + 错误码（PE-26）。
func TestAssetLinkEndpoints(t *testing.T) {
	r, client, tid := newHandlerEnv(t)
	ctx := context.Background()

	asset, err := client.Asset.Create().
		SetTenantID(tid).SetAssetNumber("AST-PE26-HTTP").SetName("HTTP 资产").SetStatus("available").Save(ctx)
	require.NoError(t, err)

	// 建项目。
	w, env := doJSON(t, r, http.MethodPost, "/projects", map[string]any{"name": "PE26 HTTP 项目"})
	require.Equal(t, http.StatusOK, w.Code)
	var created ProjectResponse
	require.NoError(t, json.Unmarshal(env.Data, &created))

	// 空列表。
	w, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/asset-links", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list AssetLinkListResponse
	require.NoError(t, json.Unmarshal(env.Data, &list))
	assert.Equal(t, common.SuccessCode, env.Code)
	assert.Empty(t, list.Items)

	// 新增登记（含备注）。
	w, env = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/asset-links", created.ID), map[string]any{
		"linkType": "asset", "targetId": asset.ID, "note": "结项移交",
	})
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(env.Data, &list))
	require.Len(t, list.Items, 1)
	assert.Equal(t, "AST-PE26-HTTP", list.Items[0].Label)
	assert.Equal(t, "结项移交", list.Items[0].Note)
	linkID := list.Items[0].ID

	// 重复登记 → 409；非法类型/不存在目标 → 400。
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/asset-links", created.ID), map[string]any{
		"linkType": "asset", "targetId": asset.ID,
	})
	assert.Equal(t, http.StatusConflict, w.Code)
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/asset-links", created.ID), map[string]any{
		"linkType": "server", "targetId": asset.ID,
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = doJSON(t, r, http.MethodPost, fmt.Sprintf("/projects/%d/asset-links", created.ID), map[string]any{
		"linkType": "asset", "targetId": 999999,
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 候选选择器（type 必填 + limit 校验）。
	w, env = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/asset-link-candidates?type=asset&q=HTTP", created.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var candidates AssetLinkCandidateListResponse
	require.NoError(t, json.Unmarshal(env.Data, &candidates))
	require.Len(t, candidates.Items, 1)
	assert.Equal(t, asset.ID, candidates.Items[0].ID)
	w, _ = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/asset-link-candidates?type=bogus", created.ID), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = doJSON(t, r, http.MethodGet, fmt.Sprintf("/projects/%d/asset-link-candidates?type=asset&limit=0", created.ID), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 删除（软删）→ 空列表；二次删除 404。
	w, env = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/projects/%d/asset-links/%d", created.ID, linkID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(env.Data, &list))
	assert.Empty(t, list.Items)
	w, _ = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/projects/%d/asset-links/%d", created.ID, linkID), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 不可见/不存在项目 → 404。
	w, _ = doJSON(t, r, http.MethodGet, "/projects/999999/asset-links", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 审计：create/delete 各留痕（resource=project），body 含类型/目标/标签。
	for _, action := range []string{"asset_link_create", "asset_link_delete"} {
		audits, err := client.AuditLog.Query().
			Where(auditlog.ResourceEQ("project"), auditlog.ActionEQ(action)).
			All(ctx)
		require.NoError(t, err)
		require.Len(t, audits, 1, "action=%s 应留痕一次", action)
		require.NotNil(t, audits[0].RequestBody)
		if action == "asset_link_create" {
			assert.Contains(t, *audits[0].RequestBody, "asset")
			assert.Contains(t, *audits[0].RequestBody, "AST-PE26-HTTP")
		}
	}
}