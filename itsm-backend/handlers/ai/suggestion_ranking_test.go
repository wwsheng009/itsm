package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"itsm-backend/service"
)

// O2-01 建议排序权重（先规则）：组内重排、分组契约保持、冷启动中性、并列稳定。

func suggestionIDs(items []assistantContextSuggestion) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

func TestRankAssistantSuggestions_ReweightsWithinKind(t *testing.T) {
	items := []assistantContextSuggestion{
		{ID: "a", Kind: "task", Rank: 0},
		{ID: "b", Kind: "task", Rank: 1},
		{ID: "q1", Kind: "question", Rank: 2},
		{ID: "q2", Kind: "question", Rank: 3},
	}
	weights := map[string]service.SuggestionWeight{
		"a":  {Shown: 100, Clicked: 5, DecayedShown: 100, DecayedClicked: 5},
		"b":  {Shown: 100, Clicked: 50, DecayedShown: 100, DecayedClicked: 50},
		"q1": {Shown: 20, Clicked: 2, DecayedShown: 20, DecayedClicked: 2},
		"q2": {Shown: 20, Clicked: 10, DecayedShown: 20, DecayedClicked: 10},
	}

	out := rankAssistantSuggestions(items, weights)

	// 组内按 CTR 降序；分组顺序保持（任务在前、问题在后）——I0-02 契约不破。
	assert.Equal(t, []string{"b", "a", "q2", "q1"}, suggestionIDs(out))
	assert.Equal(t, []int{0, 1, 2, 3}, []int{out[0].Rank, out[1].Rank, out[2].Rank, out[3].Rank})
	assert.Greater(t, out[0].Score, out[1].Score)
	assert.Greater(t, out[2].Score, out[3].Score)

	// 反馈计数随建议返回（审计/运营可见）。
	assert.NotNil(t, out[0].Feedback)
	assert.Equal(t, 50, out[0].Feedback.Clicked)
	assert.Equal(t, 100, out[0].Feedback.Shown)
	assert.InDelta(t, 50.0, out[0].Feedback.DecayedClicked, 1e-9)
}

func TestRankAssistantSuggestions_ColdStartAndTies(t *testing.T) {
	// 无反馈的卡取全局先验分（与其它无反馈卡同分）→ 并列保持注册表顺序。
	items := []assistantContextSuggestion{
		{ID: "x", Kind: "task"},
		{ID: "y", Kind: "task"},
	}
	out := rankAssistantSuggestions(items, nil)
	assert.Equal(t, []string{"x", "y"}, suggestionIDs(out))
	assert.Nil(t, out[0].Feedback)
	assert.InDelta(t, suggestionPriorCtr, out[0].Score, 1e-9, "无数据 → 经验先验（冷启动中性）")

	// 反馈数据存在但卡片无反馈 → 同样取全局先验（按全局 CTR 平滑）。
	weights := map[string]service.SuggestionWeight{
		"x": {Shown: 100, Clicked: 30, DecayedShown: 100, DecayedClicked: 30},
		"z": {Shown: 100, Clicked: 10, DecayedShown: 100, DecayedClicked: 10},
	}
	out = rankAssistantSuggestions(items, weights)
	prior := globalSuggestionCtr(weights)
	assert.InDelta(t, 0.2, prior, 1e-9)
	assert.InDelta(t, prior, out[1].Score, 1e-9)
	assert.Greater(t, out[0].Score, out[1].Score)

	// 单条/空列表直接返回。
	single := []assistantContextSuggestion{{ID: "only", Kind: "task"}}
	assert.Equal(t, single, rankAssistantSuggestions(single, weights))
	assert.Empty(t, rankAssistantSuggestions(nil, weights))
}

func TestGlobalSuggestionCtr_Fallbacks(t *testing.T) {
	assert.InDelta(t, suggestionPriorCtr, globalSuggestionCtr(nil), 1e-9)
	assert.InDelta(t, suggestionPriorCtr, globalSuggestionCtr(map[string]service.SuggestionWeight{"a": {Shown: 0, Clicked: 3}}), 1e-9)
	assert.InDelta(t, 0.5, globalSuggestionCtr(map[string]service.SuggestionWeight{
		"a": {DecayedShown: 10, DecayedClicked: 5},
		"b": {DecayedShown: 10, DecayedClicked: 5},
	}), 1e-9)
	assert.Equal(t, "weighted", suggestionRankingMode(true))
	assert.Equal(t, "registry", suggestionRankingMode(false))
}

// 忽略负样本：同等点击下，高忽略卡降权但不被否决（λ=0.5 半权计入分母）。
func TestRankAssistantSuggestions_IgnorePenalty(t *testing.T) {
	items := []assistantContextSuggestion{
		{ID: "noisy", Kind: "task"},
		{ID: "clean", Kind: "task"},
	}
	weights := map[string]service.SuggestionWeight{
		"noisy": {Shown: 100, Clicked: 10, Ignored: 60, DecayedShown: 100, DecayedClicked: 10, DecayedIgnored: 60},
		"clean": {Shown: 100, Clicked: 10, DecayedShown: 100, DecayedClicked: 10},
	}

	out := rankAssistantSuggestions(items, weights)
	assert.Equal(t, []string{"clean", "noisy"}, suggestionIDs(out))
	// 无忽略：score = (10 + k·prior) / (100 + k)，prior = 全局衰减 CTR = 20/200 = 0.1。
	assert.InDelta(t, 0.1, out[0].Score, 1e-9)
	// 忽略只降权、不否决：noisy 分数为正且低于 clean。
	assert.Greater(t, out[1].Score, 0.0)
	assert.Greater(t, out[0].Score, out[1].Score)
	assert.Equal(t, 60, out[1].Feedback.Ignored)
}

// 时间衰减：原始计数相同（CTR 相同）时，旧反馈衰减后不再压过新反馈。
func TestRankAssistantSuggestions_DecayOrdering(t *testing.T) {
	items := []assistantContextSuggestion{
		{ID: "stale", Kind: "task"},
		{ID: "fresh", Kind: "task"},
	}
	// 各 40 展示 / 20 点击；stale 的旧反馈已衰减（有效 CTR 0.25 vs 0.5）。
	weights := map[string]service.SuggestionWeight{
		"stale": {Shown: 40, Clicked: 20, DecayedShown: 8, DecayedClicked: 2},
		"fresh": {Shown: 40, Clicked: 20, DecayedShown: 40, DecayedClicked: 20},
	}

	out := rankAssistantSuggestions(items, weights)
	assert.Equal(t, []string{"fresh", "stale"}, suggestionIDs(out))
	assert.Greater(t, out[0].Score, out[1].Score)
}
