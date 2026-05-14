#!/usr/bin/env bash
set -euo pipefail

MYSQL_HOST="${MYSQL_HOST:-localhost}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_DB="${MYSQL_DB:-server_monitor}"
MYSQL_USER="${MYSQL_USER:-root}"
MYSQL_PASSWORD_FILE="${MYSQL_PASSWORD_FILE:-}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-}"
LIMIT="${LIMIT:-50}"
OUTPUT_DIR="${OUTPUT_DIR:-./export}"
DAYS="${DAYS:-30}"

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Export diagnosis feedback cases for NLU/RAG/Prompt evaluation.

Options:
  --mysql-host HOST      MySQL host (default: localhost)
  --mysql-port PORT      MySQL port (default: 3306)
  --mysql-db DB          MySQL database (default: server_monitor)
  --mysql-user USER      MySQL user (default: root)
  --mysql-password FILE  MySQL password file (default: stdin)
  --limit N              Max cases per type (default: 50)
  --output-dir DIR       Output directory (default: ./export)
  --days N               Look back days (default: 30)
  --help                 Show this help message

Environment variables:
  MYSQL_HOST             MySQL host
  MYSQL_PORT             MySQL port
  MYSQL_DB               MySQL database
  MYSQL_USER             MySQL user
  MYSQL_PASSWORD         MySQL password (insecure, prefer --mysql-password)
  MYSQL_PASSWORD_FILE    MySQL password file
  LIMIT                  Max cases per type
  OUTPUT_DIR             Output directory
  DAYS                   Look back days
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --mysql-host)    MYSQL_HOST="$2"; shift 2 ;;
        --mysql-port)    MYSQL_PORT="$2"; shift 2 ;;
        --mysql-db)      MYSQL_DB="$2"; shift 2 ;;
        --mysql-user)    MYSQL_USER="$2"; shift 2 ;;
        --mysql-password) MYSQL_PASSWORD_FILE="$2"; shift 2 ;;
        --limit)         LIMIT="$2"; shift 2 ;;
        --output-dir)    OUTPUT_DIR="$2"; shift 2 ;;
        --days)          DAYS="$2"; shift 2 ;;
        --help)          usage; exit 0 ;;
        *)               echo "Unknown option: $1" >&2; usage >&2; exit 1 ;;
    esac
done

if ! command -v mysql &>/dev/null; then
    echo "Error: mysql client not found" >&2
    exit 1
fi

if ! command -v jq &>/dev/null; then
    echo "Error: jq not found" >&2
    exit 1
fi

mkdir -p "$OUTPUT_DIR"

MYSQL_ARGS=("-h" "$MYSQL_HOST" "-P" "$MYSQL_PORT" "-u" "$MYSQL_USER" "--skip-column-names" "--batch")

if [[ -n "$MYSQL_PASSWORD_FILE" && -f "$MYSQL_PASSWORD_FILE" ]]; then
    MYSQL_ARGS+=("--password=$(cat "$MYSQL_PASSWORD_FILE")")
elif [[ -n "$MYSQL_PASSWORD" ]]; then
    MYSQL_ARGS+=("--password=$MYSQL_PASSWORD")
fi

run_query() {
    local sql="$1"
    mysql "${MYSQL_ARGS[@]}" "$MYSQL_DB" -e "$sql" 2>/dev/null
}

EXPORTED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "Exporting NLU evaluation candidates..."
NLU_QUERY="
SELECT JSON_OBJECT(
    'source', 'feedback_not_useful',
    'diagnosis_id', f.diagnosis_id,
    'alert_name', d.alert_name,
    'target_name', d.target_name,
    'confidence', d.confidence,
    'feedback_rating', f.rating,
    'feedback_comment', f.comment,
    'suggested_intent', 'metric_query',
    'suggested_input', CONCAT('查看 ', d.target_name, ' 的 CPU 使用率')
) AS json_row
FROM diagnosis_feedback f
JOIN diagnosis_reports d ON d.id = f.diagnosis_id
WHERE f.rating = 'not_useful'
  AND f.created_at >= DATE_SUB(NOW(), INTERVAL $DAYS DAY)
ORDER BY f.created_at DESC
LIMIT $LIMIT;
"

NLU_CASES=$(run_query "$NLU_QUERY" 2>/dev/null | grep '^{' || true)

NLU_JSON=$(jq -n \
    --arg exported_at "$EXPORTED_AT" \
    --argjson days "$DAYS" \
    --argjson cases "[${NLU_CASES//$'\n'/,}]" \
    '{exported_at: $exported_at, look_back_days: $days, total_cases: ($cases | length), cases: $cases}')

echo "$NLU_JSON" > "$OUTPUT_DIR/nlu_eval_candidates.json"
echo "  -> $OUTPUT_DIR/nlu_eval_candidates.json ($(echo "$NLU_JSON" | jq '.total_cases') cases)"

echo "Exporting RAG evaluation candidates..."
RAG_QUERY="
SELECT JSON_OBJECT(
    'source', 'feedback_not_useful',
    'diagnosis_id', f.diagnosis_id,
    'alert_name', d.alert_name,
    'runbook_hit_count', JSON_LENGTH(d.runbooks_json),
    'feedback_rating', f.rating,
    'feedback_comment', f.comment,
    'suggested_query', d.alert_name,
    'suggested_want_file', ''
) AS json_row
FROM diagnosis_feedback f
JOIN diagnosis_reports d ON d.id = f.diagnosis_id
WHERE f.rating = 'not_useful'
  AND f.created_at >= DATE_SUB(NOW(), INTERVAL $DAYS DAY)
ORDER BY f.created_at DESC
LIMIT $LIMIT;
"

RAG_CASES=$(run_query "$RAG_QUERY" 2>/dev/null | grep '^{' || true)

RAG_JSON=$(jq -n \
    --arg exported_at "$EXPORTED_AT" \
    --argjson days "$DAYS" \
    --argjson cases "[${RAG_CASES//$'\n'/,}]" \
    '{exported_at: $exported_at, look_back_days: $days, total_cases: ($cases | length), cases: $cases}')

echo "$RAG_JSON" > "$OUTPUT_DIR/rag_eval_candidates.json"
echo "  -> $OUTPUT_DIR/rag_eval_candidates.json ($(echo "$RAG_JSON" | jq '.total_cases') cases)"

echo "Exporting Prompt evaluation candidates..."
PROMPT_QUERY="
SELECT JSON_OBJECT(
    'source', CASE WHEN d.llm_model = 'rule-only' THEN 'diagnosis_fallback' ELSE 'feedback_not_useful' END,
    'diagnosis_id', d.id,
    'alert_name', d.alert_name,
    'confidence', d.confidence,
    'llm_result', CASE WHEN d.llm_model = 'rule-only' THEN 'fallback' ELSE 'llm' END,
    'feedback_rating', COALESCE(f.rating, ''),
    'feedback_comment', COALESCE(f.comment, ''),
    'summary_excerpt', LEFT(d.summary, 200)
) AS json_row
FROM diagnosis_reports d
LEFT JOIN diagnosis_feedback f ON f.diagnosis_id = d.id AND f.rating = 'not_useful'
WHERE (d.llm_model = 'rule-only' OR f.rating = 'not_useful')
  AND d.status = 'completed'
  AND d.created_at >= DATE_SUB(NOW(), INTERVAL $DAYS DAY)
ORDER BY d.confidence ASC
LIMIT $LIMIT;
"

PROMPT_CASES=$(run_query "$PROMPT_QUERY" 2>/dev/null | grep '^{' || true)

PROMPT_JSON=$(jq -n \
    --arg exported_at "$EXPORTED_AT" \
    --argjson days "$DAYS" \
    --argjson cases "[${PROMPT_CASES//$'\n'/,}]" \
    '{exported_at: $exported_at, look_back_days: $days, total_cases: ($cases | length), cases: $cases}')

echo "$PROMPT_JSON" > "$OUTPUT_DIR/prompt_eval_candidates.json"
echo "  -> $OUTPUT_DIR/prompt_eval_candidates.json ($(echo "$PROMPT_JSON" | jq '.total_cases') cases)"

echo "Export complete."
