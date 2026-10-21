package project

import (
	"context"
	"strings"

	"itsm-backend/common"
	"itsm-backend/middleware"

	"github.com/gin-gonic/gin"
)

// 归口（ownerTeam）候选选择器（A2 回归遗留）。
//
// 背景：`ownerTeamId` 在后端早就有写入校验（ensureOwnerTeam）与报表聚合，
// 但前端**没有任何维护入口**，导致「归口」列恒为「未设置」、报表中心恒为「未归口」。
// 本端点补齐取数来源；项目创建/编辑弹窗据此提供下拉。
//
// 语义：
//   - 租户级字典（与具体项目无关，创建时也能用），权限沿用 `project:read`；
//   - 只返回同租户、未软删、status=active 的团队（与 ensureOwnerTeam 写入校验同口径）；
//   - 最小字段 {id,name,code}，不回传团队负责人/成员等非必要信息。

// OwnerTeamCandidate 归口团队候选（最小字段）。
type OwnerTeamCandidate struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Code string `json:"code,omitempty"`
}

// ListOwnerTeamCandidates 归口团队候选
// @Summary 归口团队候选（同租户、未删除、active；q 匹配名称/代码）
// @Tags 项目管理
// @Produce json
// @Param q query string false "关键词（名称/代码）"
// @Param limit query int false "返回条数（默认 20，上限 50）"
// @Success 200 {object} common.Response{data=[]OwnerTeamCandidate}
// @Router /api/v1/projects/owner-team-candidates [get]
func (h *Handler) ListOwnerTeamCandidates(c *gin.Context) {
	tenantID, ok := middleware.TenantIDOrUnauthorized(c)
	if !ok {
		return
	}
	items, err := h.service.ListOwnerTeamCandidates(
		c.Request.Context(), tenantID, c.Query("q"), atoiOrZero(c.Query("limit")),
	)
	if err != nil {
		h.fail(c, err)
		return
	}
	common.Success(c, items)
}

// ListOwnerTeamCandidates 服务层：归口候选查询（limit 收敛 ≤50；失败不降级为假数据）。
func (s *Service) ListOwnerTeamCandidates(
	ctx context.Context, tenantID int, keyword string, limit int,
) ([]OwnerTeamCandidate, error) {
	if limit <= 0 {
		limit = 20
	}
	refs, err := s.repo.ListActiveTeamRefs(ctx, tenantID, strings.TrimSpace(keyword), limit)
	if err != nil {
		return nil, err
	}
	out := make([]OwnerTeamCandidate, 0, len(refs))
	for _, ref := range refs {
		if ref.Deleted || ref.Status != "active" {
			continue
		}
		out = append(out, OwnerTeamCandidate{ID: ref.ID, Name: ref.Name, Code: ref.Code})
	}
	return out, nil
}
