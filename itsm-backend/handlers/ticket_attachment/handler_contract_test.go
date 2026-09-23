package ticket_attachment

// handler_contract_test.go 承载 BE-6 验收（方案 §8「旧工单端点逐字段 JSON 对照」，AC-2）：
//
//  1. TestLegacyTicketAttachmentContract_FrozenWithoutGenericBackend
//     开关全关（= 改造前链路）时，列表/上传/下载/预览/删除的状态码与 JSON 字段被冻结：
//     envelope 键、data 键集合、逐字段取值口径（fileUrl 形态、filePath 归属等）。
//  2. TestLegacyTicketAttachmentContract_GenericBackendFieldParity
//     同一对端点、同一数据库、同一用户：开关关闭时产出的响应与开关打开（通用后端）后
//     产出的响应逐字段对照，只允许 id / filePath / fileUrl / createdAt 取值不同，
//     且这四个字段各有独立形态断言（身份/存储字段，见 contractValueAllowlist）。
//  3. TestLegacyTicketAttachmentContract_ErrorCodeParity
//     错误路径（宿主不存在 / 附件不存在 / 类型拒绝 / 无认证上下文）在两种模式下
//     状态码、业务码、可读文案完全一致。
//
// 依赖：enttest(sqlite3) 内存库；t.Chdir 把旧链路落盘目录（CWD 相对 uploads/tickets）
// 重定向到临时目录，避免污染仓库工作区。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// contractPNG 标准 PNG magic bytes + 可辨识载荷：嗅探结果稳定为 image/png。
var contractPNG = append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte("ticket-attachment-contract-payload")...)

// contractFileName 含中日韩字符与空格，覆盖清洗逻辑与 Content-Disposition 编码。
const contractFileName = "兼容 测试.png"

// goldenTicketAttachmentFields 冻结 dto.TicketAttachmentResponse 的 JSON 键集合
// （按字典序排列，与 sortedJSONKeys 的比对口径一致）。
// 任何新增/删除字段都会让本用例失败——旧端点契约不接受静默变更。
var goldenTicketAttachmentFields = []string{
	"createdAt", "fileName", "filePath", "fileSize", "fileType", "fileUrl",
	"id", "mimeType", "ticketId", "uploadedBy", "uploader",
}

// goldenTicketUploaderFields 冻结 dto.UserInfo 的 JSON 键集合（字典序）。
var goldenTicketUploaderFields = []string{
	"department", "email", "id", "name", "role", "tenantId", "username",
}

// contractEnvelope 统一响应体（common.Success / common.Fail）。
type contractEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// contractFileURLPattern 规范访问地址的历史形态：/api/v1/tickets/{ticketId}/attachments/{id}/preview
var contractFileURLPattern = regexp.MustCompile(`^/api/v1/tickets/(\d+)/attachments/(\d+)/preview$`)

// contractFlags 可变灰度开关：同一进程内切换「旧链路 / 通用后端」以做逐字段对照。
type contractFlags struct{ read, write bool }

func (f *contractFlags) GenericReadEnabled(int) bool  { return f.read }
func (f *contractFlags) GenericWriteEnabled(int) bool { return f.write }

type contractEnv struct {
	t           *testing.T
	router      *gin.Engine // 注入 tenant_id/user_id（模拟认证中间件）
	noAuthRouter *gin.Engine // 只有权限上下文、没有认证上下文，用于「认证信息缺失」对照
	svc         *service.TicketAttachmentService
	client      *ent.Client
	tenant      *ent.Tenant
	user        *ent.User
	ticket      *ent.Ticket
	flags       *contractFlags
}

func newContractEnv(t *testing.T) *contractEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// 旧链路落盘目录是相对 CWD 的 `uploads/tickets`（NewTicketAttachmentService），
	// 重定向到临时目录，保证用例不往仓库工作区写文件。
	t.Chdir(t.TempDir())

	dsn := fmt.Sprintf("file:ticket_attachment_contract_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	tn, err := client.Tenant.Create().
		SetName("旧工单附件契约测试租户").
		SetCode(fmt.Sprintf("ticket-attach-contract-%d", time.Now().UnixNano())).
		SetDomain("ticket-attach-contract.test").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	u, err := client.User.Create().
		SetUsername(fmt.Sprintf("ticket-attach-contract-user-%d", tn.ID)).
		SetEmail("ticket-attach-contract@example.com").
		SetName("旧工单附件契约测试用户").
		SetPasswordHash("hashed-for-test").
		SetRole("agent").
		SetActive(true).
		SetTenantID(tn.ID).
		Save(ctx)
	require.NoError(t, err)
	tk, err := client.Ticket.Create().
		SetTitle("旧工单附件契约测试工单").
		SetTicketNumber(fmt.Sprintf("T-CONTRACT-%d", tn.ID)).
		SetRequesterID(u.ID).
		SetTenantID(tn.ID).
		Save(ctx)
	require.NoError(t, err)

	logger := zaptest.NewLogger(t).Sugar()
	svc := service.NewTicketAttachmentService(client, logger)
	generic := service.NewAttachmentService(client, logger, service.NewLocalStorageProvider(t.TempDir()))
	flags := &contractFlags{}
	svc.SetGenericBackend(generic, flags)
	h := NewHandler(svc, logger)

	authed := gin.New()
	authed.Use(func(c *gin.Context) {
		c.Set("tenant_id", tn.ID)
		c.Set("user_id", u.ID)
		c.Set("role", "super_admin") // 权限中间件直通：本用例只断言契约，不覆盖 RBAC 表
		c.Set("client", client)
		c.Next()
	})
	registerContractRoutes(authed.Group("/api/v1/tickets"), h)

	// 无认证上下文：handler 自身应给出「认证信息缺失」（AuthFailedCode），
	// 该分支与后端开关无关，两种模式下必须完全一致。
	noAuth := gin.New()
	noAuth.Use(func(c *gin.Context) {
		c.Set("role", "super_admin")
		c.Set("client", client)
		c.Next()
	})
	registerContractRoutes(noAuth.Group("/api/v1/tickets"), h)

	return &contractEnv{
		t: t, router: authed, noAuthRouter: noAuth,
		svc: svc, client: client, tenant: tn, user: u, ticket: tk, flags: flags,
	}
}

// registerContractRoutes 与 router/ticket_routes.go 的旧工单附件端点同构
// （路径与权限码逐条一致：read / create / read / read / read / delete）。
func registerContractRoutes(tickets *gin.RouterGroup, h *Handler) {
	tickets.GET("/:id/attachments", middleware.RequirePermission("ticket", "read"), h.ListTicketAttachments)
	tickets.POST("/:id/attachments", middleware.RequirePermission("ticket", "create"), h.UploadAttachment)
	tickets.GET("/:id/attachments/:attachment_id", middleware.RequirePermission("ticket", "read"), h.DownloadAttachment)
	tickets.GET("/:id/attachments/:attachment_id/download", middleware.RequirePermission("ticket", "read"), h.DownloadAttachment)
	tickets.GET("/:id/attachments/:attachment_id/preview", middleware.RequirePermission("ticket", "read"), h.PreviewAttachment)
	tickets.DELETE("/:id/attachments/:attachment_id", middleware.RequirePermission("ticket", "delete"), h.DeleteAttachment)
}

// ---------------------------------------------------------------------------
// 请求夹具
// ---------------------------------------------------------------------------

func (e *contractEnv) do(engine *gin.Engine, method, target, contentType string, body io.Reader) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func contractMultipart(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body, writer.FormDataContentType()
}

// uploadPNG 通过旧端点上传契约样张（filename 可覆盖，用于类型拒绝对照）。
func (e *contractEnv) uploadPNG(filename string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.uploadPNGTo(e.ticket.ID, filename)
}

// uploadPNGTo 上传契约样张到指定工单（跨宿主隔离用例需要第二个工单）。
func (e *contractEnv) uploadPNGTo(ticketID int, filename string) *httptest.ResponseRecorder {
	e.t.Helper()
	body, contentType := contractMultipart(e.t, filename, contractPNG)
	return e.do(e.router, http.MethodPost, fmt.Sprintf("/api/v1/tickets/%d/attachments", ticketID), contentType, body)
}

func (e *contractEnv) uploadRaw(filename string, content []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.uploadRawTo(e.ticket.ID, filename, content)
}

func (e *contractEnv) uploadRawTo(ticketID int, filename string, content []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	body, contentType := contractMultipart(e.t, filename, content)
	return e.do(e.router, http.MethodPost, fmt.Sprintf("/api/v1/tickets/%d/attachments", ticketID), contentType, body)
}

func (e *contractEnv) list() *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do(e.router, http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d/attachments", e.ticket.ID), "", nil)
}

func (e *contractEnv) download(ref string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.downloadFor(e.ticket.ID, ref)
}

// downloadFor 以指定工单的路径下载附件（宿主归属断言的构造入口）。
func (e *contractEnv) downloadFor(ticketID int, ref string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do(e.router, http.MethodGet,
		fmt.Sprintf("/api/v1/tickets/%d/attachments/%s", ticketID, url.PathEscape(ref)), "", nil)
}

func (e *contractEnv) preview(ref string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do(e.router, http.MethodGet, e.attachmentPath(ref)+"/preview", "", nil)
}

func (e *contractEnv) remove(ref string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do(e.router, http.MethodDelete, e.attachmentPath(ref), "", nil)
}

// attachmentPath 生成附件引用路径：引用可能是历史存储文件名（含中日韩字符与空格，
// 真实前端按 URL 编码发起请求），必须按路径段转义后再构造请求。
func (e *contractEnv) attachmentPath(ref string) string {
	return fmt.Sprintf("/api/v1/tickets/%d/attachments/%s", e.ticket.ID, url.PathEscape(ref))
}

// ---------------------------------------------------------------------------
// 解码与断言辅助
// ---------------------------------------------------------------------------

func decodeContractEnvelope(t *testing.T, w *httptest.ResponseRecorder) contractEnvelope {
	t.Helper()
	var env contractEnvelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), "body=%s", w.Body.String())
	return env
}

// decodeContractObject 解析 data 为对象，并返回排序后的键集合（键集合本身即契约）。
func decodeContractObject(t *testing.T, w *httptest.ResponseRecorder) (contractEnvelope, map[string]any, []string) {
	t.Helper()
	env := decodeContractEnvelope(t, w)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &obj), "data=%s", string(env.Data))
	return env, obj, sortedJSONKeys(obj)
}

// decodeContractList 解析列表端点 data：{"attachments":[...],"total":n}。
func decodeContractList(t *testing.T, w *httptest.ResponseRecorder) (contractEnvelope, []map[string]any, int, []string) {
	t.Helper()
	env := decodeContractEnvelope(t, w)
	var payload struct {
		Attachments []map[string]any `json:"attachments"`
		Total       int              `json:"total"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &payload), "data=%s", string(env.Data))
	var top map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &top), "data=%s", string(env.Data))
	return env, payload.Attachments, payload.Total, sortedJSONKeys(top)
}

func sortedJSONKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// contractValueAllowlist 明确列出「同一逻辑附件在两种后端下允许取值不同」的字段，
// 每个字段仍带独立形态断言。除此之外任何字段取值不同都判失败（逐字段对照）。
func contractValueAllowlist(t *testing.T, ticketID int) map[string]func(want, got any) {
	t.Helper()
	number := func(v any) int {
		f, ok := v.(float64)
		require.True(t, ok, "期望数值，实际 %#v", v)
		return int(f)
	}
	return map[string]func(want, got any){
		// 身份字段：两条记录各自的自增主键，必须为正整数，且与自身 fileUrl 一致。
		"id": func(want, got any) {
			require.Positive(t, number(want))
			require.Positive(t, number(got))
		},
		// 存储字段：旧链路为 uploads/tickets/<ts>_<name>，通用链路为
		// {tenant}/{bizType}/{bizId}/{uuid}.{ext}；均为非空字符串（前端不消费该值）。
		"filePath": func(want, got any) {
			require.NotEmpty(t, want.(string))
			require.NotEmpty(t, got.(string))
		},
		// 访问地址：两条链路都必须输出历史 preview 形态（D5：历史 URL 永不失效）。
		"fileUrl": func(want, got any) {
			for _, v := range []any{want, got} {
				m := contractFileURLPattern.FindStringSubmatch(v.(string))
				require.Len(t, m, 3, "fileUrl 必须保持历史 preview 形态，实际 %v", v)
				require.Equal(t, strconv.Itoa(ticketID), m[1], "fileUrl 中的工单 ID")
				require.NotEqual(t, "0", m[2], "fileUrl 必须携带有效附件 ID")
			}
		},
		// 时间字段：格式必须同为 RFC3339(Nano)。
		"createdAt": func(want, got any) {
			_, err := time.Parse(time.RFC3339Nano, want.(string))
			require.NoError(t, err)
			_, err = time.Parse(time.RFC3339Nano, got.(string))
			require.NoError(t, err)
		},
	}
}

// assertContractParity 逐字段对照两个响应对象：键集合、取值、嵌套 uploader 全量相等；
// allowlist 内的字段改为形态断言。
func assertContractParity(t *testing.T, want, got map[string]any, allow map[string]func(want, got any)) {
	t.Helper()
	require.Equal(t, sortedJSONKeys(want), sortedJSONKeys(got), "JSON 字段集合必须逐字段一致")

	for _, k := range sortedJSONKeys(want) {
		if check, ok := allow[k]; ok {
			check(want[k], got[k])
			continue
		}
		require.Equal(t, want[k], got[k], "字段 %s 取值必须一致", k)
	}

	// uploader 是嵌套对象：同一数据库同一用户，必须逐字段相等（含 id）。
	wantUploader, wOK := want["uploader"].(map[string]any)
	gotUploader, gOK := got["uploader"].(map[string]any)
	require.Equal(t, wOK, gOK, "uploader 字段存在性必须一致")
	if wOK && gOK {
		require.Equal(t, goldenTicketUploaderFields, sortedJSONKeys(wantUploader), "uploader 字段集合")
		require.Equal(t, goldenTicketUploaderFields, sortedJSONKeys(gotUploader), "uploader 字段集合")
		for _, k := range goldenTicketUploaderFields {
			require.Equal(t, wantUploader[k], gotUploader[k], "uploader.%s 取值必须一致", k)
		}
	}
}

// ---------------------------------------------------------------------------
// 1. 改造前链路：字段与状态码冻结
// ---------------------------------------------------------------------------

func TestLegacyTicketAttachmentContract_FrozenWithoutGenericBackend(t *testing.T) {
	env := newContractEnv(t)
	require.False(t, env.flags.read, "前置条件：通用读开关关闭（= 改造前链路）")
	require.False(t, env.flags.write, "前置条件：通用写开关关闭（= 改造前链路）")

	// --- 上传：200 + SuccessCode + 冻结字段集合 ---
	uploadW := env.uploadPNG(contractFileName)
	require.Equal(t, http.StatusOK, uploadW.Code, uploadW.Body.String())
	uploadEnv, data, keys := decodeContractObject(t, uploadW)
	require.Equal(t, common.SuccessCode, uploadEnv.Code)
	require.Equal(t, goldenTicketAttachmentFields, keys, "上传响应 data 字段集合被冻结")

	uploader, ok := data["uploader"].(map[string]any)
	require.True(t, ok, "uploader 必须存在（旧实现带 WithUploader）")
	require.Equal(t, goldenTicketUploaderFields, sortedJSONKeys(uploader))

	attachmentID := int(data["id"].(float64))
	require.Positive(t, attachmentID)
	require.Equal(t, float64(env.ticket.ID), data["ticketId"])
	require.Equal(t, contractFileName, data["fileName"])
	require.Equal(t, float64(len(contractPNG)), data["fileSize"])
	require.Equal(t, "image/png", data["fileType"])
	require.Equal(t, "image/png", data["mimeType"])
	require.Equal(t, float64(env.user.ID), data["uploadedBy"])
	require.Equal(t,
		fmt.Sprintf("/api/v1/tickets/%d/attachments/%d/preview", env.ticket.ID, attachmentID),
		data["fileUrl"], "规范访问地址固定为历史 preview 形态")
	require.Contains(t, data["filePath"].(string), "uploads",
		"旧链路 filePath 为 uploads/tickets 下的落盘路径")
	_, err := time.Parse(time.RFC3339Nano, data["createdAt"].(string))
	require.NoError(t, err)

	// --- 列表：{attachments,total} ---
	listW := env.list()
	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	listEnv, attachments, total, listKeys := decodeContractList(t, listW)
	require.Equal(t, common.SuccessCode, listEnv.Code)
	require.Equal(t, []string{"attachments", "total"}, listKeys)
	require.Equal(t, 1, total)
	require.Len(t, attachments, 1)
	require.Equal(t, goldenTicketAttachmentFields, sortedJSONKeys(attachments[0]))
	require.Equal(t, data["id"], attachments[0]["id"])
	require.Equal(t, data["fileUrl"], attachments[0]["fileUrl"])

	ref := strconv.Itoa(attachmentID)
	legacyRef := filepath.Base(data["filePath"].(string))

	// --- 下载 / 预览：内容与响应头 ---
	downloadW := env.download(ref)
	require.Equal(t, http.StatusOK, downloadW.Code)
	require.Equal(t, "image/png", downloadW.Header().Get("Content-Type"))
	require.Contains(t, downloadW.Header().Get("Content-Disposition"), "attachment")
	require.Equal(t, contractPNG, downloadW.Body.Bytes())

	previewW := env.preview(ref)
	require.Equal(t, http.StatusOK, previewW.Code)
	require.Equal(t, "image/png", previewW.Header().Get("Content-Type"))
	require.Contains(t, previewW.Header().Get("Content-Disposition"), "inline")
	require.Equal(t, contractPNG, previewW.Body.Bytes())

	// 历史形态引用（存储文件名，D5：曾被写入富文本的 URL 永不失效）
	require.Equal(t, http.StatusOK, env.download(legacyRef).Code)
	require.Equal(t, contractPNG, env.download(legacyRef).Body.Bytes())

	// --- 删除：200 + SuccessCode + data:null ---
	deleteW := env.remove(ref)
	require.Equal(t, http.StatusOK, deleteW.Code, deleteW.Body.String())
	deleteEnv := decodeContractEnvelope(t, deleteW)
	require.Equal(t, common.SuccessCode, deleteEnv.Code)
	require.Empty(t, strings.TrimSpace(string(deleteEnv.Data)), "删除响应 data 缺省（omitempty）")

	_, after, totalAfter, _ := decodeContractList(t, env.list())
	require.Equal(t, 0, totalAfter)
	require.Empty(t, after)
}

// ---------------------------------------------------------------------------
// 2. 薄适配：同一端点、同一用户、同一数据库下的逐字段对照
// ---------------------------------------------------------------------------

func TestLegacyTicketAttachmentContract_GenericBackendFieldParity(t *testing.T) {
	env := newContractEnv(t)
	allow := contractValueAllowlist(t, env.ticket.ID)

	// 阶段 1：改造前链路产出记录 + 列表快照
	legacyUploadW := env.uploadPNG(contractFileName)
	require.Equal(t, http.StatusOK, legacyUploadW.Code, legacyUploadW.Body.String())
	legacyEnv, legacyData, _ := decodeContractObject(t, legacyUploadW)
	require.Equal(t, common.SuccessCode, legacyEnv.Code)
	legacyID := int(legacyData["id"].(float64))
	legacyRef := filepath.Base(legacyData["filePath"].(string))
	legacyListW := env.list()
	require.Equal(t, http.StatusOK, legacyListW.Code)
	_, legacyList, legacyTotal, _ := decodeContractList(t, legacyListW)
	require.Equal(t, 1, legacyTotal)
	require.Len(t, legacyList, 1)

	// 阶段 2：打开通用后端（读 + 写），同一对旧端点重放
	env.flags.read, env.flags.write = true, true

	genericUploadW := env.uploadPNG(contractFileName)
	require.Equal(t, http.StatusOK, genericUploadW.Code, genericUploadW.Body.String())
	genericEnv, genericData, genericKeys := decodeContractObject(t, genericUploadW)

	require.Equal(t, legacyEnv.Code, genericEnv.Code, "envelope.code 必须一致")
	require.Equal(t, legacyEnv.Message, genericEnv.Message, "envelope.message 必须一致")
	require.Equal(t, goldenTicketAttachmentFields, genericKeys, "字段集合必须与改造前一致")
	assertContractParity(t, legacyData, genericData, allow)

	// 前端真正消费的字段显式再钉一遍（allowlist 之外不得有任何取值漂移）
	require.Equal(t, legacyData["fileName"], genericData["fileName"])
	require.Equal(t, legacyData["fileSize"], genericData["fileSize"])
	require.Equal(t, legacyData["fileType"], genericData["fileType"])
	require.Equal(t, legacyData["mimeType"], genericData["mimeType"])
	require.Equal(t, legacyData["uploadedBy"], genericData["uploadedBy"])
	require.Equal(t, legacyData["ticketId"], genericData["ticketId"])
	require.Equal(t, "image/png", genericData["mimeType"])

	genericID := int(genericData["id"].(float64))
	// 两条记录分别落在各自的事实表：灰度期旧表（未回填）与新表并存，互不覆盖。
	genericRows, err := env.client.Attachment.Query().Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, genericRows, "通用链路写入 attachments 表")
	legacyRows, err := env.client.TicketAttachment.Query().Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, legacyRows, "旧链路记录仍在 ticket_attachments 表")

	// 列表：读开关打开后旧端点只返回通用表记录，但字段与改造前逐字段一致
	genericListW := env.list()
	require.Equal(t, http.StatusOK, genericListW.Code, genericListW.Body.String())
	genericListEnv, genericList, genericTotal, _ := decodeContractList(t, genericListW)
	require.Equal(t, common.SuccessCode, genericListEnv.Code)
	require.Equal(t, 1, genericTotal, "灰度期读通用表：旧表记录不参与（由回填/双写收敛）")
	require.Len(t, genericList, 1)
	assertContractParity(t, legacyList[0], genericList[0], allow)

	// 下载/预览：数字 ID（通用表）与历史文件名（旧表回退）都必须可用
	genericRef := strconv.Itoa(genericID)
	require.Equal(t, http.StatusOK, env.download(genericRef).Code)
	require.Equal(t, contractPNG, env.download(genericRef).Body.Bytes())
	require.Equal(t, "image/png", env.download(genericRef).Header().Get("Content-Type"))
	require.Equal(t, "attachment",
		strings.SplitN(env.download(genericRef).Header().Get("Content-Disposition"), ";", 2)[0])

	previewW := env.preview(genericRef)
	require.Equal(t, http.StatusOK, previewW.Code)
	require.Equal(t, "image/png", previewW.Header().Get("Content-Type"))
	require.Contains(t, previewW.Header().Get("Content-Disposition"), "inline")
	require.Equal(t, contractPNG, previewW.Body.Bytes())

	// D5：读开关打开时，仅存在于旧表的历史记录（按存储文件名引用）仍可下载
	legacyDownloadW := env.download(legacyRef)
	require.Equal(t, http.StatusOK, legacyDownloadW.Code, "历史文件名引用不得因读开关打开而失效")
	require.Equal(t, contractPNG, legacyDownloadW.Body.Bytes())

	// 删除：通用表记录走软删 + 引用保护
	deleteW := env.remove(genericRef)
	require.Equal(t, http.StatusOK, deleteW.Code, deleteW.Body.String())
	require.Equal(t, common.SuccessCode, decodeContractEnvelope(t, deleteW).Code)
	_, afterDelete, totalAfterDelete, _ := decodeContractList(t, env.list())
	require.Equal(t, 0, totalAfterDelete)
	require.Empty(t, afterDelete)

	// 删除回退：写开关打开但记录只存在于旧表（未回填/历史遗留）时，旧端点仍必须可删
	legacyDeleteW := env.remove(strconv.Itoa(legacyID))
	require.Equal(t, http.StatusOK, legacyDeleteW.Code, legacyDeleteW.Body.String())
	require.Equal(t, common.SuccessCode, decodeContractEnvelope(t, legacyDeleteW).Code)

	remaining, err := env.client.TicketAttachment.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, remaining, "旧表记录被回退路径清理")
}

// TestLegacyTicketAttachmentContract_GenericReadHostScoped 冻结「旧端点读取必须受路径宿主约束」。
//
// 灰度期通用表与旧表的主键序列相互独立、同号是常态：若通用读路径只按
// ID + 租户 取流，持有工单 A 路径的用户就能读到工单 B（乃至其它 biz_type）
// 的同号附件内容，而改造前旧链路按 ticket_id 过滤、不存在该行为。
func TestLegacyTicketAttachmentContract_GenericReadHostScoped(t *testing.T) {
	env := newContractEnv(t)
	ctx := context.Background()

	ticketB, err := env.client.Ticket.Create().
		SetTitle("旧工单附件契约测试工单 B").
		SetTicketNumber(fmt.Sprintf("T-CONTRACT-B-%d", env.tenant.ID)).
		SetRequesterID(env.user.ID).
		SetTenantID(env.tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	contentA := append(append([]byte{}, contractPNG...), []byte("payload-ticket-a")...)
	contentB := append(append([]byte{}, contractPNG...), []byte("payload-ticket-b")...)
	contentB2 := append(append([]byte{}, contractPNG...), []byte("payload-ticket-b-2")...)

	// 阶段 1（写开关关闭）：工单 A 经旧链路落一条记录，旧表首条主键为 1。
	legacyAW := env.uploadRawTo(env.ticket.ID, contractFileName, contentA)
	require.Equal(t, http.StatusOK, legacyAW.Code, legacyAW.Body.String())
	_, legacyAData, _ := decodeContractObject(t, legacyAW)
	legacyAID := int(legacyAData["id"].(float64))
	require.Equal(t, 1, legacyAID, "旧表首条记录主键为 1（同号构造的前提）")

	// 阶段 2（读写开关打开）：工单 B 经通用链路落一条记录，通用表首条主键同为 1。
	env.flags.read, env.flags.write = true, true
	genericBW := env.uploadRawTo(ticketB.ID, "契约 b.png", contentB)
	require.Equal(t, http.StatusOK, genericBW.Code, genericBW.Body.String())
	_, genericBData, _ := decodeContractObject(t, genericBW)
	genericBID := int(genericBData["id"].(float64))
	require.Equal(t, legacyAID, genericBID, "两表主键同号：读路径必须按宿主过滤，否则会串读")

	// 工单 A 的数字引用必须命中「属于工单 A 的旧表记录」，而不是通用表里的同号附件。
	viaA := env.downloadFor(env.ticket.ID, strconv.Itoa(legacyAID))
	require.Equal(t, http.StatusOK, viaA.Code, viaA.Body.String())
	require.Equal(t, contentA, viaA.Body.Bytes(), "不得串读到其它工单的同号附件")

	previewA := env.preview(strconv.Itoa(legacyAID))
	require.Equal(t, http.StatusOK, previewA.Code, previewA.Body.String())
	require.Equal(t, contentA, previewA.Body.Bytes(), "预览同样不得串读")

	// 工单 B 自身读取正常（宿主一致）。
	viaB := env.downloadFor(ticketB.ID, strconv.Itoa(genericBID))
	require.Equal(t, http.StatusOK, viaB.Code, viaB.Body.String())
	require.Equal(t, contentB, viaB.Body.Bytes())

	// 无同号旧记录时不串读：工单 A 引用通用表里属于工单 B 的第二条记录 → 404。
	genericB2W := env.uploadRawTo(ticketB.ID, "契约 b2.png", contentB2)
	require.Equal(t, http.StatusOK, genericB2W.Code, genericB2W.Body.String())
	_, genericB2Data, _ := decodeContractObject(t, genericB2W)
	foreignRef := strconv.Itoa(int(genericB2Data["id"].(float64)))

	foreign := env.downloadFor(env.ticket.ID, foreignRef)
	require.Equal(t, http.StatusNotFound, foreign.Code, foreign.Body.String())
	require.Equal(t, common.NotFoundCode, decodeContractEnvelope(t, foreign).Code)
	require.NotContains(t, foreign.Body.String(), "payload-ticket-b-2")
}

// ---------------------------------------------------------------------------
// 3. 错误路径：状态码 / 业务码 / 文案在两种模式下一致
// ---------------------------------------------------------------------------

func TestLegacyTicketAttachmentContract_ErrorCodeParity(t *testing.T) {
	env := newContractEnv(t)

	scenarios := []struct {
		name string
		call func(*contractEnv) *httptest.ResponseRecorder
	}{
		{"列表-宿主不存在", func(e *contractEnv) *httptest.ResponseRecorder {
			return e.do(e.router, http.MethodGet, "/api/v1/tickets/999999/attachments", "", nil)
		}},
		{"下载-附件不存在", func(e *contractEnv) *httptest.ResponseRecorder {
			return e.download("999999")
		}},
		{"预览-附件不存在", func(e *contractEnv) *httptest.ResponseRecorder {
			return e.preview("999999")
		}},
		{"删除-附件不存在", func(e *contractEnv) *httptest.ResponseRecorder {
			return e.remove("999999")
		}},
		{"上传-类型拒绝", func(e *contractEnv) *httptest.ResponseRecorder {
			return e.uploadRaw("恶意载荷.exe", []byte("MZ\x90\x00contract-test-payload"))
		}},
		{"下载-认证信息缺失", func(e *contractEnv) *httptest.ResponseRecorder {
			return e.do(e.noAuthRouter, http.MethodGet,
				fmt.Sprintf("/api/v1/tickets/%d/attachments/1", e.ticket.ID), "", nil)
		}},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			env.flags.read, env.flags.write = false, false
			legacyW := sc.call(env)

			env.flags.read, env.flags.write = true, true
			genericW := sc.call(env)

			require.Equal(t, legacyW.Code, genericW.Code,
				"HTTP 状态码必须一致（legacy=%s / generic=%s）", legacyW.Body.String(), genericW.Body.String())

			legacyEnv := decodeContractEnvelope(t, legacyW)
			genericEnv := decodeContractEnvelope(t, genericW)
			require.Equal(t, legacyEnv.Code, genericEnv.Code, "业务码必须一致")
			require.Equal(t, legacyEnv.Message, genericEnv.Message, "可读文案必须一致")
			require.NotEqual(t, common.SuccessCode, legacyEnv.Code, "错误场景不应返回成功码")
		})
	}
}
