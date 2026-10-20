package ai

import (
	"encoding/json"

	"itsm-backend/common"
	"itsm-backend/ent"

	"github.com/gin-gonic/gin"
)

// I0-04 Outcome Contract：产物详情（结果落点的「打开」目标）。
//
// 口径：
//   - 只读；不写任何业务表；
//   - 归属收敛：租户 + 发起人 + ID（bot.ArtifactStore.Get 同口径），
//     跨租户/跨用户/不存在统一 404，避免枚举；
//   - 帮助草稿（help_create_draft/help_update_draft）另由知识审阅队列承载，
//     此处仍按归属返回，避免把他人草稿暴露给普通对话入口。

type artifactDetail struct {
	ID             int             `json:"id"`
	Kind           string          `json:"kind"`
	Title          string          `json:"title"`
	ToolName       string          `json:"toolName,omitempty"`
	ConversationID int             `json:"conversationId,omitempty"`
	RunID          int             `json:"runId,omitempty"`
	Content        json.RawMessage `json:"content,omitempty"`
	Evidence       json.RawMessage `json:"evidence,omitempty"`
	CreatedAt      string          `json:"createdAt,omitempty"`
}

// GetArtifact handles GET /api/v1/ai/artifacts/:id.
func (h *Handler) GetArtifact(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	userID := c.GetInt("user_id")
	if tenantID <= 0 || userID <= 0 {
		common.Fail(c, common.AuthFailedCode, "租户/用户信息缺失")
		return
	}
	if h == nil || h.svc == nil || h.svc.artifacts == nil {
		common.Fail(c, common.ServiceUnavailableCode, "产物服务未就绪")
		return
	}
	id, ok := common.ParseResourceID(c, "id", "产物")
	if !ok {
		return
	}
	row, err := h.svc.artifacts.Get(c.Request.Context(), tenantID, userID, id)
	if err != nil {
		if ent.IsNotFound(err) {
			common.NotFound(c, "产物不存在或无权访问")
			return
		}
		if h.svc.logger != nil {
			h.svc.logger.Warnw("读取产物详情失败", "error", err, "tenantID", tenantID, "artifactID", id)
		}
		common.Fail(c, common.ServiceUnavailableCode, "读取产物失败")
		return
	}
	common.Success(c, toArtifactDetail(row))
}

func toArtifactDetail(row *ent.BotArtifact) artifactDetail {
	detail := artifactDetail{
		ID:             row.ID,
		Kind:           row.Kind,
		Title:          row.Title,
		ToolName:       row.ToolName,
		ConversationID: row.ConversationID,
		RunID:          row.RunID,
		CreatedAt:      row.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if json.Valid([]byte(row.ContentJSON)) {
		detail.Content = json.RawMessage(row.ContentJSON)
	}
	if json.Valid([]byte(row.EvidenceJSON)) {
		detail.Evidence = json.RawMessage(row.EvidenceJSON)
	}
	return detail
}
