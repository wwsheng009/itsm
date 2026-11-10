package ticket

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 工单详情响应需把 SLA 暂停态与评分明细一并下发，避免桌面端只能通过
// 额外接口拼接状态、或在刷新后丢失暂停原因。
func TestTicketToResponseIncludesSLAAndRating(t *testing.T) {
	pausedAt := time.Date(2026, 11, 10, 8, 30, 0, 0, time.UTC)
	ratedAt := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	rating := 5
	ratingComment := "恢复很快"
	ratedBy := 9

	resp := ticketToResponse(&Ticket{
		ID:             7,
		TicketNumber:   "TKT-7",
		Title:          "VPN 无法连接",
		Status:         "resolved",
		Priority:       "high",
		Type:           "incident",
		RequesterID:    9,
		TenantID:       2,
		SLAStatus:      "paused",
		SLAPausedAt:    &pausedAt,
		SLAPauseReason: "等待客户确认",
		Rating:         &rating,
		RatingComment:  &ratingComment,
		RatedAt:        &ratedAt,
		RatedBy:        &ratedBy,
		CreatedAt:      ratedAt,
		UpdatedAt:      ratedAt,
	})

	require.NotNil(t, resp)
	assert.Equal(t, "paused", resp.SLAStatus)
	require.NotNil(t, resp.SLAPausedAt)
	assert.Equal(t, pausedAt, *resp.SLAPausedAt)
	assert.Equal(t, "等待客户确认", resp.SLAPauseReason)
	assert.Equal(t, 5, resp.Rating)
	assert.Equal(t, "恢复很快", resp.RatingComment)
	require.NotNil(t, resp.RatedAt)
	assert.Equal(t, ratedAt, *resp.RatedAt)
	assert.Equal(t, 9, resp.RatedBy)
}
