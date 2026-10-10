// user_profile.go：自助资料更新（IP-P1-6 补口，剩余缺口治理 B3）。
//
// 边界：
//   - 仅本人可写（路由 AuthMiddleware + 从 claims 取 user_id/tenant_id，不接受路径参数）；
//   - 字段白名单：name/email/phone（不含 role/department/status 等治理字段）；
//   - 邮箱唯一性沿用 admin UpdateUser 口径（跨用户占用 → 400）；
//   - 错误映射：占用/空值 → 400，未找到 → 404。
package common

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/user"

	"github.com/gin-gonic/gin"
)

// 自助资料域错误（handler 映射为 400）。
var (
	ErrEmailTaken = errors.New("email already taken")
	ErrNameEmpty  = errors.New("name must not be empty")
	ErrEmailEmpty = errors.New("email must not be empty")
)

// UserProfileView 自助资料更新后的可见投影（不返回角色/租户等治理字段）。
type UserProfileView struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
}

// UpdateUserProfile 更新本人资料（name/email/phone 白名单；nil 表示不动该字段）。
func (s *Service) UpdateUserProfile(
	ctx context.Context,
	userID, tenantID int,
	name, email, phone *string,
) (*UserProfileView, error) {
	if s.client == nil {
		return nil, errors.New("ent client not available")
	}
	if userID <= 0 || tenantID <= 0 {
		return nil, fmt.Errorf("用户信息缺失")
	}

	current, err := s.client.User.Query().
		Where(user.IDEQ(userID), user.TenantIDEQ(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, err
	}

	update := s.client.User.UpdateOneID(userID).Where(user.TenantIDEQ(tenantID))

	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return nil, ErrNameEmpty
		}
		update = update.SetName(trimmed)
	}
	if email != nil {
		trimmed := strings.TrimSpace(*email)
		if trimmed == "" {
			return nil, ErrEmailEmpty
		}
		if trimmed != current.Email {
			exists, err := s.client.User.Query().
				Where(user.And(user.EmailEQ(trimmed), user.IDNEQ(userID))).
				Exist(ctx)
			if err != nil {
				return nil, err
			}
			if exists {
				return nil, ErrEmailTaken
			}
		}
		update = update.SetEmail(trimmed)
	}
	if phone != nil {
		update = update.SetPhone(strings.TrimSpace(*phone))
	}

	if err := update.Exec(ctx); err != nil {
		return nil, err
	}

	updated, err := s.client.User.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &UserProfileView{
		ID:       updated.ID,
		Username: updated.Username,
		Name:     updated.Name,
		Email:    updated.Email,
		Phone:    updated.Phone,
	}, nil
}

// UpdateMe PUT /api/v1/users/me —— 自助资料更新（仅本人）。
func (h *Handler) UpdateMe(c *gin.Context) {
	userID := c.GetInt("user_id")
	tenantID := c.GetInt("tenant_id")
	if userID <= 0 || tenantID <= 0 {
		common.Fail(c, common.AuthFailedCode, "用户信息缺失")
		return
	}

	var req struct {
		Name  *string `json:"name,omitempty" binding:"omitempty,min=1,max=100"`
		Email *string `json:"email,omitempty" binding:"omitempty,email"`
		Phone *string `json:"phone,omitempty" binding:"omitempty,max=32"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamError(c, "参数错误: "+err.Error())
		return
	}
	if req.Name == nil && req.Email == nil && req.Phone == nil {
		common.ParamError(c, "至少提供一个可更新字段（name/email/phone）")
		return
	}

	view, err := h.svc.UpdateUserProfile(c.Request.Context(), userID, tenantID, req.Name, req.Email, req.Phone)
	switch {
	case errors.Is(err, ErrEmailTaken):
		common.Fail(c, common.ParamErrorCode, "邮箱已被占用")
		return
	case errors.Is(err, ErrNameEmpty):
		common.Fail(c, common.ParamErrorCode, "姓名不能为空")
		return
	case errors.Is(err, ErrEmailEmpty):
		common.Fail(c, common.ParamErrorCode, "邮箱不能为空")
		return
	case ent.IsNotFound(err):
		common.Fail(c, common.NotFoundCode, "用户不存在")
		return
	case err != nil:
		common.FailWithErr(c, err, "更新资料失败")
		return
	}
	common.Success(c, view)
}
