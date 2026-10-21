package project

import (
	"os"
	"strings"
	"time"
	// 内嵌时区数据库：容器（alpine）未安装 tzdata 时仍能解析 Asia/Shanghai。
	_ "time/tzdata"
)

// 业务时区（A6 时间源校准）。
//
// 约定：
//   - 存储仍统一 UTC（timestamptz），不做数据迁移；
//   - **面向人的日期**（项目编号日期段、报告时间、导出文本）统一按业务时区格式化，
//     避免服务端 UTC 导致“跨天偏移”（报告编号 PRJ-20261020 对应本地 2026-10-21）。
//   - 业务时区可由 ITSM_BUSINESS_TIMEZONE 覆盖，默认 Asia/Shanghai；解析失败回退固定 +08:00。
const defaultBusinessTimezone = "Asia/Shanghai"

var businessLocation = loadBusinessLocation()

func loadBusinessLocation() *time.Location {
	name := strings.TrimSpace(os.Getenv("ITSM_BUSINESS_TIMEZONE"))
	if name == "" {
		name = defaultBusinessTimezone
	}
	if loc, err := time.LoadLocation(name); err == nil && loc != nil {
		return loc
	}
	// 固定偏移兜底（中国无夏令时；避免容器缺 tzdata 时 panic）。
	return time.FixedZone("CST", 8*3600)
}

// BusinessLocation 返回业务时区（只读）。
func BusinessLocation() *time.Location { return businessLocation }

// InBusinessTime 将任意时刻转换为业务时区（用于展示/编号，不用于存储）。
func InBusinessTime(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.In(businessLocation)
}

// FormatBusinessDate 业务时区的 YYYY-MM-DD。
func FormatBusinessDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return InBusinessTime(t).Format("2006-01-02")
}

// FormatBusinessDateTime 业务时区的 YYYY-MM-DD HH:mm。
func FormatBusinessDateTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return InBusinessTime(t).Format("2006-01-02 15:04")
}
