package rls

import (
	"context"
	"testing"

	"itsm-backend/common/tenantctx"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// TestMetricsCollector（监控接入，报告 §5）：计数器桥接到 Prometheus 后，
// 抓取端可读到正确数值；RegisterMetrics 幂等（重复注册不报错）。
func TestMetricsCollector(t *testing.T) {
	d := NewDriver(&fakeDriver{}, ModeEnforce, zap.NewNop().Sugar())
	d.SetAppPool(&fakeDriver{})

	ctxT := tenantctx.WithTenantID(context.Background(), 7)
	if err := d.Query(ctxT, "SELECT 1", nil, nil); err != nil {
		t.Fatalf("tenant query: %v", err)
	}
	if err := d.Query(context.Background(), "SELECT 2", nil, nil); err == nil {
		t.Fatal("missing tenant must fail closed in enforce")
	}

	reg := prometheus.NewRegistry()
	if err := reg.Register(newMetricsCollector(d)); err != nil {
		t.Fatalf("register collector: %v", err)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	got := map[string]float64{}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			if m.GetCounter() != nil {
				got[mf.GetName()] = m.GetCounter().GetValue()
			} else if m.GetGauge() != nil {
				got[mf.GetName()] = m.GetGauge().GetValue()
			}
		}
	}
	if v := got["itsm_rls_app_routed_total"]; v != 1 {
		t.Fatalf("itsm_rls_app_routed_total=%v, want 1", v)
	}
	if v := got["itsm_rls_missing_tenant_total"]; v != 1 {
		t.Fatalf("itsm_rls_missing_tenant_total=%v, want 1", v)
	}
	if v := got["itsm_rls_app_pool_configured"]; v != 1 {
		t.Fatalf("itsm_rls_app_pool_configured=%v, want 1", v)
	}
	if v := got["itsm_rls_info"]; v != 1 {
		t.Fatalf("itsm_rls_info=%v, want 1", v)
	}

	// 幂等：首次注册成功；重复注册命中 AlreadyRegistered 也应返回 nil。
	if err := RegisterMetrics(d); err != nil {
		t.Fatalf("first RegisterMetrics: %v", err)
	}
	if err := RegisterMetrics(d); err != nil {
		t.Fatalf("second RegisterMetrics must tolerate AlreadyRegistered: %v", err)
	}
}
