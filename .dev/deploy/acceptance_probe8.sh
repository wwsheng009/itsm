#!/usr/bin/env bash
# 238 demo 验收探针 P8：tickets 层/绑定/分发/原生行最终断言（只读）
set -u
DB=itsm_demo
PSQL="docker exec itsm-postgres-prod psql -U itsm -d $DB"

echo "== TICKETS(>=37) =="
$PSQL -c "SELECT id, tenant_id, sla_definition_id, sla_matched_layer, (sla_response_deadline IS NOT NULL) AS rd FROM tickets WHERE id >= 37 ORDER BY id"
echo "== BINDINGS =="
$PSQL -c "SELECT id, contract_id, sla_definition_id, ticket_type_id FROM msp_contract_sla_bindings ORDER BY id"
echo "== DISTRIBUTIONS =="
$PSQL -c "SELECT id, sync_status, copy_type_id FROM ticket_type_distributions ORDER BY id"
echo "== MFG-NATIVE-UNTOUCHED =="
$PSQL -c "SELECT id, code, updated_at FROM ticket_types WHERE id IN (13,14,15,16) ORDER BY id"
echo "== PROVIDER-ACTIVE-DEFS =="
$PSQL -c "SELECT id, owner_provider_tenant_id FROM sla_definitions WHERE tenant_id=2 AND is_active ORDER BY id"
echo "== PROBE8-DONE =="
