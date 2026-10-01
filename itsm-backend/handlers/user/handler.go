package user

import (
	"strconv"
	"strings"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/middleware"
	"itsm-backend/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// UserHandler HTTP handlers for user domain
type UserHandler struct {
	userService  Service
	provisioning *service.UserProvisioningService
	logger       *zap.SugaredLogger
}

// NewHandler creates a new UserHandler
func NewHandler(userService Service, logger *zap.SugaredLogger) *UserHandler {
	return &UserHandler{
		userService: userService,
		logger:      logger,
	}
}

// SetProvisioningService 注入建号通道收口服务（IP-P0-5）。
// 未接线时创建用户回退既有逻辑（测试/灰度回滚）。
func (h *UserHandler) SetProvisioningService(p *service.UserProvisioningService) {
	h.provisioning = p
}

// CreateUser 创建用户
// @Summary 创建用户
// @Description 创建新用户
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param user body dto.CreateUserRequest true "用户信息"
// @Success 200 {object} common.Response{data=dto.UserResponse}
// @Failure 400 {object} common.Response
// @Router /api/v1/users [post]
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req dto.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorf("参数绑定失败: %v", err)
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}

	// 确定目标租户：对于super_admin且请求中指定了tenant_id，使用请求中的；否则使用上下文中的
	targetTenantID := tenantID
	userRole, _ := c.Get("role")
	if userRole == "super_admin" && req.TenantID > 0 {
		targetTenantID = req.TenantID
	}

	// IP-P0-5：建号通道收口（platform/msp/tenant 自动解析 + 角色白名单）。
	// 未接线时保留下方 legacy 校验逻辑（灰度开关关闭或测试场景）。
	if h.provisioning != nil {
		h.provisionUserWithRequest(c, targetTenantID, &req)
		return
	}

	// 角色越权防护（C3 修复）：调用者不能分配高于自身权限的角色
	callerRole := c.GetString("role")
	if strings.TrimSpace(req.Role) != "" {
		targetRole := normalizeRole(req.Role)
		if roleRank(targetRole) > roleRank(callerRole) {
			common.Forbidden(c, "无权限分配高于自身角色的用户角色")
			return
		}
		// 跨租户创建用户仅允许 super_admin（MSP 场景）
		if targetTenantID != tenantID && callerRole != "super_admin" {
			common.Forbidden(c, "无权限跨租户创建用户")
			return
		}
	}
	// RoleIDs 越权防护：roleIds 中的角色不得高于调用者自身权限等级
	if len(req.RoleIDs) > 0 {
		if err := h.userService.CanGrantRoles(c.Request.Context(), targetTenantID, req.RoleIDs, callerRole); err != nil {
			common.Forbidden(c, err.Error())
			return
		}
	}

	user, err := h.userService.CreateUser(c.Request.Context(), &req, targetTenantID)
	if err != nil {
		h.logger.Errorf("创建用户失败: %v", err)
		// 业务错误：用户名/邮箱重复
		if strings.Contains(err.Error(), "已存在") {
			common.ParamErrorWithErr(c, err, "请求参数错误")
			return
		}
		// 密码策略校验失败属于客户端输入错误，应返回 400 并透出具体规则
		// （validatePassword 的提示信息不含敏感数据，可直接展示给用户）
		if strings.HasPrefix(err.Error(), "密码") {
			common.ParamError(c, err.Error())
			return
		}
		common.FailWithErr(c, err, "操作失败")
		return
	}

	response := dto.ToUserDetailResponse(user)

	common.Success(c, response)
}

// ProvisionUserToTenant 平台/租户通道建号（POST /api/v1/tenants/:id/users）。
func (h *UserHandler) ProvisionUserToTenant(c *gin.Context) {
	tenantID, err := strconv.Atoi(c.Param("id"))
	if err != nil || tenantID <= 0 {
		common.ParamError(c, "无效的租户ID")
		return
	}
	h.provisionUser(c, tenantID)
}

// ProvisionUserToCustomer MSP 通道建号（POST /api/v1/msp/customers/:customer_tenant_id/users）。
func (h *UserHandler) ProvisionUserToCustomer(c *gin.Context) {
	tenantID, err := strconv.Atoi(c.Param("customer_tenant_id"))
	if err != nil || tenantID <= 0 {
		common.ParamError(c, "无效的客户租户ID")
		return
	}
	h.provisionUser(c, tenantID)
}

// provisionUser 绑定请求体并走统一建号入口。
func (h *UserHandler) provisionUser(c *gin.Context, targetTenantID int) {
	var req dto.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorf("参数绑定失败: %v", err)
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}
	h.provisionUserWithRequest(c, targetTenantID, &req)
}

// provisionUserWithRequest 共用建号调用 + 稳定错误码映射。
func (h *UserHandler) provisionUserWithRequest(c *gin.Context, targetTenantID int, req *dto.CreateUserRequest) {
	if h.provisioning == nil {
		common.Fail(c, common.ServiceUnavailableCode, "建号通道未启用")
		return
	}
	created, err := h.provisioning.ProvisionUser(c.Request.Context(), provisionActorFromContext(c), targetTenantID, req)
	if err != nil {
		respondProvisionError(c, err)
		return
	}
	common.Success(c, dto.ToUserDetailResponse(created))
}

// provisionActorFromContext 从 gin context 提取调用方身份（JWT claims 派生）。
func provisionActorFromContext(c *gin.Context) service.ProvisionActor {
	mspRole := c.GetString("msp_role")
	if strings.TrimSpace(mspRole) == "" {
		mspRole = c.GetString("mspRole")
	}
	return service.ProvisionActor{
		UserID:       c.GetInt("user_id"),
		HomeTenantID: c.GetInt("tenant_id"),
		Role:         c.GetString("role"),
		MSPRole:      mspRole,
		Username:     c.GetString("username"),
	}
}

// respondProvisionError 将建号通道的稳定错误码映射为响应（对齐 MSP 中间件风格）。
func respondProvisionError(c *gin.Context, err error) {
	if pe, ok := service.AsProvisionError(err); ok {
		c.JSON(pe.Status, gin.H{"code": pe.Code, "message": pe.Message, "error": pe.Message})
		c.Abort()
		return
	}
	common.FailWithErr(c, err, "创建用户失败")
}

// ListUsers 获取用户列表
// @Summary 获取用户列表
// @Description 分页获取用户列表
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页数量" default(10)
// @Param status query string false "状态过滤" Enums(active, inactive)
// @Param department query string false "部门过滤"
// @Param search query string false "搜索关键词"
// @Success 200 {object} common.Response{data=dto.PagedUsersResponse}
// @Failure 400 {object} common.Response
// @Router /api/v1/users [get]
func (h *UserHandler) ListUsers(c *gin.Context) {
	var req dto.ListUsersRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		h.logger.Errorf("参数绑定失败: %v", err)
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	// 设置默认值
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 10
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}

	result, err := h.userService.ListUsers(c.Request.Context(), &req, tenantID)
	if err != nil {
		h.logger.Errorf("获取用户列表失败: %v", err)
		common.FailWithErr(c, err, "操作失败")
		return
	}

	common.Success(c, result)
}

// GetUser 获取用户详情
// @Summary 获取用户详情
// @Description 根据ID获取用户详情
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} common.Response{data=dto.UserResponse}
// @Failure 400 {object} common.Response
// @Failure 404 {object} common.Response
// @Router /api/v1/users/{id} [get]
func (h *UserHandler) GetUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.Fail(c, 1001, "用户ID格式错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}

	user, err := h.userService.GetUserByID(c.Request.Context(), id, tenantID)
	if err != nil {
		h.logger.Errorf("获取用户详情失败: %v", err)
		if strings.Contains(err.Error(), "用户不存在") {
			common.NotFound(c, "用户不存在")
			return
		}
		common.FailWithErr(c, err, "操作失败")
		return
	}

	response := dto.ToUserDetailResponse(user)

	common.Success(c, response)
}

// UpdateUser 更新用户信息
// @Summary 更新用户信息
// @Description 更新用户信息
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Param user body dto.UpdateUserRequest true "用户信息"
// @Success 200 {object} common.Response{data=dto.UserResponse}
// @Failure 400 {object} common.Response
// @Failure 404 {object} common.Response
// @Router /api/v1/users/{id} [put]
func (h *UserHandler) UpdateUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.Fail(c, 1001, "用户ID格式错误")
		return
	}

	var req dto.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorf("参数绑定失败: %v", err)
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}

	// 角色越权防护（C3 修复）
	callerRole := c.GetString("role")
	if strings.TrimSpace(req.Role) != "" {
		if roleRank(normalizeRole(req.Role)) > roleRank(callerRole) {
			common.Forbidden(c, "无权限分配高于自身角色的用户角色")
			return
		}
	}
	// RoleIDs 越权防护：roleIds 中的角色不得高于调用者自身权限等级
	if len(req.RoleIDs) > 0 {
		if err := h.userService.CanGrantRoles(c.Request.Context(), tenantID, req.RoleIDs, callerRole); err != nil {
			common.Forbidden(c, err.Error())
			return
		}
	}

	user, err := h.userService.UpdateUser(c.Request.Context(), id, &req, tenantID)
	if err != nil {
		h.logger.Errorf("更新用户失败: %v", err)
		if strings.Contains(err.Error(), "用户不存在") {
			common.NotFound(c, "用户不存在")
			return
		}
		if strings.Contains(err.Error(), "已存在") {
			common.ParamErrorWithErr(c, err, "请求参数错误")
			return
		}
		common.FailWithErr(c, err, "操作失败")
		return
	}

	response := dto.ToUserDetailResponse(user)

	common.Success(c, response)
}

// DeleteUser 删除用户
// @Summary 删除用户
// @Description 删除用户（软删除）
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} common.Response
// @Failure 400 {object} common.Response
// @Failure 404 {object} common.Response
// @Router /api/v1/users/{id} [delete]
func (h *UserHandler) DeleteUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.Fail(c, 1001, "用户ID格式错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}

	err = h.userService.DeleteUser(c.Request.Context(), id, tenantID)
	if err != nil {
		h.logger.Errorf("删除用户失败: %v", err)
		if strings.Contains(err.Error(), "用户不存在") {
			common.NotFound(c, "用户不存在")
			return
		}
		common.FailWithErr(c, err, "操作失败")
		return
	}

	common.Success(c, nil)
}

// ChangeUserStatus 更改用户状态
// @Summary 更改用户状态
// @Description 激活或禁用用户
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Param status body dto.ChangeUserStatusRequest true "状态信息"
// @Success 200 {object} common.Response
// @Failure 400 {object} common.Response
// @Failure 404 {object} common.Response
// @Router /api/v1/users/{id}/status [put]
func (h *UserHandler) ChangeUserStatus(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.Fail(c, 1001, "用户ID格式错误")
		return
	}

	var req dto.ChangeUserStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorf("参数绑定失败: %v", err)
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	// 获取当前登录用户ID
	currentUserID := c.GetInt("user_id")
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}

	err = h.userService.ChangeUserStatus(c.Request.Context(), id, req.Active, currentUserID, tenantID)
	if err != nil {
		h.logger.Errorf("更改用户状态失败: %v", err)
		if strings.Contains(err.Error(), "用户不存在") {
			common.NotFound(c, "用户不存在")
			return
		}
		common.FailWithErr(c, err, "操作失败")
		return
	}

	common.Success(c, nil)
}

// ResetPassword 重置用户密码
// @Summary 重置用户密码
// @Description 管理员重置用户密码
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param id path int true "用户ID"
// @Param password body dto.ResetPasswordRequest true "新密码"
// @Success 200 {object} common.Response
// @Failure 400 {object} common.Response
// @Failure 404 {object} common.Response
// @Router /api/v1/users/{id}/reset-password [put]
func (h *UserHandler) ResetPassword(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.Fail(c, 1001, "用户ID格式错误")
		return
	}

	var req dto.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorf("参数绑定失败: %v", err)
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}

	err = h.userService.ResetPassword(c.Request.Context(), id, req.NewPassword, tenantID)
	if err != nil {
		h.logger.Errorf("重置密码失败: %v", err)
		if strings.Contains(err.Error(), "用户不存在") {
			common.NotFound(c, "用户不存在")
			return
		}
		// 密码策略校验失败属于客户端输入错误，应返回 400 并透出具体规则
		if strings.HasPrefix(err.Error(), "密码") {
			common.ParamError(c, err.Error())
			return
		}
		common.FailWithErr(c, err, "操作失败")
		return
	}

	common.Success(c, nil)
}

// GetUserStats 获取用户统计
// @Summary 获取用户统计
// @Description 获取用户统计信息
// @Tags 用户管理
// @Accept json
// @Produce json
// @Success 200 {object} common.Response{data=object}
// @Failure 400 {object} common.Response
// @Router /api/v1/users/stats [get]
func (h *UserHandler) GetUserStats(c *gin.Context) {
	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}

	stats, err := h.userService.GetUserStats(c.Request.Context(), tenantID)
	if err != nil {
		h.logger.Errorf("获取用户统计失败: %v", err)
		common.FailWithErr(c, err, "操作失败")
		return
	}

	common.Success(c, stats)
}

// BatchUpdateUsers 批量更新用户
// @Summary 批量更新用户
// @Description 批量更新用户状态或部门
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param request body dto.BatchUpdateUsersRequest true "批量更新请求"
// @Success 200 {object} common.Response
// @Failure 400 {object} common.Response
// @Router /api/v1/users/batch [put]
func (h *UserHandler) BatchUpdateUsers(c *gin.Context) {
	var req dto.BatchUpdateUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorf("参数绑定失败: %v", err)
		common.ParamErrorWithErr(c, err, "请求参数错误")
		return
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}
	req.OperatorID = c.GetInt("user_id")

	err := h.userService.BatchUpdateUsers(c.Request.Context(), &req, tenantID)
	if err != nil {
		h.logger.Errorf("批量更新用户失败: %v", err)
		common.FailWithErr(c, err, "操作失败")
		return
	}

	common.Success(c, nil)
}

// SearchUsers 搜索用户
// @Summary 搜索用户
// @Description 搜索用户
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param keyword query string true "搜索关键词"
// @Param limit query int false "限制数量" default(10)
// @Success 200 {object} common.Response{data=[]dto.UserResponse}
// @Failure 400 {object} common.Response
// @Router /api/v1/users/search [get]
func (h *UserHandler) SearchUsers(c *gin.Context) {
	var req dto.SearchUsersRequest
	// 兼容：测试与部分调用方使用 POST+JSON；也支持 GET+Query
	if c.Request.Method == "POST" {
		if err := c.ShouldBindJSON(&req); err != nil {
			h.logger.Errorf("参数绑定失败: %v", err)
			common.ParamErrorWithErr(c, err, "请求参数错误")
			return
		}
	} else {
		if err := c.ShouldBindQuery(&req); err != nil {
			h.logger.Errorf("参数绑定失败: %v", err)
			common.ParamErrorWithErr(c, err, "请求参数错误")
			return
		}
	}

	if strings.TrimSpace(req.Keyword) == "" {
		common.Fail(c, 1001, "搜索关键词不能为空")
		return
	}

	// 设置默认限制
	if req.Limit <= 0 {
		req.Limit = 10
	}

	tenantID := c.GetInt("tenant_id")
	if tenantID == 0 {
		common.Fail(c, common.UnauthorizedCode, "租户信息缺失")
		return
	}

	users, err := h.userService.SearchUsers(c.Request.Context(), &req, tenantID)
	if err != nil {
		h.logger.Errorf("搜索用户失败: %v", err)
		common.FailWithErr(c, err, "操作失败")
		return
	}

	common.Success(c, users)
}

// roleRank 返回角色权限层级（单一词表：middleware.RoleRank，覆盖 msp_*；IP-P0-9/07:G3）。
func roleRank(role string) int {
	return middleware.RoleRank(role)
}

// normalizeRole 将前端传入的角色归一化（user -> end_user）
func normalizeRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	if r == "user" {
		return "end_user"
	}
	return r
}
