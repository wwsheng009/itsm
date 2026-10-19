// user_preferences.go：用户服务端偏好（IP-P1-6c）。
//
// 边界：
//   - 仅本人可读写（路由 AuthMiddleware + 从 claims 取 user_id，不接受路径参数）；
//   - 只接受白名单键（workbenchFilter / V2-01 assistantMemory），未知键整体拒绝（防偏好膨胀为任意 KV）；
//   - 单请求 ≤8KB；nil 值表示删除该键。
package common

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"itsm-backend/common"
	"itsm-backend/dto"

	"github.com/gin-gonic/gin"
)

// 偏好域错误（handler 映射为 400）。
var (
	ErrPreferenceKeyNotAllowed   = errors.New("preference key not allowed")
	ErrPreferencePayloadTooLarge = errors.New("preference payload too large")
	ErrPreferenceValueInvalid    = errors.New("preference value invalid")
)

// allowedPreferenceKeys 偏好键白名单（P1：workbenchFilter；V2-01：assistantMemory；后续按需登记）。
var allowedPreferenceKeys = map[string]bool{
	"workbenchFilter": true,
	"assistantMemory": true,
}

const maxPreferenceBytes = 8 << 10

// assistantMemory 结构护栏（V2-01，前后端同口径）：
// 元数据优先——recent 只存动作键 + 时间，绝不承载对话正文；上限与前端 normalizeMemory 对齐。
const (
	memoryMaxPhrases        = 20
	memoryMaxPhraseRunes    = 200
	memoryMaxTemplates      = 10
	memoryMaxTemplateName   = 40
	memoryMaxTemplateBody   = 1000
	memoryMaxRecent         = 20
	memoryMaxRecentKeyRunes = 120
)

// recentActionKeyPattern：动作键为结构化短值（如 inline_apply:triage_detail），拒绝任意文本。
var recentActionKeyPattern = regexp.MustCompile(`^[a-z0-9_.:-]+$`)

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
	if raw, ok := patch["assistantMemory"]; ok && raw != nil {
		if err := validateAssistantMemory(raw); err != nil {
			return nil, ErrPreferenceValueInvalid
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

// validateAssistantMemory 校验 AI 助手个性化记忆结构（白名单字段 + 条数/长度上限）。
//
// 非法结构整体拒绝（不静默裁剪）：前端 normalizeMemory 已裁剪，服务端拒绝即契约破坏的显性信号。
func validateAssistantMemory(raw any) error {
	obj, ok := raw.(map[string]any)
	if !ok {
		return ErrPreferenceValueInvalid
	}
	for key := range obj {
		switch key {
		case "version", "phrases", "templates", "recent":
		default:
			return ErrPreferenceValueInvalid
		}
	}
	if v, ok := obj["version"]; ok {
		if num, ok := v.(float64); !ok || num != 1 {
			return ErrPreferenceValueInvalid
		}
	}
	if v, ok := obj["phrases"]; ok {
		list, ok := v.([]any)
		if !ok || len(list) > memoryMaxPhrases {
			return ErrPreferenceValueInvalid
		}
		for _, item := range list {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > memoryMaxPhraseRunes {
				return ErrPreferenceValueInvalid
			}
		}
	}
	if v, ok := obj["templates"]; ok {
		list, ok := v.([]any)
		if !ok || len(list) > memoryMaxTemplates {
			return ErrPreferenceValueInvalid
		}
		for _, item := range list {
			tpl, ok := item.(map[string]any)
			if !ok {
				return ErrPreferenceValueInvalid
			}
			for key := range tpl {
				if key != "name" && key != "body" {
					return ErrPreferenceValueInvalid
				}
			}
			name, _ := tpl["name"].(string)
			body, _ := tpl["body"].(string)
			if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > memoryMaxTemplateName ||
				utf8.RuneCountInString(body) > memoryMaxTemplateBody {
				return ErrPreferenceValueInvalid
			}
		}
	}
	if v, ok := obj["recent"]; ok {
		list, ok := v.([]any)
		if !ok || len(list) > memoryMaxRecent {
			return ErrPreferenceValueInvalid
		}
		for _, item := range list {
			entry, ok := item.(map[string]any)
			if !ok {
				return ErrPreferenceValueInvalid
			}
			for key := range entry {
				if key != "key" && key != "at" {
					return ErrPreferenceValueInvalid
				}
			}
			actionKey, _ := entry["key"].(string)
			at, _ := entry["at"].(string)
			if !recentActionKeyPattern.MatchString(actionKey) ||
				utf8.RuneCountInString(actionKey) > memoryMaxRecentKeyRunes ||
				at == "" || utf8.RuneCountInString(at) > 40 {
				return ErrPreferenceValueInvalid
			}
		}
	}
	return nil
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
	case errors.Is(err, ErrPreferenceValueInvalid):
		common.Fail(c, common.ParamErrorCode, "偏好内容非法（assistantMemory 结构/上限不符）")
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
