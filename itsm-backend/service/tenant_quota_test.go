package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/ent/tenant"
	"itsm-backend/ent/user"
	"itsm-backend/pkg/tenantquota"
)

// tenant_quota_test.go：IP-P2-6 租户硬配额——模型接入、用量口径与三条写入路径的拦截。
//
// 覆盖：
//  1. TenantQuotaService 用量/校验口径（含软删附件不计入、nil 安全、租户缺失 fail-closed）；
//  2. 工单创建 maxTicketsPerMonth → 422 TENANT_QUOTA_EXCEEDED；
//  3. 三通道建号 maxUsers → ProvisionError(TENANT_QUOTA_EXCEEDED, 422)；
//  4. 附件上传 maxStorageMB → 既有 6106 语义（ErrAttachmentQuotaExceeded）且不落盘。

func newTenantQuotaFixture(t *testing.T) (*ent.Client, *TenantQuotaService) {
	t.Helper()
	dsn := fmt.Sprintf("file:tenant_quota_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	return client, NewTenantQuotaService(client, zaptest.NewLogger(t).Sugar())
}

func mkQuotaTenant(t *testing.T, client *ent.Client, code string, limits tenantquota.Limits) *ent.Tenant {
	t.Helper()
	tn, err := client.Tenant.Create().
		SetName("配额租户 " + code).
		SetCode(code).
		SetType(tenant.TypeMspCustomer).
		SetStatus("active").
		SetQuota(limits).
		Save(context.Background())
	require.NoError(t, err)
	return tn
}

func mkQuotaUser(t *testing.T, client *ent.Client, tenantID int, name string) *ent.User {
	t.Helper()
	u, err := client.User.Create().
		SetUsername(name).SetEmail(name + "@example.com").SetName(name).
		SetPasswordHash("hash").SetRole(user.RoleAgent).SetActive(true).
		SetTenantID(tenantID).
		Save(context.Background())
	require.NoError(t, err)
	return u
}

func mkQuotaTicket(t *testing.T, client *ent.Client, tenantID, requesterID int, number string) *ent.Ticket {
	t.Helper()
	tk, err := client.Ticket.Create().
		SetTicketNumber(number).SetTitle("配额工单 " + number).
		SetType("incident").SetPriority("medium").SetStatus("open").
		SetRequesterID(requesterID).SetTenantID(tenantID).
		Save(context.Background())
	require.NoError(t, err)
	return tk
}

func mkQuotaAttachment(t *testing.T, client *ent.Client, tenantID, size int, status string) *ent.Attachment {
	t.Helper()
	att, err := client.Attachment.Create().
		SetTenantID(tenantID).
		SetBizType("ticket").SetBizID(1).
		SetFileName("seed.bin").SetFilePath(fmt.Sprintf("seed/%d.bin", time.Now().UnixNano())).
		SetFileSize(size).SetFileType("application/octet-stream").
		SetUploadedBy(1).SetStatus(status).
		Save(context.Background())
	require.NoError(t, err)
	return att
}

// TestTenantQuotaService_UsageAndChecks 锁定用量口径与三键校验语义。
func TestTenantQuotaService_UsageAndChecks(t *testing.T) {
	client, svc := newTenantQuotaFixture(t)
	ctx := context.Background()

	tn := mkQuotaTenant(t, client, "quota-usage", tenantquota.Limits{
		MaxUsers: 2, MaxTicketsPerMonth: 3, MaxStorageMB: 1,
	})
	u := mkQuotaUser(t, client, tn.ID, "quota-usage-u1")
	mkQuotaTicket(t, client, tn.ID, u.ID, "QT-1")
	mkQuotaAttachment(t, client, tn.ID, 100, "active")
	mkQuotaAttachment(t, client, tn.ID, 50, "active")
	// 软删附件（status=deleted）不计入存储用量。
	_, err := client.Attachment.UpdateOneID(mkQuotaAttachment(t, client, tn.ID, 999, "deleted").ID).
		SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)

	usage, err := svc.Usage(ctx, tn.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, usage.Users)
	assert.EqualValues(t, 1, usage.TicketsThisMonth)
	assert.EqualValues(t, 150, usage.StorageBytes)

	// 建号：limit=2，已有 1 → 放行；补到 2 后再建 → 超限。
	require.NoError(t, svc.CheckUserCreate(ctx, tn.ID))
	mkQuotaUser(t, client, tn.ID, "quota-usage-u2")
	uerr := svc.CheckUserCreate(ctx, tn.ID)
	require.Error(t, uerr)
	qe, ok := tenantquota.AsExceeded(uerr)
	require.True(t, ok)
	assert.Equal(t, tenantquota.QuotaMaxUsers, qe.Quota)
	assert.EqualValues(t, 2, qe.Limit)

	// 工单：limit=3，已有 1 → 放行；补齐到 3 后再建 → 超限。
	require.NoError(t, svc.CheckTicketCreate(ctx, tn.ID))
	mkQuotaTicket(t, client, tn.ID, u.ID, "QT-2")
	mkQuotaTicket(t, client, tn.ID, u.ID, "QT-3")
	terr := svc.CheckTicketCreate(ctx, tn.ID)
	require.Error(t, terr)
	qe, ok = tenantquota.AsExceeded(terr)
	require.True(t, ok)
	assert.Equal(t, tenantquota.QuotaMaxTicketsPerMonth, qe.Quota)

	// 存储：limit=1MB；已用 150B + 1MB → 超限；+1B → 放行。
	serr := svc.CheckStorageAdd(ctx, tn.ID, 1<<20)
	require.Error(t, serr)
	qe, ok = tenantquota.AsExceeded(serr)
	require.True(t, ok)
	assert.Equal(t, tenantquota.QuotaMaxStorageMB, qe.Quota)
	require.NoError(t, svc.CheckStorageAdd(ctx, tn.ID, 1))
}

// TestTenantQuotaService_FailClosedAndNilSafe 缺失租户 fail-closed；未接入路径 nil 安全。
func TestTenantQuotaService_FailClosedAndNilSafe(t *testing.T) {
	client, svc := newTenantQuotaFixture(t)
	ctx := context.Background()

	_, err := svc.LimitsOf(ctx, 99999)
	require.Error(t, err, "租户不存在时必须拒绝（fail-closed）")
	require.Error(t, svc.CheckUserCreate(ctx, 99999))

	var nilSvc *TenantQuotaService
	require.NoError(t, nilSvc.CheckUserCreate(ctx, 1))
	require.NoError(t, nilSvc.CheckTicketCreate(ctx, 1))
	require.NoError(t, nilSvc.CheckStorageAdd(ctx, 1, 1))

	// 未配置配额（零值）= 全部放行。
	plain := mkQuotaTenant(t, client, "quota-none", tenantquota.Limits{})
	require.NoError(t, svc.CheckUserCreate(ctx, plain.ID))
	require.NoError(t, svc.CheckTicketCreate(ctx, plain.ID))
	require.NoError(t, svc.CheckStorageAdd(ctx, plain.ID, 1<<40))
}

// TestTicketService_QuotaBlocksCreation 工单创建接入 maxTicketsPerMonth。
func TestTicketService_QuotaBlocksCreation(t *testing.T) {
	client, quota := newTenantQuotaFixture(t)
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	tn := mkQuotaTenant(t, client, "quota-ticket", tenantquota.Limits{MaxTicketsPerMonth: 1})
	usr := mkQuotaUser(t, client, tn.ID, "quota-ticket-u1")

	svc := NewTicketServiceForTest(client, logger)
	svc.SetTenantQuotaService(quota)

	_, err := svc.CreateTicket(ctx, &dto.CreateTicketRequest{Title: "第一张", Priority: "medium", RequesterID: usr.ID}, tn.ID)
	require.NoError(t, err)

	_, err = svc.CreateTicket(ctx, &dto.CreateTicketRequest{Title: "第二张", Priority: "medium", RequesterID: usr.ID}, tn.ID)
	require.Error(t, err)
	qe, ok := tenantquota.AsExceeded(err)
	require.True(t, ok, "超限必须返回 *tenantquota.ExceededError（handler 映射 422）")
	assert.Equal(t, tenantquota.QuotaMaxTicketsPerMonth, qe.Quota)
}

// TestProvisioningService_QuotaBlocksUserCreation 三通道建号接入 maxUsers。
func TestProvisioningService_QuotaBlocksUserCreation(t *testing.T) {
	client, quota := newTenantQuotaFixture(t)
	ctx := context.Background()
	logger := zaptest.NewLogger(t).Sugar()

	provider := mkQuotaTenant(t, client, "quota-prov", tenantquota.Limits{MaxUsers: 1})
	platform := mkQuotaTenant(t, client, "quota-platform", tenantquota.Limits{})

	prov := NewUserProvisioningService(client, NewUserService(client, logger), logger)
	prov.SetChannelsEnabled(true)
	prov.SetTenantQuotaService(quota)

	actor := ProvisionActor{UserID: 1, HomeTenantID: platform.ID, Role: "super_admin", Username: "root"}
	mkReq := func(name string) *dto.CreateUserRequest {
		return &dto.CreateUserRequest{
			Username: name, Email: name + "@corp.example.com",
			Name: name, Password: "Str0ng-P@ssw0rd!", Role: "admin",
		}
	}

	_, err := prov.ProvisionUser(ctx, actor, provider.ID, mkReq("quota-prov-admin"))
	require.NoError(t, err)

	_, err = prov.ProvisionUser(ctx, actor, provider.ID, mkReq("quota-prov-admin-2"))
	require.Error(t, err)
	pe, ok := AsProvisionError(err)
	require.True(t, ok, "建号超限必须返回稳定 ProvisionError")
	assert.Equal(t, tenantquota.CodeTenantQuotaExceeded, pe.Code)
	assert.Equal(t, 422, pe.Status)
}

// TestAttachmentUpload_QuotaBlocks 附件上传接入 maxStorageMB（6106 语义 + 不落盘）。
func TestAttachmentUpload_QuotaBlocks(t *testing.T) {
	svc, client, root := newAttachmentTestService(t)
	ctx := context.Background()
	tn, usr := seedHost(t, client, "quota")
	tk := createTestTicket(t, client, tn, usr, "T-QUOTA-1")

	// 配额 1MB；预置 2MB 有效附件 → 任意新增上传均超限。
	require.NoError(t, func() error {
		_, err := client.Tenant.UpdateOneID(tn.ID).
			SetQuota(tenantquota.Limits{MaxStorageMB: 1}).Save(ctx)
		return err
	}())
	mkQuotaAttachment(t, client, tn.ID, 2<<20, "active")
	svc.SetTenantQuotaService(NewTenantQuotaService(client, zaptest.NewLogger(t).Sugar()))

	_, err := svc.Upload(ctx, tn.ID, usr.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket,
		BizID:   tk.ID,
		File:    pngHeader("quota.png", pngMagic),
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAttachmentQuotaExceeded), "配额超限必须沿用 6106 语义，实际: %v", err)
	assert.Equal(t, 0, countFilesUnder(t, root), "配额超限时不得落盘")

	// 放宽配额后同一调用成功（正例回归，证明拦截点只由配额驱动）。
	_, err = client.Tenant.UpdateOneID(tn.ID).
		SetQuota(tenantquota.Limits{MaxStorageMB: 8}).Save(ctx)
	require.NoError(t, err)
	_, err = svc.Upload(ctx, tn.ID, usr.ID, AttachmentUploadInput{
		BizType: AttachmentBizTypeTicket,
		BizID:   tk.ID,
		File:    pngHeader("quota-ok.png", pngMagic),
	})
	require.NoError(t, err)
}
