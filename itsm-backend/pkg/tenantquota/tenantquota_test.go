package tenantquota

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseValidAndOmitted(t *testing.T) {
	limits, err := Parse(nil)
	if err != nil || !limits.IsZero() {
		t.Fatalf("nil → 零值不限, got %+v err=%v", limits, err)
	}
	limits, err = Parse(map[string]interface{}{
		"maxUsers":           5,
		"maxTicketsPerMonth": json.Number("100"),
		"maxStorageMB":       10.0,
	})
	if err != nil {
		t.Fatalf("valid parse: %v", err)
	}
	if limits.MaxUsers != 5 || limits.MaxTicketsPerMonth != 100 || limits.MaxStorageMB != 10 {
		t.Fatalf("unexpected limits: %+v", limits)
	}
	// 0 与缺省同义（不限），且 ToMap 不下发。
	limits, err = Parse(map[string]interface{}{"maxUsers": 0, "maxStorageMB": 0})
	if err != nil || !limits.IsZero() {
		t.Fatalf("zero → 不限, got %+v err=%v", limits, err)
	}
}

func TestParseRejects(t *testing.T) {
	cases := []map[string]interface{}{
		{"unknown": 1},
		{"maxUsers": -1},
		{"maxUsers": 1.5},
		{"maxUsers": "10"},
		{"maxUsers": true},
		{"maxUsers": int64(1) << 41},
	}
	for _, raw := range cases {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("期望拒绝: %#v", raw)
		} else {
			var invalid *InvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("错误类型应为 *InvalidError: %#v -> %v", raw, err)
			}
		}
	}
}

func TestToMapAndStorageLimit(t *testing.T) {
	limits := Limits{MaxUsers: 3, MaxTicketsPerMonth: 0, MaxStorageMB: 2}
	m := limits.ToMap()
	if m[QuotaMaxUsers] != int64(3) {
		t.Fatalf("maxUsers 应在 map 中: %#v", m)
	}
	if _, ok := m[QuotaMaxTicketsPerMonth]; ok {
		t.Fatalf("0 值键不应下发: %#v", m)
	}
	if got := limits.StorageLimitBytes(); got != 2*1024*1024 {
		t.Fatalf("StorageLimitBytes = %d", got)
	}
	if got := (Limits{}).StorageLimitBytes(); got != 0 {
		t.Fatalf("不限时 StorageLimitBytes = %d", got)
	}
}

func TestChecks(t *testing.T) {
	limits := Limits{MaxUsers: 2, MaxTicketsPerMonth: 1, MaxStorageMB: 1}

	if exceeded := limits.CheckUsers(1); exceeded != nil {
		t.Fatalf("used=1 limit=2 应放行: %v", exceeded)
	}
	exceeded := limits.CheckUsers(2)
	if exceeded == nil || exceeded.Quota != QuotaMaxUsers || exceeded.Limit != 2 || exceeded.Used != 2 {
		t.Fatalf("used=2 limit=2 应超限: %+v", exceeded)
	}

	if exceeded := limits.CheckTicketsThisMonth(0); exceeded != nil {
		t.Fatalf("used=0 limit=1 应放行: %v", exceeded)
	}
	if exceeded := limits.CheckTicketsThisMonth(1); exceeded == nil || exceeded.Quota != QuotaMaxTicketsPerMonth {
		t.Fatalf("used=1 limit=1 应超限: %+v", exceeded)
	}

	if exceeded := limits.CheckStorage(100, 1); exceeded != nil {
		t.Fatalf("100B+1B limit=1MB 应放行: %v", exceeded)
	}
	if exceeded := limits.CheckStorage(0, 1<<20+1); exceeded == nil || exceeded.Quota != QuotaMaxStorageMB {
		t.Fatalf("1MB+1B limit=1MB 应超限: %+v", exceeded)
	}

	// 不限（零值）永不超限。
	unlimited := Limits{}
	if unlimited.CheckUsers(1<<40) != nil || unlimited.CheckTicketsThisMonth(1<<40) != nil || unlimited.CheckStorage(1<<40, 1<<40) != nil {
		t.Fatal("零值必须不限")
	}
}

func TestAsExceeded(t *testing.T) {
	exceeded := &ExceededError{Quota: QuotaMaxUsers, Limit: 1, Used: 1}
	got, ok := AsExceeded(exceeded)
	if !ok || got != exceeded {
		t.Fatalf("AsExceeded 未命中: %v %v", got, ok)
	}
	if _, ok := AsExceeded(errors.New("other")); ok {
		t.Fatal("普通错误不应命中")
	}
}
