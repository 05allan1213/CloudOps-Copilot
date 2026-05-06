#!/bin/sh
set -eu

ES_URL="${ELASTICSEARCH_URL:-http://elasticsearch:9200}"
ILM_POLICY_NAME="${ILM_POLICY_NAME:-sm-logs-policy}"
INDEX_TEMPLATE_NAME="${INDEX_TEMPLATE_NAME:-sm-logs-template}"

echo "waiting for Elasticsearch at ${ES_URL}"
attempt=1
while [ "$attempt" -le 60 ]; do
  if curl -fsS "${ES_URL}/_cluster/health?local=true" >/dev/null; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 2
done

if [ "$attempt" -gt 60 ]; then
  echo "Elasticsearch is not ready after 120 seconds" >&2
  exit 1
fi

echo "installing ILM policy ${ILM_POLICY_NAME}"
curl -fsS \
  -X PUT "${ES_URL}/_ilm/policy/${ILM_POLICY_NAME}" \
  -H "Content-Type: application/json" \
  --data-binary "@/init/ilm-policy.json"

echo "installing index template ${INDEX_TEMPLATE_NAME}"
curl -fsS \
  -X PUT "${ES_URL}/_index_template/${INDEX_TEMPLATE_NAME}" \
  -H "Content-Type: application/json" \
  --data-binary "@/init/index-template.json"

echo "Elasticsearch log lifecycle initialization completed"
