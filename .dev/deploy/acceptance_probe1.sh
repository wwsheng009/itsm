#!/usr/bin/env bash
# 238 demo 验收探针 P1：身份/租户/环境侦察（只读，无副作用）
set -u
DB=itsm_demo
PSQL="docker exec itsm-postgres-prod psql -U itsm -d $DB"

echo "== TENANTS-COLUMNS =="
$PSQL -tAc "SELECT column_name FROM information_schema.columns WHERE table_name='tenants' ORDER BY ordinal_position" | tr '\n' ' '; echo
echo "== USERS-COLUMNS =="
$PSQL -tAc "SELECT column_name FROM information_schema.columns WHERE table_name='users' ORDER BY ordinal_position" | tr '\n' ' '; echo
echo "== TENANTS-ROWS(json) =="
$PSQL -tAc "SELECT row_to_json(t) FROM (SELECT * FROM tenants ORDER BY id LIMIT 30) t"
echo "== USERS-ROWS(json,core) =="
$PSQL -tAc "SELECT jsonb_build_object('id',id,'username',username,'tenant_id',tenant_id,'active',active) FROM users ORDER BY id LIMIT 60"
echo "== SLA-COUNTS =="
$PSQL -tAc "SELECT 'sla_defs='||count(*) FROM sla_definitions"
$PSQL -tAc "SELECT 'grants='||count(*) FROM sla_definition_grants"
$PSQL -tAc "SELECT 'contracts='||count(*) FROM msp_contracts"
$PSQL -tAc "SELECT 'contract_bindings='||count(*) FROM msp_contract_sla_bindings"
$PSQL -tAc "SELECT 'ticket_types='||count(*) FROM ticket_types"
$PSQL -tAc "SELECT 'distributions='||count(*) FROM ticket_type_distributions"
$PSQL -tAc "SELECT 'tickets='||count(*) FROM tickets"
echo "== GRANTS-ROWS =="
$PSQL -tAc "SELECT json_build_object('id',id,'sla_definition_id',sla_definition_id,'customer_tenant_id',customer_tenant_id,'provider_tenant_id',provider_tenant_id,'is_active',is_active) FROM sla_definition_grants ORDER BY id LIMIT 20"
echo "== ENV-KEYS(masked) =="
grep -iE 'password|user|admin|demo|msp|mfg' /home/n9e/itsm/repo/.env.demo | sed -E 's/=.*/=<set>/' | head -50
echo "== PROBE1-DONE =="
