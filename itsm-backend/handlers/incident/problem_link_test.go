package incident

import (
	"testing"

	"itsm-backend/ent"

	"github.com/stretchr/testify/assert"
)

// TestToDomain_ExposesProblemID 锁定事件详情关联问题入口：
// CreateProblemFromIncident 在 problem 侧 AddIncidentIDs 建边，事件详情必须
// 从 incident.problems 反向边回填 problemId，前端才能显示“查看问题”。
func TestToDomain_ExposesProblemID(t *testing.T) {
	repo := &EntRepository{}
	incidentEntity := &ent.Incident{
		ID:    7,
		Title: "converted incident",
		Edges: ent.IncidentEdges{
			Problems: []*ent.Problem{{ID: 42}},
		},
	}
	got := repo.toDomain(incidentEntity)
	assert.NotNil(t, got.ProblemID)
	assert.Equal(t, 42, *got.ProblemID)
}

func TestFirstProblemID_NilOrEmpty(t *testing.T) {
	assert.Nil(t, firstProblemID(nil))
	assert.Nil(t, firstProblemID(&ent.Incident{}))
}