package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent"
	"itsm-backend/ent/mspcontract"
	"itsm-backend/ent/mspcontractslabinding"
	"itsm-backend/ent/sladefinition"
	"itsm-backend/ent/sladefinitiongrant"
	"itsm-backend/ent/sladefinitionsnapshot"

	"go.uber.org/zap"
)

const (
	SLASnapshotKindContractBinding = "contract_binding"
	SLASnapshotKindGrant           = "grant"
)

// SyncContractSnapshot materializes the current contract bindings into the
// customer tenant. Old active snapshots for the contract are marked
// superseded first; the new rows become the only L1/provided read source.
func SyncContractSnapshot(ctx context.Context, client *ent.Client, contractID int) error {
	contract, err := client.MSPContract.Query().
		Where(mspcontract.IDEQ(contractID)).
		WithSLABindings(func(q *ent.MSPContractSLABindingQuery) {
			q.WithSLADefinition()
		}).
		Only(ctx)
	if err != nil {
		return err
	}
	customerID := contract.CustomerTenantID
	providerID := contract.ProviderTenantID
	sysCtx := tenantctx.SystemContext(ctx, "sla_snapshot:contract",
		fmt.Sprintf("sync contract %d to customer %d", contract.ID, customerID))

	tx, err := client.Tx(sysCtx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.SLADefinitionSnapshot.Update().
		Where(
			sladefinitionsnapshot.TenantIDEQ(customerID),
			sladefinitionsnapshot.SourceKindEQ(SLASnapshotKindContractBinding),
			sladefinitionsnapshot.SourceContractIDEQ(contract.ID),
			sladefinitionsnapshot.StatusEQ("active"),
		).
		SetStatus("superseded").
		Save(sysCtx); err != nil {
		return err
	}

	if contract.Status == "active" {
		version := time.Now().UnixNano()
		for _, binding := range contract.Edges.SLABindings {
			def := binding.Edges.SLADefinition
			if def == nil || !def.IsActive {
				continue
			}
			ticketTypeID := optionalTicketTypeID(binding.TicketTypeID)
			terms := snapshotTerms(def)
			payloadHash := snapshotPayloadHash(map[string]interface{}{
				"source_kind":          SLASnapshotKindContractBinding,
				"source_contract_id":   contract.ID,
				"source_binding_id":    binding.ID,
				"source_definition_id": def.ID,
				"source_tenant_id":     providerID,
				"source_version":       version,
				"definition_name":      def.Name,
				"ticket_type_id":       ticketTypeID,
				"terms":                terms,
				"effective_from":       contract.EffectiveFrom,
				"effective_until":      contract.EffectiveUntil,
				"status":               "active",
			})
			if _, err := tx.SLADefinitionSnapshot.Create().
				SetTenantID(customerID).
				SetSourceKind(SLASnapshotKindContractBinding).
				SetSourceContractID(contract.ID).
				SetSourceContractNo(contract.ContractNo).
				SetSourceBindingID(binding.ID).
				SetSourceDefinitionID(def.ID).
				SetSourceTenantID(providerID).
				SetSourceVersion(version).
				SetDefinitionName(def.Name).
				SetDescription(def.Description).
				SetServiceType(def.ServiceType).
				SetPriority(def.Priority).
				SetNillableTicketTypeID(ticketTypeID).
				SetTerms(terms).
				SetEffectiveFrom(contract.EffectiveFrom).
				SetNillableEffectiveUntil(contract.EffectiveUntil).
				SetStatus("active").
				SetPayloadHash(payloadHash).
				Save(sysCtx); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// SyncGrantSnapshot materializes one grant (and its source definition) into the
// customer tenant. Deactivated grants revoke prior active snapshots.
func SyncGrantSnapshot(ctx context.Context, client *ent.Client, grantID int) error {
	grant, err := client.SLADefinitionGrant.Query().
		Where(sladefinitiongrant.IDEQ(grantID)).
		Only(ctx)
	if err != nil {
		return err
	}
	customerID := grant.CustomerTenantID
	providerID := grant.ProviderTenantID
	sysCtx := tenantctx.SystemContext(ctx, "sla_snapshot:grant",
		fmt.Sprintf("sync grant %d to customer %d", grant.ID, customerID))

	tx, err := client.Tx(sysCtx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.SLADefinitionSnapshot.Update().
		Where(
			sladefinitionsnapshot.TenantIDEQ(customerID),
			sladefinitionsnapshot.SourceKindEQ(SLASnapshotKindGrant),
			sladefinitionsnapshot.SourceGrantIDEQ(grant.ID),
			sladefinitionsnapshot.StatusEQ("active"),
		).
		SetStatus("superseded").
		Save(sysCtx); err != nil {
		return err
	}

	if grant.IsActive {
		def, err := client.SLADefinition.Query().
			Where(sladefinition.IDEQ(grant.SLADefinitionID), sladefinition.TenantIDEQ(providerID)).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return tx.Commit()
			}
			return err
		}
		if def.IsActive {
			version := time.Now().UnixNano()
			ticketTypeID := optionalTicketTypeID(grant.TicketTypeID)
			terms := snapshotTerms(def)
			payloadHash := snapshotPayloadHash(map[string]interface{}{
				"source_kind":          SLASnapshotKindGrant,
				"source_grant_id":      grant.ID,
				"source_definition_id": def.ID,
				"source_tenant_id":     providerID,
				"source_version":       version,
				"definition_name":      def.Name,
				"ticket_type_id":       ticketTypeID,
				"terms":                terms,
				"effective_from":       grant.CreatedAt,
				"effective_until":      grant.ValidUntil,
				"status":               "active",
			})
			if _, err := tx.SLADefinitionSnapshot.Create().
				SetTenantID(customerID).
				SetSourceKind(SLASnapshotKindGrant).
				SetSourceGrantID(grant.ID).
				SetSourceDefinitionID(def.ID).
				SetSourceTenantID(providerID).
				SetSourceVersion(version).
				SetDefinitionName(def.Name).
				SetDescription(def.Description).
				SetServiceType(def.ServiceType).
				SetPriority(def.Priority).
				SetNillableTicketTypeID(ticketTypeID).
				SetTerms(terms).
				SetEffectiveFrom(grant.CreatedAt).
				SetNillableEffectiveUntil(grant.ValidUntil).
				SetStatus("active").
				SetPayloadHash(payloadHash).
				Save(sysCtx); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// ResyncDefinitionSnapshots refreshes every grant/contract binding that points
// at a provider definition. Called after definition publish/update.
func ResyncDefinitionSnapshots(ctx context.Context, client *ent.Client, definitionID, providerTenantID int) error {
	grants, err := client.SLADefinitionGrant.Query().
		Where(
			sladefinitiongrant.SLADefinitionIDEQ(definitionID),
			sladefinitiongrant.TenantIDEQ(providerTenantID),
		).
		All(ctx)
	if err != nil {
		return err
	}
	for _, grant := range grants {
		if err := SyncGrantSnapshot(ctx, client, grant.ID); err != nil {
			return err
		}
	}

	bindings, err := client.MSPContractSLABinding.Query().
		Where(
			mspcontractslabinding.SLADefinitionIDEQ(definitionID),
			mspcontractslabinding.TenantIDEQ(providerTenantID),
		).
		All(ctx)
	if err != nil {
		return err
	}
	seen := make(map[int]struct{}, len(bindings))
	for _, binding := range bindings {
		if _, ok := seen[binding.ContractID]; ok {
			continue
		}
		seen[binding.ContractID] = struct{}{}
		if err := SyncContractSnapshot(ctx, client, binding.ContractID); err != nil {
			return err
		}
	}
	return nil
}

// RevokeDefinitionSnapshots removes every active customer copy of a deleted
// definition. The source definition is gone, so no replacement version exists.
func RevokeDefinitionSnapshots(ctx context.Context, client *ent.Client, definitionID, providerTenantID int) error {
	sysCtx := tenantctx.SystemContext(ctx, "sla_snapshot:revoke_definition",
		fmt.Sprintf("revoke snapshots for deleted definition %d", definitionID))
	_, err := client.SLADefinitionSnapshot.Update().
		Where(
			sladefinitionsnapshot.SourceDefinitionIDEQ(definitionID),
			sladefinitionsnapshot.SourceTenantIDEQ(providerTenantID),
			sladefinitionsnapshot.StatusEQ("active"),
		).
		SetStatus("revoked").
		Save(sysCtx)
	return err
}

// BackfillAllSLASnapshots is used by cmd/backfill_sla_snapshots and tests.
func BackfillAllSLASnapshots(ctx context.Context, client *ent.Client) error {
	contracts, err := client.MSPContract.Query().All(ctx)
	if err != nil {
		return err
	}
	for _, contract := range contracts {
		if err := SyncContractSnapshot(ctx, client, contract.ID); err != nil {
			return err
		}
	}
	grants, err := client.SLADefinitionGrant.Query().All(ctx)
	if err != nil {
		return err
	}
	for _, grant := range grants {
		if err := SyncGrantSnapshot(ctx, client, grant.ID); err != nil {
			return err
		}
	}
	return nil
}

func snapshotTerms(def *ent.SLADefinition) map[string]interface{} {
	return map[string]interface{}{
		"response_time":    def.ResponseTime,
		"resolution_time":  def.ResolutionTime,
		"business_hours":   def.BusinessHours,
		"escalation_rules": def.EscalationRules,
		"conditions":       def.Conditions,
		"priority":         def.Priority,
		"service_type":     def.ServiceType,
	}
}

func optionalTicketTypeID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	v := id
	return &v
}

func snapshotPayloadHash(payload map[string]interface{}) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		zap.L().Warn("sla snapshot payload marshal failed", zap.Error(err))
		raw = []byte(fmt.Sprintf("marshal-error:%v", err))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// DefinitionFromSnapshot builds a read-only in-memory definition for the
// existing deadline calculator. It never loads provider sla_definitions.
func DefinitionFromSnapshot(row *ent.SLADefinitionSnapshot) *ent.SLADefinition {
	if row == nil {
		return nil
	}
	terms := row.Terms
	response := snapshotTermInt(terms["response_time"])
	resolution := snapshotTermInt(terms["resolution_time"])
	if response <= 0 && resolution <= 0 {
		return nil
	}
	businessHours, _ := terms["business_hours"].(map[string]interface{})
	escalationRules, _ := terms["escalation_rules"].(map[string]interface{})
	conditions, _ := terms["conditions"].(map[string]interface{})
	return &ent.SLADefinition{
		ID:                    row.SourceDefinitionID,
		Name:                  row.DefinitionName,
		Description:           row.Description,
		ServiceType:           row.ServiceType,
		Priority:              row.Priority,
		ResponseTime:          response,
		ResolutionTime:        resolution,
		BusinessHours:         businessHours,
		EscalationRules:       escalationRules,
		Conditions:            conditions,
		IsActive:              true,
		TenantID:              row.SourceTenantID,
		OwnerProviderTenantID: row.SourceTenantID,
	}
}

func snapshotTermInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}
