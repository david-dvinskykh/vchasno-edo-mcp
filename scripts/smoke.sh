#!/usr/bin/env bash
# Smoke test against a running vchasno-edo-mcp over its REST bridge.
#
#   MCP_URL=http://localhost:8088 ./scripts/smoke.sh
#
# With a company that has no API tariff every data call answers access_denied;
# that is a valid result and the script says so rather than failing.
set -uo pipefail

MCP_URL="${MCP_URL:-http://localhost:8088}"
AUTH=()
[ -n "${MCP_TOKEN:-}" ] && AUTH=(-H "Authorization: Bearer ${MCP_TOKEN}")

call() {
  local name="$1" args="${2:-{\}}"
  printf '\n── %s ──\n' "$name"
  curl -sS -X POST "${MCP_URL}/tools/${name}" "${AUTH[@]}" \
    -H 'Content-Type: application/json' -d "$args" | head -c 1500
  printf '\n'
}

printf '── service ──\n'
curl -sS "${MCP_URL}/" | head -c 400; printf '\n'
curl -sS "${MCP_URL}/ready" | head -c 400; printf '\n'

call self_check '{"deep":true}'
call get_billing
call list_document_categories
call list_roles
call list_tags
call list_groups
call list_fields
call list_documents '{"limit":5,"with":["recipients"]}'
call list_incoming_documents '{"limit":5,"processed":"false"}'
call list_scenarios
call list_document_templates
call list_archive_folders
call list_delete_requests
call check_counterparty '{"edrpou":"12345678"}'

printf '\n── resources ──\n'
curl -sS "${MCP_URL}/resources" "${AUTH[@]}" | head -c 600; printf '\n'
curl -sS "${MCP_URL}/resources/read?uri=vchasno%%3A%%2F%%2Fguide%%2Fstatuses" "${AUTH[@]}" | head -c 400; printf '\n'

printf '\n── prompts ──\n'
curl -sS "${MCP_URL}/prompts" "${AUTH[@]}" | head -c 600; printf '\n'
