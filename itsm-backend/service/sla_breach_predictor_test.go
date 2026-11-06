package service

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件守卫 SLA 违约预测器的性质。前四个用例移植自上游同名测试（作行为规格），
// 最后一个用例针对本地加固（RWMutex 保护的系数替换）——上游没有这条。

// TestPredictorRanksRiskCorrectly 锁住排序能力（AUC 只看排序）：
// 历史均值超过 SLA 窗口的工单，风险分必须高于历史均值远低于窗口的工单。
func TestPredictorRanksRiskCorrectly(t *testing.T) {
	p := NewSLABreachPredictor()

	high, err := p.Predict(SLABreachFeatures{
		HistoricalAvgResolutionHours: 8,
		HorizonMinutes:               240, // 4h 窗口，历史 8h → 必超
		Priority:                     "medium",
		SimilarTicketsLast30d:        5,
	})
	require.NoError(t, err)

	low, err := p.Predict(SLABreachFeatures{
		HistoricalAvgResolutionHours: 1,
		HorizonMinutes:               240,
		Priority:                     "medium",
		SimilarTicketsLast30d:        5,
	})
	require.NoError(t, err)

	assert.Greater(t, high.Probability, low.Probability,
		"历史均值 8h/窗口 4h 的风险必须高于 1h/4h")
	assert.Greater(t, high.RiskRatio, 1.0, "超窗口的比值应 >1")
	assert.Less(t, low.RiskRatio, 1.0, "远低于窗口的比值应 <1")
}

// TestPredictorQueueLoadIncreasesRisk 队列负载加重必须推高风险。
func TestPredictorQueueLoadIncreasesRisk(t *testing.T) {
	p := NewSLABreachPredictor()
	base := SLABreachFeatures{
		HistoricalAvgResolutionHours: 2,
		HorizonMinutes:               240,
		Priority:                     "high",
		SimilarTicketsLast30d:        1,
	}
	light, err := p.Predict(base)
	require.NoError(t, err)

	heavy := base
	heavy.SimilarTicketsLast30d = 100
	loaded, err := p.Predict(heavy)
	require.NoError(t, err)

	assert.Greater(t, loaded.Probability, light.Probability,
		"队列负载 100 单的风险必须高于 1 单")
}

// TestPredictorRejectsInvalidFeatures 非法特征报错而不是猜值。
func TestPredictorRejectsInvalidFeatures(t *testing.T) {
	p := NewSLABreachPredictor()
	_, err := p.Predict(SLABreachFeatures{HistoricalAvgResolutionHours: 0, HorizonMinutes: 240})
	require.Error(t, err, "历史均值为 0 必须报错")

	_, err = p.Predict(SLABreachFeatures{HistoricalAvgResolutionHours: 1, HorizonMinutes: 0})
	require.Error(t, err, "SLA 窗口为 0 必须报错")
}

// TestPredictorProbabilityRange 概率必须落在 (0,1) 开区间。
func TestPredictorProbabilityRange(t *testing.T) {
	p := NewSLABreachPredictor()
	for _, f := range []SLABreachFeatures{
		{HistoricalAvgResolutionHours: 0.01, HorizonMinutes: 100000, Priority: "low", SimilarTicketsLast30d: 0},
		{HistoricalAvgResolutionHours: 1000, HorizonMinutes: 1, Priority: "critical", SimilarTicketsLast30d: 999},
	} {
		risk, err := p.Predict(f)
		require.NoError(t, err)
		assert.Greater(t, risk.Probability, 0.0)
		assert.Less(t, risk.Probability, 1.0, "排序工具不得输出绝对确定的 0 或 1")
	}
}

// TestAdjustSpeedFactorsAppliesAndIgnoresInvalid 守卫本地加固（上游无此用例）：
// ① 系数替换立即生效；② 非正系数被忽略；③ 读写并发不 panic（配合 `go test -race` 可验数据竞争）。
func TestAdjustSpeedFactorsAppliesAndIgnoresInvalid(t *testing.T) {
	p := NewSLABreachPredictor()
	f := SLABreachFeatures{
		HistoricalAvgResolutionHours: 2,
		HorizonMinutes:               240,
		Priority:                     "medium",
		SimilarTicketsLast30d:        0,
	}
	speedFactorMu.RLock()
	original := prioritySpeedFactor["medium"]
	speedFactorMu.RUnlock()
	defer AdjustSpeedFactors(map[string]float64{"medium": original})

	before, err := p.Predict(f)
	require.NoError(t, err)

	AdjustSpeedFactors(map[string]float64{"medium": 2.0, "bogus": -1})
	after, err := p.Predict(f)
	require.NoError(t, err)
	assert.Greater(t, after.ExpectedResolutionHours, before.ExpectedResolutionHours,
		"把 medium 系数从 1.0 调到 2.0，预计时长必须翻倍上升")
	assert.InDelta(t, before.ExpectedResolutionHours*2, after.ExpectedResolutionHours, 0.02,
		"翻倍关系应精确（除舍入外）")

	// 并发读写：验证 Mutex 生效（-race 下才有意义；普通运行至少不 panic）
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = p.Predict(f) }()
		go func() { defer wg.Done(); AdjustSpeedFactors(map[string]float64{"high": 0.9}) }()
	}
	wg.Wait()
}
