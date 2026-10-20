package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAmbiguousWriteReason(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "序数指代与随便选择同时出现",
			query: "直接提交第二张单 + 随便挂个资产",
			want:  "第二张单",
		},
		{
			name:  "近指代",
			query: "把那张单改成高优先级",
			want:  "那张单",
		},
		{
			name:  "任意选择",
			query: "随便找台服务器挂上去",
			want:  "随便",
		},
		{
			name:  "明确编号不触发",
			query: "提交 INC-20261020-0007，并关联 CI:202609:000001",
			want:  "",
		},
		{
			name:  "用户已确认序数指代不触发",
			query: "确认按第二张单提交，类型是事件单",
			want:  "",
		},
		{
			name:  "只说确认但未给类型仍触发",
			query: "确认第二张单，提交吧",
			want:  "第二张单",
		},
		{
			name:  "确认但仍随意选择资产仍触发",
			query: "确认第二张单，资产随便挂一个",
			want:  "第二张单",
		},
		{
			name:  "普通建单不触发",
			query: "我的笔记本开不了机，帮我建一张事件单",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ambiguousWriteReason(tc.query))
		})
	}
}
