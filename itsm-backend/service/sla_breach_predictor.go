package service

import (
	"fmt"
	"math"
	"sync"
)

// 本文件实现 SLA 违约**风险预测**。
//
// ## 来源与边界（移植声明）
//
// 移植自上游 `heidsoft/itsm` 的 `service/sla_breach_predictor.go`（2026-10-xx，纯 `fmt`/`math` 实现），
// 移植批次：`docs/plan/upstream-absorption-plan-2026-11-06.md` P1 试点。
// **本批只移植库与测试，未接线**（无路由/无调用方），行为对齐以上游 `*_test.go` 为规格。
// 本地加固：`prioritySpeedFactor` 由 `AdjustSpeedFactors` 改写、`Predict` 读取，
// 上游无锁（并发读写即 data race）；此处加 `RWMutex`，不改变任何输入→输出行为。
//
// ## 为什么需要它
//
// 上游 `ai/eval` 的预测 AUC 门禁此前把分数直接取自数据集自带的
// `breach_probability_min` —— 分数与标签构造上一致，AUC 恒 1.00，门禁不测任何生产代码。
// 运维真正需要的是「这单会不会超时」要在超时之前知道。
//
// ## 两层结构
//
//	排序层（模型）：预计解决时长 / SLA 窗口 = riskRatio；比值越大越可能违约，可解释。
//	校准层（映射）：把比值映射成概率（sigmoid），参数来自标注样本，可随线上反馈再校准。
//
// AUC 只看排序，不看概率绝对值；因此校准参数调整不破坏排序能力。
// 注意：这是**排序工具**，不是「必然违约」判定器。

// SLABreachFeatures 是违约风险预测的输入特征。
//
// 字段与 SLA 语义一一对应（都能在生产里取到），不是为评测凑的数：
//
//	HistoricalAvgResolutionHours —— 该类工单的历史平均解决时长
//	SimilarTicketsLast30d        —— 近 30 天相似工单量（队列负载代理）
//	HorizonMinutes               —— SLA 窗口（从创建到解决的上限）
//	Priority                     —— 优先级
//	CreatedHour                  —— 创建时刻（小时）
//	Weekday                      —— 星期（0=周日）
type SLABreachFeatures struct {
	HistoricalAvgResolutionHours float64 `json:"historical_avg_resolution_hours"`
	SimilarTicketsLast30d        int     `json:"similar_tickets_last_30d"`
	HorizonMinutes               int     `json:"horizon_minutes"`
	Priority                     string  `json:"priority"`
	CreatedHour                  int     `json:"created_hour"`
	Weekday                      int     `json:"weekday"`
}

// SLABreachRisk 是违约风险预测的输出。
type SLABreachRisk struct {
	// RiskRatio 是预计解决时长与 SLA 窗口的比值：>1 表示按历史速度会超时。
	RiskRatio float64 `json:"riskRatio"`
	// Probability 是校准后的违约概率（0-1），仅用于同批工单排序比较。
	Probability float64 `json:"probability"`
	// ExpectedResolutionHours 是按历史速度与负载推算的预计解决时长。
	ExpectedResolutionHours float64 `json:"expectedResolutionHours"`
}

// SLABreachPredictor 违约风险预测器。零值即可用（无状态、无外部依赖）。
type SLABreachPredictor struct{}

// NewSLABreachPredictor 构造预测器。
func NewSLABreachPredictor() *SLABreachPredictor { return &SLABreachPredictor{} }

// speedFactorMu 保护 prioritySpeedFactor：AdjustSpeedFactors 写、Predict 读。
var speedFactorMu sync.RWMutex

// prioritySpeedFactor 是各优先级的「历史时长修正系数」。
//
// 语义：紧急单往往走快速通道（系数 <1），低优先级单可能排队（系数 >1）。
// 这些系数是**假设**，不是实测 —— 一旦有线上反馈应替换为实测值（见 AdjustSpeedFactors）。
// 集中在此处而不是散落代码，是为了让「换实测值」是一处改动。
var prioritySpeedFactor = map[string]float64{
	"critical": 0.7,
	"high":     0.85,
	"medium":   1.0,
	"low":      1.2,
}

// queueLoadFactor 是队列负载对解决时长的放大系数上限。
//
// 相似工单越多，单个工单实际等待越长；用对数饱和而非线性：负载 10→20 的影响远大于 100→110。
const queueLoadFactor = 0.6

// Predict 计算违约风险。
//
// 输入非法时返回错误而不是猜一个值：特征缺失时给出的概率是编造的，而
// 编造的概率会被当成真实信号进入告警与调度。
func (p *SLABreachPredictor) Predict(f SLABreachFeatures) (*SLABreachRisk, error) {
	if f.HistoricalAvgResolutionHours <= 0 {
		return nil, fmt.Errorf("sla breach predict: historical_avg_resolution_hours 必须为正")
	}
	if f.HorizonMinutes <= 0 {
		return nil, fmt.Errorf("sla breach predict: horizon_minutes 必须为正")
	}

	// 1) 预计解决时长 = 历史均值 × 优先级系数 × 队列负载因子
	speed := 1.0
	speedFactorMu.RLock()
	if v, ok := prioritySpeedFactor[f.Priority]; ok {
		speed = v
	}
	speedFactorMu.RUnlock()
	load := 1.0 + queueLoadFactor*math.Log10(float64(f.SimilarTicketsLast30d+1))
	expectedHours := f.HistoricalAvgResolutionHours * speed * load

	// 2) 风险比值 = 预计解决时长 / SLA 窗口
	slaHours := float64(f.HorizonMinutes) / 60.0
	riskRatio := expectedHours / slaHours

	// 3) 概率：对数几率映射。比值 1（正好压线）对应 0.5 附近。
	//    斜率 1.6 是校准参数，应随线上反馈调整。
	const slope = 1.6
	probability := 1.0 / (1.0 + math.Exp(-slope*(riskRatio-1.0)))
	// 夹在开区间 (0,1)：sigmoid 在极端输入下会舍入成 0 或 1，
	// 而「绝对确定」不是排序工具该输出的结论（会被误读成「必然违约」并触发自动化动作）。
	if probability < 0.0001 {
		probability = 0.0001
	}
	if probability > 0.9999 {
		probability = 0.9999
	}
	probability = math.Round(probability*10000) / 10000

	return &SLABreachRisk{
		RiskRatio:               math.Round(riskRatio*1000) / 1000,
		Probability:             probability,
		ExpectedResolutionHours: math.Round(expectedHours*100) / 100,
	}, nil
}

// AdjustSpeedFactors 替换优先级修正系数（线上实测后调用）。
//
// 仅接受正数（非正系数会让预计时长归零或为负，属无意义输入，忽略即可）。
// 替换后需重跑预测门禁确认排序能力未退化：概率值变化是预期的，AUC 大幅下降才是问题。
func AdjustSpeedFactors(factors map[string]float64) {
	speedFactorMu.Lock()
	defer speedFactorMu.Unlock()
	for k, v := range factors {
		if v <= 0 {
			continue
		}
		prioritySpeedFactor[k] = v
	}
}
