#!/usr/bin/env bash
# 238 demo 验收探针 P7：场景数据的 DB 层断言（只读）
set -u
DB=itsm_demo
PSQL="docker exec itsm-postgres-prod psql -U itsm -d $DB"

echo "== ACC-INCIDENTS(json) =="
$PSQL -tAc "SELECT to_jsonb(i) FROM incidents i WHERE i.title LIKE '[ACC]%' ORDER BY i.id LIMIT 12" | sed -E 's/"description":"[^"]*"/"description":"..."/g'

echo "== COUNTS =="
$PSQL -tAc "SELECT 'tickets='||count(*) FROM tickets"
$PSQL -tAc "SELECT 'incidents='||count(*) FROM incidents"

echo "== DISTRIBUTIONS =="
$PSQL -c "SELECT id, tenant_id, provider_tenant_id, source_type_id, copy_type_id, sync_status FROM ticket_type_distributions ORDER BY id LIMIT 10"

echo "== COPY/SOURCE TYPES =="
$PSQL -c "SELECT id, tenant_id, code, origin, origin_provider_tenant_id, origin_type_id, controlled_by_provider, sync_status FROM ticket_types WHERE id IN (25,26) ORDER BY id"

echo "== CONTRACTS =="
$PSQL -c "SELECT id, contract_no, customer_tenant_id, status, effective_from, effective_until FROM msp_contracts ORDER BY id"

echo "== DEFS-OWNER =="
$PSQL -c "SELECT id, tenant_id, owner_provider_tenant_id, is_active FROM sla_definitions WHERE id IN (9,10) ORDER BY id"

echo "== MFG-TYPES =="
$PSQL -c "SELECT id, code, origin, controlled_by_provider, sync_status, updated_at FROM ticket_types WHERE tenant_id=3 ORDER BY id"

echo "== TICKETS-RECENT =="
$PSQL -c "SELECT id, tenant_id, title, type, ticket_type_id FROM tickets ORDER BY id DESC LIMIT 8"

echo "== PROBE7-DONE =="
