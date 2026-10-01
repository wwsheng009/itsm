// user_preferences.go：用户服务端偏好（IP-P1-6c）。
//
// 边界：
//   - 仅本人可读写（路由 AuthMiddleware + 从 claims 取 user_id，不接受路径参数）；
//   - 只接受白名单键（当前 workbenchFilter），未知键整体拒绝（防偏好膨胀为任意 KV）；
//   - 单请求 ≤8KB；nil 值表示删除该键。
package common

import (
	"context"
	"encoding/json"
	"errors"

	"itsm-backend/common"
	"itsm-backend/dto"

	"github.com/gin-gonic/gin"
)

// 偏好域错误（handler 映射为 400）。
var (
	ErrPreferenceKeyNotAllowed   = errors.New("preference key not allowed")
	ErrPreferencePayloadTooLarge = errors.New("preference payload too large")
)

// allowedPreferenceKeys 偏好键白名单（P1：workbenchFilter；后续按需登记）。
var allowedPreferenceKeys = map[string]bool{
	"workbenchFilter": true,
}

const maxPreferenceBytes = 8 << 10

// GetUserPreferences 读取本人偏好（无则返回空 map，不写库）。
func (s *Service) GetUserPreferences(ctx context.Context, userID int) (map[string]any, error) {
	if s.client == nil {
		return nil, errors.New("ent client not available")
	}
	u, err := s.client.User.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.Preferences == nil {
		return map[string]any{}, nil
	}
	return u.Preferences, nil
}

// UpdateUserPreferences 合并写入本人偏好：白名单校验 + 大小限制 + 顶层合并（nil 删除键）。
func (s *Service) UpdateUserPreferences(ctx context.Context, userID int, patch map[string]any) (map[string]any, error) {
	if s.client == nil {
		return nil, errors.New("ent client not available")
	}
	for k := range patch {
		if !allowedPreferenceKeys[k] {
			return nil, ErrPreferenceKeyNotAllowed
		}
	}
	if raw, err := json.Marshal(patch); err != nil || len(raw) > maxPreferenceBytes {
		return nil, ErrPreferencePayloadTooLarge
	}
	u, err := s.client.User.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	merged := map[string]any{}
	for k, v := range u.Preferences {
		merged[k] = v
	}
	for k, v := range patch {
		if v == nil {
			delete(merged, k)
			continue
		}
		merged[k] = v
	}
	if _, err := s.client.User.UpdateOneID(userID).SetPreferences(merged).Save(ctx); err != nil {
		return nil, err
	}
	return merged, nil
}

// GetMyPreferences GET /api/v1/users/me/preferences
func (h *Handler) GetMyPreferences(c *gin.Context) {
	userID := c.GetInt("user_id")
	if userID <= 0 {
		common.Fail(c, common.AuthFailedCode, "用户信息缺失")
		return
	}
	prefs, err := h.svc.GetUserPreferences(c.Request.Context(), userID)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, "获取偏好失败")
		return
	}
	common.Success(c, dto.UserPreferencesResponse{Preferences: prefs})
}

// UpdateMyPreferences PUT /api/v1/users/me/preferences
func (h *Handler) UpdateMyPreferences(c *gin.Context) {
	userID := c.GetInt("user_id")
	if userID <= 0 {
		common.Fail(c, common.AuthFailedCode, "用户信息缺失")
		return
	}
	var patch map[string]any
	if err := c.ShouldBindJSON(&patch); err != nil || patch == nil {
		common.Fail(c, common.ParamErrorCode, "请求参数错误（需 JSON 对象）")
		return
	}
	prefs, err := h.svc.UpdateUserPreferences(c.Request.Context(), userID, patch)
	switch {
	case errors.Is(err, ErrPreferenceKeyNotAllowed):
		common.Fail(c, common.ParamErrorCode, "不支持的偏好项")
		return
	case errors.Is(err, ErrPreferencePayloadTooLarge):
		common.Fail(c, common.ParamErrorCode, "偏好内容过大（≤8KB）")
		return
	case err != nil:
		common.Fail(c, common.InternalErrorCode, "保存偏好失败")
		return
	}
	common.Success(c, dto.UserPreferencesResponse{Preferences: prefs})
}
