// metrics.go：把 RLS 运行时计数器桥接到 Prometheus（拉取式，零热路径开销）。
//
// 告警口径（enforce 灰度，报告 §5）：
//   - itsm_rls_missing_tenant_total > 0  → 暂停灰度并评估回滚；
//   - itsm_rls_app_pool_configured = 0 且 itsm_rls_info{mode="enforce"} → 分流未生效
//     （检查 DB_APP_ROLE_* 与启动日志 "rls: app pool …"）。
package rls

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	rlsInfoDesc = prometheus.NewDesc(
		"itsm_rls_info",
		"RLS 运行时信息，恒为 1；mode 标签为 off/shadow/enforce",
		[]string{"mode"}, nil,
	)
	rlsQueriesOffDesc = prometheus.NewDesc(
		"itsm_rls_queries_off_total",
		"off 模式下观测到的语句总数",
		nil, nil,
	)
	rlsQueriesShadowDesc = prometheus.NewDesc(
		"itsm_rls_queries_shadow_total",
		"shadow 模式下观测到的语句总数",
		nil, nil,
	)
	rlsMissingTenantDesc = prometheus.NewDesc(
		"itsm_rls_missing_tenant_total",
		"缺少租户作用域（且非 system 绕过）的语句数；enforce 下非零应触发回滚评估",
		nil, nil,
	)
	rlsSystemBypassDesc = prometheus.NewDesc(
		"itsm_rls_system_bypass_total",
		"显式 system 作用域（平台/后台）语句数",
		nil, nil,
	)
	rlsEnforceAppliedDesc = prometheus.NewDesc(
		"itsm_rls_enforce_applied_total",
		"enforce 模式下已注入租户变量的语句数",
		nil, nil,
	)
	rlsAppRoutedDesc = prometheus.NewDesc(
		"itsm_rls_app_routed_total",
		"路由到低权请求池（itsm_app）的租户语句数",
		nil, nil,
	)
	rlsAppPoolDesc = prometheus.NewDesc(
		"itsm_rls_app_pool_configured",
		"低权请求池是否已配置并连通（1=是，0=否）",
		nil, nil,
	)
)

// metricsCollector 拉取式收集某个 Driver 的计数器；Driver 实例本身即数据源。
type metricsCollector struct {
	d *Driver
}

func newMetricsCollector(d *Driver) *metricsCollector { return &metricsCollector{d: d} }

// Describe 声明全部输出指标（先 Describe 后 Collect，符合注册协议）。
func (c *metricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- rlsInfoDesc
	ch <- rlsQueriesOffDesc
	ch <- rlsQueriesShadowDesc
	ch <- rlsMissingTenantDesc
	ch <- rlsSystemBypassDesc
	ch <- rlsEnforceAppliedDesc
	ch <- rlsAppRoutedDesc
	ch <- rlsAppPoolDesc
}

// Collect 在抓取时读取计数器快照；不占用热路径。
func (c *metricsCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.d.Stats()
	ch <- prometheus.MustNewConstMetric(rlsInfoDesc, prometheus.GaugeValue, 1, string(s.Mode))
	ch <- prometheus.MustNewConstMetric(rlsQueriesOffDesc, prometheus.CounterValue, float64(s.QueriesOff))
	ch <- prometheus.MustNewConstMetric(rlsQueriesShadowDesc, prometheus.CounterValue, float64(s.QueriesShadow))
	ch <- prometheus.MustNewConstMetric(rlsMissingTenantDesc, prometheus.CounterValue, float64(s.MissingTenant))
	ch <- prometheus.MustNewConstMetric(rlsSystemBypassDesc, prometheus.CounterValue, float64(s.SystemBypass))
	ch <- prometheus.MustNewConstMetric(rlsEnforceAppliedDesc, prometheus.CounterValue, float64(s.EnforceApplied))
	ch <- prometheus.MustNewConstMetric(rlsAppRoutedDesc, prometheus.CounterValue, float64(s.AppRouted))
	pool := 0.0
	if c.d.AppPoolConfigured() {
		pool = 1
	}
	ch <- prometheus.MustNewConstMetric(rlsAppPoolDesc, prometheus.GaugeValue, pool)
}

// RegisterMetrics 把 Driver 计数器注册到默认注册表（/metrics 同源）。
// 幂等：同名已注册（测试或重复初始化）返回 nil，仅真实注册错误向上抛。
func RegisterMetrics(d *Driver) error {
	if d == nil {
		return nil
	}
	if err := prometheus.Register(newMetricsCollector(d)); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); ok {
			return nil
		}
		return err
	}
	return nil
}
