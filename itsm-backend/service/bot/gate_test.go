package bot

import "testing"

// TestGate 覆盖 BP5 验收：开关默认关闭、显式开启后组件可判定。
func TestGate(t *testing.T) {
	t.Run("默认关闭", func(t *testing.T) {
		g := NewGate(false)
		if g.Enabled() {
			t.Fatal("bot 开关默认应为 false（零行为变化）")
		}
	})

	t.Run("显式开启", func(t *testing.T) {
		g := NewGate(true)
		if !g.Enabled() {
			t.Fatal("bot 开关显式开启后 Enabled 应为 true")
		}
	})

	t.Run("零值等价于关闭", func(t *testing.T) {
		var g Gate
		if g.Enabled() {
			t.Fatal("Gate 零值应为关闭态")
		}
	})
}
