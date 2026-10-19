package ai

import (
	"sort"

	"itsm-backend/service"
)

// O2-01 建议排序权重（先规则，后模型）：按近窗口反馈 CTR 组内重排。
//
// 规则：
//   - 仅**组内**重排（任务卡/问题卡各自排序），保持 I0-02「任务在前」分组契约；
//   - 打分 = 拉普拉斯平滑 `(clicked' + k·prior) / (shown' + λ·ignored' + k)`（k=10，λ=0.5）：
//     `x'` 为服务层半衰期衰减后的有效计数（近期反馈主导、旧反馈自然退场）；
//     无反馈的卡取全局先验分（冷启动中性），有反馈的卡按真实 CTR 升降；
//   - 忽略负样本：`ignored`（展示后未点击）按 λ 半权计入分母——比普通曝光更强的负证据、
//     不与点击同权，且随时间衰减（一张卡可凭近期表现恢复排名）；
//   - 并列保持注册表顺序（稳定排序）。

const (
	suggestionPriorStrength = 10.0
	suggestionPriorCtr      = 0.2
	// 忽略样本惩罚系数（λ）：隐式负样本（展示后未点击）相对普通曝光的增量惩罚。
	suggestionIgnorePenalty = 0.5
)

// globalSuggestionCtr 全局 CTR（衰减口径；无展示数据 → 经验先验）。
func globalSuggestionCtr(weights map[string]service.SuggestionWeight) float64 {
	shown, clicked := 0.0, 0.0
	for _, weight := range weights {
		shown += weight.DecayedShown
		clicked += weight.DecayedClicked
	}
	if shown <= 0 {
		return suggestionPriorCtr
	}
	return clicked / shown
}

// suggestionScore 拉普拉斯平滑打分（k = suggestionPriorStrength；忽略负样本按 λ 计入分母）。
func suggestionScore(weight service.SuggestionWeight, prior float64) float64 {
	return (weight.DecayedClicked + suggestionPriorStrength*prior) /
		(weight.DecayedShown + suggestionIgnorePenalty*weight.DecayedIgnored + suggestionPriorStrength)
}

// rankAssistantSuggestions 组内重排并重编号 rank（返回新切片，不修改入参）。
func rankAssistantSuggestions(items []assistantContextSuggestion, weights map[string]service.SuggestionWeight) []assistantContextSuggestion {
	if len(items) <= 1 {
		return items
	}
	prior := globalSuggestionCtr(weights)

	groupOrder := make([]string, 0, 2)
	groups := make(map[string][]assistantContextSuggestion, 2)
	for _, item := range items {
		weight, hasFeedback := weights[item.ID]
		item.Score = suggestionScore(weight, prior)
		if !hasFeedback {
			item.Score = suggestionScore(service.SuggestionWeight{}, prior)
			item.Feedback = nil
		} else {
			copied := weight
			item.Feedback = &copied
		}
		if _, ok := groups[item.Kind]; !ok {
			groupOrder = append(groupOrder, item.Kind)
		}
		groups[item.Kind] = append(groups[item.Kind], item)
	}

	out := make([]assistantContextSuggestion, 0, len(items))
	for _, kind := range groupOrder {
		group := groups[kind]
		sort.SliceStable(group, func(i, j int) bool { return group[i].Score > group[j].Score })
		out = append(out, group...)
	}
	for index := range out {
		out[index].Rank = index
	}
	return out
}

// suggestionRankingMode 归一化排序模式（响应字段；便于前端/审计区分）。
func suggestionRankingMode(weighted bool) string {
	if weighted {
		return "weighted"
	}
	return "registry"
}
