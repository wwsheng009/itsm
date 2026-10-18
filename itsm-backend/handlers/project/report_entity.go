package project

import (
	"fmt"
	"strings"
	"time"

	"itsm-backend/ent"
)

// ---------------------------------------------------------------------------
// 状态报告词表（PE-17；模板 + 周期数据驱动，报告正文由事实数据实时派生）
// ---------------------------------------------------------------------------

const (
	ReportCadenceWeekly   = "weekly"
	ReportCadenceBiweekly = "biweekly"
	ReportCadenceMonthly  = "monthly"

	ReportTemplateStatusDraft    = "draft"
	ReportTemplateStatusActive   = "active"
	ReportTemplateStatusArchived = "archived"

	ReportSectionOverview   = "overview"
	ReportSectionTasks      = "tasks"
	ReportSectionMilestones = "milestones"
	ReportSectionGates      = "gates"
	ReportSectionVariance   = "variance"
	ReportSectionTeam       = "team"
)

// 报告词表（服务层校验 + 前端下拉同源）。
var (
	validReportCadences = map[string]bool{
		ReportCadenceWeekly: true, ReportCadenceBiweekly: true, ReportCadenceMonthly: true,
	}
	validReportSections = map[string]bool{
		ReportSectionOverview: true, ReportSectionTasks: true, ReportSectionMilestones: true,
		ReportSectionGates: true, ReportSectionVariance: true, ReportSectionTeam: true,
	}
)

// defaultReportSections 模板未显式指定章节时的默认输出。
func defaultReportSections() []string {
	return []string{
		ReportSectionOverview, ReportSectionTasks, ReportSectionMilestones,
		ReportSectionGates, ReportSectionVariance,
	}
}

// ---------------------------------------------------------------------------
// 请求 DTO
// ---------------------------------------------------------------------------

// ReportTemplateCreateRequest 报告模板创建（body）。
type ReportTemplateCreateRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Cadence     string   `json:"cadence"`
	Sections    []string `json:"sections"`
	Status      string   `json:"status"`
}

// ReportTemplateUpdateRequest 报告模板更新（nil = 不修改；version 乐观锁）。
type ReportTemplateUpdateRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Cadence     *string   `json:"cadence"`
	Sections    *[]string `json:"sections"`
	Status      *string   `json:"status"`
	Version     *int      `json:"version"`
}

// GenerateReportRequest 生成报告（body；templateId=0 用默认章节）。
type GenerateReportRequest struct {
	TemplateID int `json:"templateId"`
}

// ---------------------------------------------------------------------------
// 响应 DTO
// ---------------------------------------------------------------------------

// ReportTemplateResponse 报告模板响应。
type ReportTemplateResponse struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Cadence     string    `json:"cadence"`
	Sections    []string  `json:"sections"`
	Status      string    `json:"status"`
	Version     int       `json:"version"`
	TenantID    int       `json:"tenantId"`
	CreatedBy   *int      `json:"createdBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ToReportTemplateResponse 映射报告模板响应（sections 归一化，避免 JSONB null）。
func ToReportTemplateResponse(t *ent.ProjectReportTemplate) *ReportTemplateResponse {
	if t == nil {
		return nil
	}
	sections := normalizeReportSections(t.Sections)
	resp := &ReportTemplateResponse{
		ID: t.ID, Name: t.Name, Description: t.Description, Cadence: t.Cadence,
		Sections: sections, Status: t.Status, Version: t.Version, TenantID: t.TenantID,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
	if t.CreatedBy > 0 {
		id := t.CreatedBy
		resp.CreatedBy = &id
	}
	return resp
}

// ToReportTemplateResponseList 批量映射。
func ToReportTemplateResponseList(items []*ent.ProjectReportTemplate) []*ReportTemplateResponse {
	out := make([]*ReportTemplateResponse, 0, len(items))
	for _, item := range items {
		out = append(out, ToReportTemplateResponse(item))
	}
	return out
}

// normalizeReportSections 去重 + 保序 + 丢弃未知章节；空结果交回调用方决定默认。
func normalizeReportSections(sections []string) []string {
	seen := make(map[string]bool, len(sections))
	out := make([]string, 0, len(sections))
	for _, raw := range sections {
		section := strings.TrimSpace(raw)
		if section == "" || seen[section] || !validReportSections[section] {
			continue
		}
		seen[section] = true
		out = append(out, section)
	}
	return out
}

// ReportTaskStats 任务统计（报告章节 tasks）。
type ReportTaskStats struct {
	Total     int `json:"total"`
	Done      int `json:"done"`
	Open      int `json:"open"`
	Cancelled int `json:"cancelled"`
	Overdue   int `json:"overdue"`
	DueSoon   int `json:"dueSoon"`
}

// ReportMilestoneStats 里程碑统计（报告章节 milestones；达成率口径见契约 §7）。
type ReportMilestoneStats struct {
	Total     int     `json:"total"`
	Reached   int     `json:"reached"`
	Planned   int     `json:"planned"`
	Missed    int     `json:"missed"`
	Overdue   int     `json:"overdue"`
	OnTime    int     `json:"onTime"`
	Rate      float64 `json:"rate"`
	OverdueRate float64 `json:"overdueRate"`
}

// ReportGateItem 阶段门状态行（报告章节 gates）。
type ReportGateItem struct {
	Gate      string     `json:"gate"`
	Status    string     `json:"status"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// ReportVarianceSummary 最新基线偏差汇总（报告章节 variance；无基线省略）。
type ReportVarianceSummary struct {
	BaselineVersion   int        `json:"baselineVersion"`
	BaselineCreatedAt *time.Time `json:"baselineCreatedAt,omitempty"`
	Added             int        `json:"added"`
	Removed           int        `json:"removed"`
	Changed           int        `json:"changed"`
	Unchanged         int        `json:"unchanged"`
}

// ReportOwnerTeam 报告中的归口团队（软删 → Deleted=true，UI 降级提示）。
type ReportOwnerTeam struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// ReportTemplateRef 报告使用的模板引用。
type ReportTemplateRef struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Cadence string `json:"cadence"`
}

// ReportData 报告数据（数据派生；供前端展示与二次处理）。
type ReportData struct {
	ProjectID      int                `json:"projectId"`
	ProjectName    string             `json:"projectName"`
	ProjectCode    string             `json:"projectCode"`
	Status         string             `json:"status"`
	Health         string             `json:"health"`
	// PE-27：派生健康度解释项（无信号省略；与列表/D-57 同源）。
	HealthReasons  []HealthReason     `json:"healthReasons,omitempty"`
	Progress       int                `json:"progress"`
	ManagerID      *int               `json:"managerId,omitempty"`
	OwnerTeam      *ReportOwnerTeam   `json:"ownerTeam,omitempty"`
	GeneratedAt    time.Time          `json:"generatedAt"`
	Template       *ReportTemplateRef `json:"template,omitempty"`
	Sections       []string           `json:"sections"`
	TaskStats      ReportTaskStats    `json:"taskStats"`
	MilestoneStats ReportMilestoneStats `json:"milestoneStats"`
	Gates          []ReportGateItem   `json:"gates"`
	Variance       *ReportVarianceSummary `json:"variance,omitempty"`
}

// ReportResponse 生成结果（markdown 先导出；data 供前端展示/AI 后置）。
type ReportResponse struct {
	Markdown string     `json:"markdown"`
	Data     ReportData `json:"data"`
}

// ReportGroupResponse 报表分组行（按归口团队聚合）。
type ReportGroupResponse struct {
	OwnerTeamID      *int   `json:"ownerTeamId,omitempty"`
	OwnerTeamName    string `json:"ownerTeamName"`
	OwnerTeamDeleted bool   `json:"ownerTeamDeleted"`
	ProjectCount     int    `json:"projectCount"`
	GreenCount       int    `json:"greenCount"`
	YellowCount      int    `json:"yellowCount"`
	RedCount         int    `json:"redCount"`
}

// buildReportMarkdown 由 ReportData 派生 Markdown（纯函数，便于单测）。
func buildReportMarkdown(data *ReportData) string {
	if data == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# 项目状态报告 · %s（%s）\n\n", data.ProjectName, data.ProjectCode)
	fmt.Fprintf(&b, "- 生成时间：%s\n", data.GeneratedAt.Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "- 状态：%s｜健康度：%s｜进度：%d%%\n", data.Status, data.Health, data.Progress)
	if len(data.HealthReasons) > 0 {
		reasons := make([]string, 0, len(data.HealthReasons))
		for _, r := range data.HealthReasons {
			reasons = append(reasons, r.Message)
		}
		fmt.Fprintf(&b, "- 健康度信号：%s\n", strings.Join(reasons, "；"))
	}
	if data.ManagerID != nil {
		fmt.Fprintf(&b, "- 项目经理ID：%d\n", *data.ManagerID)
	}
	if data.OwnerTeam != nil {
		name := data.OwnerTeam.Name
		if data.OwnerTeam.Deleted {
			name += "（归口团队已停用，请重新归口）"
		}
		fmt.Fprintf(&b, "- 归口团队：%s\n", name)
	}
	if data.Template != nil {
		fmt.Fprintf(&b, "- 报告模板：%s（周期 %s）\n", data.Template.Name, data.Template.Cadence)
	}

	for _, section := range data.Sections {
		switch section {
		case ReportSectionOverview:
			b.WriteString("\n## 概览\n\n")
			fmt.Fprintf(&b, "- 任务：%d 项（完成 %d / 进行中 %d / 已取消 %d）\n",
				data.TaskStats.Total, data.TaskStats.Done, data.TaskStats.Open, data.TaskStats.Cancelled)
			fmt.Fprintf(&b, "- 里程碑：%d 项（达成 %d / 计划 %d / 错过 %d）\n",
				data.MilestoneStats.Total, data.MilestoneStats.Reached,
				data.MilestoneStats.Planned, data.MilestoneStats.Missed)
		case ReportSectionTasks:
			b.WriteString("\n## 任务\n\n")
			fmt.Fprintf(&b, "- 总数：%d｜完成：%d｜未终结：%d｜取消：%d\n",
				data.TaskStats.Total, data.TaskStats.Done, data.TaskStats.Open, data.TaskStats.Cancelled)
			fmt.Fprintf(&b, "- 逾期：%d｜3 天内到期：%d\n", data.TaskStats.Overdue, data.TaskStats.DueSoon)
		case ReportSectionMilestones:
			b.WriteString("\n## 里程碑\n\n")
			fmt.Fprintf(&b, "- 总数：%d｜达成：%d｜计划中：%d｜错过：%d｜逾期未达成：%d\n",
				data.MilestoneStats.Total, data.MilestoneStats.Reached,
				data.MilestoneStats.Planned, data.MilestoneStats.Missed, data.MilestoneStats.Overdue)
			fmt.Fprintf(&b, "- 达成率：%.1f%%（按期达成 %d）｜逾期率：%.1f%%\n",
				data.MilestoneStats.Rate*100, data.MilestoneStats.OnTime, data.MilestoneStats.OverdueRate*100)
		case ReportSectionGates:
			b.WriteString("\n## 阶段门\n\n")
			if len(data.Gates) == 0 {
				b.WriteString("- 暂无阶段门记录\n")
			}
			for _, gate := range data.Gates {
				fmt.Fprintf(&b, "- %s：%s\n", gate.Gate, gate.Status)
			}
		case ReportSectionVariance:
			b.WriteString("\n## 计划偏差\n\n")
			if data.Variance == nil {
				b.WriteString("- 未冻结计划基线\n")
			} else {
				fmt.Fprintf(&b, "- 基线 v%d（%s）\n", data.Variance.BaselineVersion,
					formatReportTime(data.Variance.BaselineCreatedAt))
				fmt.Fprintf(&b, "- 新增：%d｜移除：%d｜变更：%d｜未变：%d\n",
					data.Variance.Added, data.Variance.Removed, data.Variance.Changed, data.Variance.Unchanged)
			}
		case ReportSectionTeam:
			b.WriteString("\n## 团队归口\n\n")
			if data.OwnerTeam == nil {
				b.WriteString("- 未归口\n")
			} else {
				fmt.Fprintf(&b, "- %s（ID %d）\n", data.OwnerTeam.Name, data.OwnerTeam.ID)
			}
		}
	}
	return b.String()
}

func formatReportTime(t *time.Time) string {
	if t == nil {
		return "未知时间"
	}
	return t.Format("2006-01-02 15:04")
}
