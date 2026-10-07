package authz

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// MSW-2 决策表：tenant.switch 授权判定单一入口（Q1=C：邀请须客户侧显式确认）。
func TestDecideTenantSwitch_DecisionTable(t *testing.T) {
	providerAdmin := SwitchActor{HomeTenantID: 1, HomeTenantType: "msp_provider", Role: "admin", MSPRole: "provider_admin", Active: true}
	foundCustomer := SwitchScope{TargetTenantID: 2, TargetFound: true, TargetType: "msp_customer", TargetStatus: "active"}

	cases := []struct {
		name  string
		actor SwitchActor
		scope SwitchScope
		facts SwitchFacts
		want  SwitchDecision
	}{
		{
			name:  "native 切回本租户",
			actor: providerAdmin,
			scope: SwitchScope{TargetTenantID: 1, TargetFound: true, TargetType: "msp_provider", TargetStatus: "active"},
			want:  SwitchDecision{Allowed: true, Basis: SwitchBasisNative},
		},
		{
			name:  "super_admin 切任意 active 租户",
			actor: SwitchActor{HomeTenantID: 9, HomeTenantType: "default", Role: "super_admin", Active: true},
			scope: foundCustomer,
			want:  SwitchDecision{Allowed: true, Basis: SwitchBasisSuperAdmin},
		},
		{
			name:  "provider+active allocation 放行",
			actor: providerAdmin,
			scope: foundCustomer,
			facts: SwitchFacts{HasActiveAllocation: true},
			want:  SwitchDecision{Allowed: true, Basis: SwitchBasisAllocation},
		},
		{
			name:  "provider+客户侧确认的 invite membership 放行（Q1=C）",
			actor: providerAdmin,
			scope: foundCustomer,
			facts: SwitchFacts{HasActiveInvite: true},
			want:  SwitchDecision{Allowed: true, Basis: SwitchBasisInvite},
		},
		{
			name:  "allocation 优先于 invite（依据稳定）",
			actor: providerAdmin,
			scope: foundCustomer,
			facts: SwitchFacts{HasActiveAllocation: true, HasActiveInvite: true},
			want:  SwitchDecision{Allowed: true, Basis: SwitchBasisAllocation},
		},
		{
			name:  "停用账号一律拒绝（不得换发令牌）",
			actor: SwitchActor{HomeTenantID: 1, HomeTenantType: "msp_provider", Role: "admin", MSPRole: "provider_admin", Active: false},
			scope: foundCustomer,
			facts: SwitchFacts{HasActiveAllocation: true, HasActiveInvite: true},
			want:  SwitchDecision{Reason: SwitchDenyUserInactive},
		},
		{
			name:  "普通客户用户跨租户拒绝（invite 不泛化）",
			actor: SwitchActor{HomeTenantID: 3, HomeTenantType: "msp_customer", Role: "end_user", Active: true},
			scope: foundCustomer,
			facts: SwitchFacts{HasActiveInvite: true},
			want:  SwitchDecision{Reason: SwitchDenyNoGrant},
		},
		{
			name:  "provider 无 allocation/invite 拒绝",
			actor: providerAdmin,
			scope: foundCustomer,
			want:  SwitchDecision{Reason: SwitchDenyNoGrant},
		},
		{
			name:  "provider 目标非客户租户拒绝",
			actor: providerAdmin,
			scope: SwitchScope{TargetTenantID: 2, TargetFound: true, TargetType: "msp_provider", TargetStatus: "active"},
			facts: SwitchFacts{HasActiveAllocation: true},
			want:  SwitchDecision{Reason: SwitchDenyNoGrant},
		},
		{
			name:  "非 provider 家租户的 msp_role 账号拒绝（类型门槛）",
			actor: SwitchActor{HomeTenantID: 3, HomeTenantType: "msp_customer", Role: "admin", MSPRole: "provider_admin", Active: true},
			scope: foundCustomer,
			facts: SwitchFacts{HasActiveInvite: true},
			want:  SwitchDecision{Reason: SwitchDenyNoGrant},
		},
		{
			name:  "provider 目标不存在不暴露（no_grant）",
			actor: providerAdmin,
			scope: SwitchScope{TargetTenantID: 2, TargetFound: false},
			want:  SwitchDecision{Reason: SwitchDenyNoGrant},
		},
		{
			name:  "super_admin 目标不存在（target_not_found）",
			actor: SwitchActor{HomeTenantID: 9, HomeTenantType: "default", Role: "super_admin", Active: true},
			scope: SwitchScope{TargetTenantID: 2, TargetFound: false},
			want:  SwitchDecision{Reason: SwitchDenyTargetNotFound},
		},
		{
			name:  "native 目标暂停",
			actor: providerAdmin,
			scope: SwitchScope{TargetTenantID: 1, TargetFound: true, TargetType: "msp_provider", TargetStatus: "suspended"},
			want:  SwitchDecision{Reason: SwitchDenyTargetSuspended},
		},
		{
			name:  "provider+allocation 目标过期",
			actor: providerAdmin,
			scope: SwitchScope{TargetTenantID: 2, TargetFound: true, TargetType: "msp_customer", TargetStatus: "active", TargetExpired: true},
			facts: SwitchFacts{HasActiveAllocation: true},
			want:  SwitchDecision{Reason: SwitchDenyTargetExpired},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideTenantSwitch(tc.actor, tc.scope, tc.facts)
			assert.Equal(t, tc.want, got)
		})
	}
}
