package ticket

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"itsm-backend/common"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/repository/base"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

type stubSequenceProvider struct {
	values []int64
	index  int
}

func (s *stubSequenceProvider) GetNextSequenceWithExpiry(_ context.Context, _ string, _ time.Time) (int64, error) {
	if s.index >= len(s.values) {
		return 0, fmt.Errorf("no more sequence values")
	}
	value := s.values[s.index]
	s.index++
	return value, nil
}

func TestRepository_SetSequenceService_TypedNilUsesDatabaseFallback(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	repo := fx.repo.(*EntRepository)
	var unavailableSequenceService *stubSequenceProvider
	repo.SetSequenceService(unavailableSequenceService)
	require.Nil(t, repo.sequenceService)

	tkt, err := repo.Create(fx.ctx, &CreateParams{
		Title:       "Database fallback ticket",
		Description: "Redis is unavailable",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, tkt.TicketNumber)
}

// repoFixture sets up an in-memory SQLite repo for testing.
type repoFixture struct {
	ctx    context.Context
	client *ent.Client
	repo   Repository
	tenant *ent.Tenant
	user   *ent.User
}

func newRepoFixture(t *testing.T) *repoFixture {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:repo_test?mode=memory&cache=shared&_fk=1")
	logger := zaptest.NewLogger(t).Sugar()
	repo := NewEntRepository(client, logger)

	tenant, err := client.Tenant.Create().
		SetName("Test Tenant").
		SetCode("test").
		SetDomain("test.com").
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	user, err := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@test.com").
		SetName("Alice").
		SetPasswordHash("hash").
		SetRole("end_user").
		SetActive(true).
		SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	return &repoFixture{ctx: ctx, client: client, repo: repo, tenant: tenant, user: user}
}

// =====================================================================
// Create
// =====================================================================

func TestRepository_Create(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	params := &CreateParams{
		Title:       "Test Ticket",
		Description: "Description",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}

	tkt, err := fx.repo.Create(fx.ctx, params, fx.tenant.ID)
	require.NoError(t, err)
	assert.NotZero(t, tkt.ID)
	assert.Equal(t, "Test Ticket", tkt.Title)
	assert.Equal(t, StatusNew, tkt.Status)
}

func TestRepository_UpdateWithTxHookRollsBackTicketWhenHookFails(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	repo := fx.repo.(*EntRepository)
	created, err := repo.Create(fx.ctx, &CreateParams{
		Title: "Before", Description: "atomic update", Priority: PriorityMedium,
		Type: TypeIncident, RequesterID: fx.user.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)

	after := "After"
	updated, err := repo.UpdateWithTxHook(fx.ctx, created.ID, &UpdateParams{
		Title: &after, Version: created.Version,
	}, fx.tenant.ID, func(_ *ent.Tx, _ *Ticket) error {
		return fmt.Errorf("outbox unavailable")
	})
	require.ErrorContains(t, err, "outbox unavailable")
	require.Nil(t, updated)

	stored, err := repo.GetByID(fx.ctx, created.ID, fx.tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, "Before", stored.Title)
	assert.Equal(t, created.Version, stored.Version)
}

func TestRepository_Create_GeneratesTicketNumber(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	params := &CreateParams{
		Title:       "Number Test",
		Description: "",
		Priority:    PriorityLow,
		Type:        TypeServiceRequest,
		RequesterID: fx.user.ID,
	}

	tkt, err := fx.repo.Create(fx.ctx, params, fx.tenant.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, tkt.TicketNumber)
	assert.Contains(t, tkt.TicketNumber, "TKT-")
}

func TestRepository_Create_RetriesOnTicketNumberConflict(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	concreteRepo, ok := fx.repo.(*EntRepository)
	require.True(t, ok)

	now := time.Now()
	existingNumber := fmt.Sprintf("TKT-%04d%02d-%06d", now.Year(), int(now.Month()), 1)
	_, err := fx.client.Ticket.Create().
		SetTitle("Existing Ticket").
		SetDescription("Existing").
		SetType(string(TypeIncident)).
		SetPriority(string(PriorityLow)).
		SetTicketNumber(existingNumber).
		SetRequesterID(fx.user.ID).
		SetTenantID(fx.tenant.ID).
		SetStatus(string(StatusNew)).
		Save(fx.ctx)
	require.NoError(t, err)

	concreteRepo.SetSequenceService(&stubSequenceProvider{values: []int64{1, 2}})

	tkt, err := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Retry Create",
		Description: "Conflict then retry",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)
	assert.NotEqual(t, existingNumber, tkt.TicketNumber)
	assert.Equal(t, fmt.Sprintf("TKT-%04d%02d-%06d", now.Year(), int(now.Month()), 2), tkt.TicketNumber)
}

func TestRepository_Create_DifferentTenants(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	params := &CreateParams{
		Title:       "Cross Tenant",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}

	tkt1, err := fx.repo.Create(fx.ctx, params, fx.tenant.ID)
	require.NoError(t, err)

	// Tenant2 should not see tenant1's ticket
	_, err = fx.repo.GetByID(fx.ctx, tkt1.ID, 99999)
	assert.Error(t, err)
}

// =====================================================================
// GetByID
// =====================================================================

func TestRepository_GetByID(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Get Test",
		Description: "Desc",
		Priority:    PriorityHigh,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	found, err := fx.repo.GetByID(fx.ctx, created.ID, fx.tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, "Get Test", found.Title)
}

func TestRepository_GetByID_NotFound(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	_, err := fx.repo.GetByID(fx.ctx, 99999, fx.tenant.ID)
	assert.Error(t, err)
}

func TestRepository_GetByID_WrongTenant(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Tenant Isolation",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	_, err := fx.repo.GetByID(fx.ctx, created.ID, 99999)
	assert.Error(t, err)
}

// =====================================================================
// GetByNumber
// =====================================================================

func TestRepository_GetByNumber(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "By Number",
		Description: "",
		Priority:    PriorityLow,
		Type:        TypeServiceRequest,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	found, err := fx.repo.GetByNumber(fx.ctx, created.TicketNumber, fx.tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, created.TicketNumber, found.TicketNumber)
}

func TestRepository_GetByNumber_NotFound(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	_, err := fx.repo.GetByNumber(fx.ctx, "TKT-DOES-NOT-EXIST", fx.tenant.ID)
	assert.Error(t, err)
}

func TestRepository_GetByNumber_WrongTenant(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "By Number Tenant",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	_, err := fx.repo.GetByNumber(fx.ctx, created.TicketNumber, 99999)
	assert.Error(t, err)
}

// =====================================================================
// Update
// =====================================================================

func TestRepository_Update(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Update Me",
		Description: "Original",
		Priority:    PriorityLow,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	title := "Updated Title"
	desc := "Updated Description"

	updated, err := fx.repo.Update(fx.ctx, created.ID, &UpdateParams{
		Title:       &title,
		Description: &desc,
		Priority:    func() *Priority { p := PriorityCritical; return &p }(),
		Version:     created.Version,
	}, fx.tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", updated.Title)
	assert.Equal(t, "Updated Description", updated.Description)
	assert.Equal(t, PriorityCritical, updated.Priority)
}

func TestRepository_Update_NotFound(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	title := "Should Fail"
	_, err := fx.repo.Update(fx.ctx, 99999, &UpdateParams{
		Title: &title,
	}, fx.tenant.ID)
	assert.Error(t, err)
}

func TestRepository_Update_RejectsStaleVersion(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()
	created, err := fx.repo.Create(fx.ctx, &CreateParams{
		Title: "Original", Priority: PriorityMedium, Type: TypeIncident, RequesterID: fx.user.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)
	title := "Must not overwrite"
	_, err = fx.repo.Update(fx.ctx, created.ID, &UpdateParams{Title: &title, Version: created.Version + 1}, fx.tenant.ID)
	require.ErrorContains(t, err, "version conflict")
	unchanged, err := fx.repo.GetByID(fx.ctx, created.ID, fx.tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, "Original", unchanged.Title)
}

// =====================================================================
// Delete
// =====================================================================

func TestRepository_Delete(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Delete Me",
		Description: "",
		Priority:    PriorityLow,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	err := fx.repo.Delete(fx.ctx, created.ID, fx.tenant.ID)
	require.NoError(t, err)

	_, err = fx.repo.GetByID(fx.ctx, created.ID, fx.tenant.ID)
	assert.Error(t, err)
}

func TestRepository_Delete_WrongTenant(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Delete Isolated",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	// Wrong tenant delete: implementation may return nil (no rows matched) or error.
	// The key invariant is the ticket still exists for the correct tenant.
	_ = fx.repo.Delete(fx.ctx, created.ID, 99999)

	_, err := fx.repo.GetByID(fx.ctx, created.ID, fx.tenant.ID)
	require.NoError(t, err)
}

// =====================================================================
// List
// =====================================================================

func TestRepository_List(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	for i := 0; i < 5; i++ {
		fx.repo.Create(fx.ctx, &CreateParams{
			Title:       "List Ticket",
			Description: "",
			Priority:    PriorityMedium,
			Type:        TypeIncident,
			RequesterID: fx.user.ID,
		}, fx.tenant.ID)
	}

	result, err := fx.repo.List(fx.ctx, fx.tenant.ID, &FilterParams{}, &base.QueryParams{})
	require.NoError(t, err)
	assert.Equal(t, 5, result.Total)
	assert.Len(t, result.Data, 5)
}

func TestRepository_List_Pagination(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	for i := 0; i < 10; i++ {
		fx.repo.Create(fx.ctx, &CreateParams{
			Title:       "Page Ticket",
			Description: "",
			Priority:    PriorityLow,
			Type:        TypeIncident,
			RequesterID: fx.user.ID,
		}, fx.tenant.ID)
	}

	result, err := fx.repo.List(fx.ctx, fx.tenant.ID, &FilterParams{}, &base.QueryParams{Page: 1, PageSize: 3})
	require.NoError(t, err)
	assert.Equal(t, 10, result.Total)
	assert.Len(t, result.Data, 3)
}

func TestRepository_List_TenantIsolation(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Tenant1 Ticket",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	result, err := fx.repo.List(fx.ctx, 99999, &FilterParams{}, &base.QueryParams{})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Total)
	assert.Len(t, result.Data, 0)
}

func TestRepository_List_ParentTypeAndOverdueFilters(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()
	parent, err := fx.repo.Create(fx.ctx, &CreateParams{
		Title: "Parent", Priority: PriorityMedium, Type: TypeIncident, RequesterID: fx.user.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)
	overdue, err := fx.repo.Create(fx.ctx, &CreateParams{
		Title: "Overdue problem", Priority: PriorityHigh, Type: TypeProblem, RequesterID: fx.user.ID, ParentTicketID: &parent.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)
	resolved, err := fx.repo.Create(fx.ctx, &CreateParams{
		Title: "Resolved problem", Priority: PriorityHigh, Type: TypeProblem, RequesterID: fx.user.ID, ParentTicketID: &parent.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)
	past := time.Now().Add(-time.Hour)
	_, err = fx.client.Ticket.UpdateOneID(overdue.ID).SetSLAResolutionDeadline(past).Save(fx.ctx)
	require.NoError(t, err)
	_, err = fx.client.Ticket.UpdateOneID(resolved.ID).SetSLAResolutionDeadline(past).SetStatus(string(StatusResolved)).Save(fx.ctx)
	require.NoError(t, err)

	problemType := TypeProblem
	result, err := fx.repo.List(fx.ctx, fx.tenant.ID, &FilterParams{
		Type: &problemType, ParentTicketID: &parent.ID, IsOverdue: true,
	}, &base.QueryParams{})
	require.NoError(t, err)
	require.Len(t, result.Data, 1)
	assert.Equal(t, overdue.ID, result.Data[0].ID)
}

// =====================================================================
// BatchDelete
// =====================================================================

func TestRepository_BatchDelete(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	ids := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		tkt, _ := fx.repo.Create(fx.ctx, &CreateParams{
			Title:       "Batch Delete",
			Description: "",
			Priority:    PriorityLow,
			Type:        TypeIncident,
			RequesterID: fx.user.ID,
		}, fx.tenant.ID)
		ids = append(ids, tkt.ID)
	}

	err := fx.repo.BatchDelete(fx.ctx, ids, fx.tenant.ID)
	require.NoError(t, err)

	for _, id := range ids {
		_, err := fx.repo.GetByID(fx.ctx, id, fx.tenant.ID)
		assert.Error(t, err)
	}
}

func TestRepository_BatchDelete_EmptyList(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	err := fx.repo.BatchDelete(fx.ctx, []int{}, fx.tenant.ID)
	assert.NoError(t, err)
}

func TestRepository_BatchDelete_TenantIsolation(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Batch Tenant",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	_ = fx.repo.BatchDelete(fx.ctx, []int{created.ID}, 99999)

	_, err := fx.repo.GetByID(fx.ctx, created.ID, fx.tenant.ID)
	require.NoError(t, err)
}

// =====================================================================
// Exists
// =====================================================================

func TestRepository_Exists(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Exists Check",
		Description: "",
		Priority:    PriorityLow,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	exists, err := fx.repo.Exists(fx.ctx, created.ID, fx.tenant.ID)
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = fx.repo.Exists(fx.ctx, 99999, fx.tenant.ID)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRepository_Exists_WrongTenant(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Exists Tenant",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	exists, err := fx.repo.Exists(fx.ctx, created.ID, 99999)
	require.NoError(t, err)
	assert.False(t, exists)
}

// =====================================================================
// UpdateStatus
// =====================================================================

func TestRepository_UpdateStatus(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Status Update",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	updated, err := fx.repo.UpdateStatus(fx.ctx, created.ID, StatusOpen, fx.tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusOpen, updated.Status)
}

func TestRepository_UpdateStatus_NotFound(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	_, err := fx.repo.UpdateStatus(fx.ctx, 99999, StatusOpen, fx.tenant.ID)
	assert.Error(t, err)
}

// =====================================================================
// AssignTicket
// =====================================================================

func TestRepository_AssignTicket(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	created, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Assign Test",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	updated, err := fx.repo.AssignTicket(fx.ctx, created.ID, fx.user.ID, fx.tenant.ID)
	require.NoError(t, err)
	assert.NotNil(t, updated.AssigneeID)
	assert.Equal(t, fx.user.ID, *updated.AssigneeID)
}

func TestRepository_AssignTicket_NotFound(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	_, err := fx.repo.AssignTicket(fx.ctx, 99999, fx.user.ID, fx.tenant.ID)
	assert.Error(t, err)
}

// =====================================================================
// CountByStatus / CountByPriority
// =====================================================================

func TestRepository_CountByStatus(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	for i := 0; i < 3; i++ {
		tkt, _ := fx.repo.Create(fx.ctx, &CreateParams{
			Title:       "Count Status",
			Description: "",
			Priority:    PriorityMedium,
			Type:        TypeIncident,
			RequesterID: fx.user.ID,
		}, fx.tenant.ID)
		fx.repo.UpdateStatus(fx.ctx, tkt.ID, StatusOpen, fx.tenant.ID)
	}

	fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Count Status New",
		Description: "",
		Priority:    PriorityLow,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	counts, err := fx.repo.CountByStatus(fx.ctx, fx.tenant.ID)
	require.NoError(t, err)
	assert.Contains(t, counts, StatusNew)
	assert.Contains(t, counts, StatusOpen)
}

func TestRepository_CountByPriority(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Priority Count",
		Description: "",
		Priority:    PriorityCritical,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	counts, err := fx.repo.CountByPriority(fx.ctx, fx.tenant.ID)
	require.NoError(t, err)
	assert.Contains(t, counts, PriorityCritical)
}

// =====================================================================
// GenerateTicketNumber
// =====================================================================

func TestRepository_GenerateTicketNumber(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	num, err := fx.repo.GenerateTicketNumber(fx.ctx, fx.tenant.ID)
	require.NoError(t, err)
	assert.Contains(t, num, "TKT-")
	assert.NotEmpty(t, num)
}

func TestRepository_GenerateTicketNumber_Uniqueness(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	seen := make(map[string]struct{})
	prev := ""
	// GenerateTicketNumber is count-based: each call without creating a ticket
	// returns the same counter value. To exercise uniqueness, create a ticket
	// between calls so the count increments.
	for i := 0; i < 20; i++ {
		_, _ = fx.repo.Create(fx.ctx, &CreateParams{
			Title:       "Num Uniq",
			Description: "",
			Priority:    PriorityLow,
			Type:        TypeIncident,
			RequesterID: fx.user.ID,
		}, fx.tenant.ID)

		num, err := fx.repo.GenerateTicketNumber(fx.ctx, fx.tenant.ID)
		require.NoError(t, err)
		assert.NotEmpty(t, num)
		assert.Contains(t, num, "TKT-")
		if prev != "" {
			assert.NotEqual(t, prev, num, "numbers should differ after creating a ticket")
		}
		prev = num
		_, exists := seen[num]
		assert.False(t, exists, "generated ticket number should be unique: %s", num)
		seen[num] = struct{}{}
	}
}

// =====================================================================
// FindByAssignee / FindByRequester
// =====================================================================

func TestRepository_FindByAssignee(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	tkt, _ := fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Assign Find",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)
	fx.repo.AssignTicket(fx.ctx, tkt.ID, fx.user.ID, fx.tenant.ID)

	tickets, err := fx.repo.FindByAssignee(fx.ctx, fx.user.ID, fx.tenant.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(tickets), 1)
}

func TestRepository_FindByRequester(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Requester Find",
		Description: "",
		Priority:    PriorityLow,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	tickets, err := fx.repo.FindByRequester(fx.ctx, fx.user.ID, fx.tenant.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(tickets), 1)
}

func TestRepository_FindByRequester_WrongTenant(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()

	fx.repo.Create(fx.ctx, &CreateParams{
		Title:       "Requester Tenant",
		Description: "",
		Priority:    PriorityMedium,
		Type:        TypeIncident,
		RequesterID: fx.user.ID,
	}, fx.tenant.ID)

	tickets, err := fx.repo.FindByRequester(fx.ctx, fx.user.ID, 99999)
	require.NoError(t, err)
	assert.Empty(t, tickets)
}

// =====================================================================
// State Machine (domain model)
// =====================================================================

func TestTicketModel_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from     Status
		to       Status
		expected bool
	}{
		{StatusNew, StatusOpen, true},
		{StatusNew, StatusCancelled, true},
		{StatusNew, StatusResolved, false},
		{StatusOpen, StatusInProgress, true},
		{StatusOpen, StatusPending, true},
		{StatusOpen, StatusResolved, true},
		{StatusInProgress, StatusPending, true},
		{StatusInProgress, StatusResolved, true},
		{StatusResolved, StatusClosed, true},
		{StatusResolved, StatusOpen, true}, // Reopen
		{StatusClosed, StatusOpen, false},  // Cannot reopen from closed
		{StatusCancelled, StatusOpen, false},
	}

	for _, tt := range tests {
		name := string(tt.from) + "_to_" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			model := &Ticket{Status: tt.from}
			result := model.CanTransitionTo(tt.to)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTicketModel_IsFinalState(t *testing.T) {
	assert.True(t, (&Ticket{Status: StatusClosed}).IsFinalState())
	assert.True(t, (&Ticket{Status: StatusCancelled}).IsFinalState())
	assert.False(t, (&Ticket{Status: StatusNew}).IsFinalState())
	assert.False(t, (&Ticket{Status: StatusOpen}).IsFinalState())
	assert.False(t, (&Ticket{Status: StatusResolved}).IsFinalState())
}

func TestTicketModel_StateError(t *testing.T) {
	err := &StateError{CurrentStatus: StatusNew, Message: "cannot resolve ticket from current status"}
	assert.Contains(t, err.Error(), "cannot resolve ticket")
}

// TestTicketStateMachine_SingleSourceOfTruth P1-1 不变式：
// repository 层的 CanTransitionTo 必须与 common.IsValidTicketStatusTransition
// 对所有已知状态对返回完全一致的结果。这锁定了"工单状态机为单一事实来源"这一要求，
// 防止将来有人在 model 层或 service 层内联一份自己的 map 导致入口间判定不一致。
func TestTicketStateMachine_SingleSourceOfTruth(t *testing.T) {
	statuses := []Status{
		StatusNew, StatusOpen, StatusInProgress, StatusPending,
		StatusResolved, StatusClosed, StatusCancelled,
		Status("assigned"), Status("approved"), Status("rejected"),
	}
	for _, from := range statuses {
		for _, to := range statuses {
			model := &Ticket{Status: from}
			gotModel := model.CanTransitionTo(to)
			gotCommon := common.IsValidTicketStatusTransition(string(from), string(to))
			assert.Equalf(t, gotCommon, gotModel,
				"CanTransitionTo diverged from common.IsValidTicketStatusTransition: %q -> %q (model=%v common=%v)",
				from, to, gotModel, gotCommon,
			)
		}
	}
}

// TestRepository_CreateWithTx_SkipsOccupiedSequenceNumber（D-6 回归）：
// 序列候选号已被占用（历史数据/其他租户）时，事务内创建必须**向前跳号**成功，
// 而不是把「必然碰撞」上抛失败；序列不可用时回退 DB 路径继续跳号。
func TestRepository_CreateWithTx_SkipsOccupiedSequenceNumber(t *testing.T) {
	fx := newRepoFixture(t)
	defer fx.client.Close()
	repo := fx.repo.(*EntRepository)

	now := time.Now()
	prefix := fmt.Sprintf("TKT-%04d%02d-", now.Year(), int(now.Month()))

	// 占用 000001（模拟序列落后：号已被历史数据使用）。
	repo.SetSequenceService(&stubSequenceProvider{values: []int64{1}})
	occupier, err := repo.Create(fx.ctx, &CreateParams{
		Title: "occupy", Description: "occupy", Priority: PriorityMedium,
		Type: TypeIncident, RequesterID: fx.user.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)
	require.Equal(t, prefix+"000001", occupier.TicketNumber)

	// 序列重放 1（且已耗尽）→ 探针发现占用 → 回退 DB 路径继续跳号 → 000002 成功。
	repo.SetSequenceService(&stubSequenceProvider{values: []int64{1}})
	tx, err := fx.client.Tx(fx.ctx)
	require.NoError(t, err)
	created, err := repo.CreateWithTx(fx.ctx, tx, &CreateParams{
		Title: "skip occupied", Description: "skip occupied", Priority: PriorityMedium,
		Type: TypeIncident, RequesterID: fx.user.ID,
	}, fx.tenant.ID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.Equal(t, prefix+"000002", created.TicketNumber)
}

// TestIsTicketNumberCollision_DetectsUniqueViolations 锁定哨兵判定：
// 事务内唯一键冲突必须被识别（Postgres 23505 / duplicate key 文案），
// 由仓库层上抛 ErrTicketNumberCollision、调用方以新事务重试（25P02 防护）。
func TestIsTicketNumberCollision_DetectsUniqueViolations(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"postgres duplicate key", fmt.Errorf("pq: duplicate key value violates unique constraint \"tickets_ticket_number_key\" (23505)"), true},
		{"postgres 23505 code", fmt.Errorf("driver: 23505 unique violation"), true},
		{"ent constraint error", &ent.ConstraintError{}, true},
		{"generic", fmt.Errorf("connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isTicketNumberCollision(tc.err))
		})
	}
}

// TestGenerateTicketNumber_SkipsGloballyOccupied （D-6 回归）：
// ticket_number 为全局唯一；序列候选号被他租户占用时必须向前跳号而非直接返回碰撞号。
// 使用独立内存库：默认 fixture 的共享 DSN 会让本用例占用的号段污染同包其他用例。
func TestGenerateTicketNumber_SkipsGloballyOccupied(t *testing.T) {
	ctx := context.Background()
	dsn := fmt.Sprintf("file:repo_skip_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano())
	client := enttest.Open(t, "sqlite3", dsn)
	defer client.Close()
	repo := NewEntRepository(client, zaptest.NewLogger(t).Sugar())

	tenant, err := client.Tenant.Create().
		SetName("Skip Tenant").SetCode("skip-a").SetDomain("skip-a.com").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	other, err := client.Tenant.Create().
		SetName("Other Tenant").SetCode("skip-b").SetDomain("skip-b.com").SetStatus("active").
		Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().
		SetUsername("skip-user").SetEmail("skip@test.com").SetName("Skip").
		SetPasswordHash("hash").SetRole("end_user").SetActive(true).SetTenantID(tenant.ID).
		Save(ctx)
	require.NoError(t, err)

	now := time.Now()
	prefix := fmt.Sprintf("TKT-%04d%02d-", now.Year(), int(now.Month()))
	// 模拟他租户/历史数据已占用 000001、000002。
	for _, n := range []string{prefix + "000001", prefix + "000002"} {
		_, err := client.Ticket.Create().
			SetTitle("occupied").SetDescription("occupied number").
			SetType(string(TypeIncident)).SetPriority(string(PriorityLow)).
			SetTicketNumber(n).SetRequesterID(user.ID).SetTenantID(other.ID).
			SetStatus(string(StatusNew)).
			Save(ctx)
		require.NoError(t, err)
	}

	repo.SetSequenceService(&stubSequenceProvider{values: []int64{1, 2, 3}})
	num, err := repo.GenerateTicketNumber(ctx, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, prefix+"000003", num, "占用号必须被跳过")
}
