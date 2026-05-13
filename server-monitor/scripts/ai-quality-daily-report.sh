#!/usr/bin/env bash
set -euo pipefail

PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"
HOURS=24
OUTPUT=""

urlencode() {
    local data="$1"
    echo -n "$data" | jq -sRr @uri
}

query_prometheus() {
    local query="$1"
    local url="${PROMETHEUS_URL}/api/v1/query?query=$(urlencode "$query")"
    local response
    response=$(curl -sf --max-time 10 "$url" 2>/dev/null) || {
        echo "ERROR: Failed to query Prometheus: $query" >&2
        return 1
    }
    local status
    status=$(echo "$response" | jq -r '.status')
    if [ "$status" != "success" ]; then
        echo "ERROR: Prometheus query failed: $query" >&2
        return 1
    fi
    echo "$response" | jq -r '.data.result[0].value[1] // "N/A"'
}

query_prometheus_vector() {
    local query="$1"
    local url="${PROMETHEUS_URL}/api/v1/query?query=$(urlencode "$query")"
    local response
    response=$(curl -sf --max-time 10 "$url" 2>/dev/null) || {
        echo "ERROR: Failed to query Prometheus: $query" >&2
        return 1
    }
    local status
    status=$(echo "$response" | jq -r '.status')
    if [ "$status" != "success" ]; then
        echo "ERROR: Prometheus query failed: $query" >&2
        return 1
    fi
    echo "$response" | jq -r '.data.result[] | "\(.metric.alertname // .metric.alertname) \(.value[1])"'
}

format_percent() {
    local value="$1"
    if [ "$value" = "N/A" ] || [ "$value" = "null" ] || [ -z "$value" ]; then
        echo "N/A"
        return
    fi
    echo "$value" | awk '{if ($1 != $1 || $1 == "NaN" || $1 == "nan") print "N/A"; else printf "%.1f%%", $1 * 100}'
}

format_number() {
    local value="$1"
    if [ "$value" = "N/A" ] || [ "$value" = "null" ] || [ -z "$value" ]; then
        echo "N/A"
        return
    fi
    echo "$value" | awk '{if ($1 != $1 || $1 == "NaN" || $1 == "nan") print "N/A"; else printf "%'\''0.f", $1}'
}

format_latency() {
    local value="$1"
    if [ "$value" = "N/A" ] || [ "$value" = "null" ] || [ -z "$value" ] || [ "$value" = "+Inf" ] || [ "$value" = "-Inf" ]; then
        echo "N/A"
        return
    fi
    echo "$value" | awk '{if ($1 != $1 || $1 == "NaN" || $1 == "nan") print "N/A"; else printf "%.3gs", $1}'
}

safe_divide() {
    local numerator="$1"
    local denominator="$2"
    if [ "$denominator" = "N/A" ] || [ "$denominator" = "null" ] || [ -z "$denominator" ]; then
        echo "N/A"
        return
    fi
    local denom_val
    denom_val=$(echo "$denominator" | awk '{print $1}')
    if [ "$denom_val" = "0" ] || [ "$denom_val" = "0.0" ]; then
        echo "N/A"
        return
    fi
    echo "$numerator" | awk -v d="$denom_val" '{printf "%.6f", $1 / d}'
}

show_help() {
    cat <<'EOF'
Usage: ai-quality-daily-report.sh [OPTIONS]

AI Quality Daily Report - queries Prometheus for AI quality metrics.

Options:
  -u, --url URL       Prometheus URL (default: http://localhost:9090)
                      Can also be set via PROMETHEUS_URL environment variable
  -h, --hours HOURS   Time range in hours (default: 24)
  -o, --output FILE   Output file (default: stdout)
      --help          Show this help message

Dependencies: curl, jq
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        -u|--url)
            PROMETHEUS_URL="$2"
            shift 2
            ;;
        -h|--hours)
            HOURS="$2"
            shift 2
            ;;
        -o|--output)
            OUTPUT="$2"
            shift 2
            ;;
        --help)
            show_help
            exit 0
            ;;
        *)
            echo "ERROR: Unknown option: $1" >&2
            show_help >&2
            exit 1
            ;;
    esac
done

HAS_ERROR=0

nlu_total=$(query_prometheus "sum(increase(copilot_nlu_classify_total[${HOURS}h]))") || HAS_ERROR=1
nlu_rule_total=$(query_prometheus "sum(increase(copilot_nlu_classify_total{source=\"rule\"}[${HOURS}h]))") || HAS_ERROR=1
nlu_rule_low_total=$(query_prometheus "sum(increase(copilot_nlu_classify_total{source=\"rule-low\"}[${HOURS}h]))") || HAS_ERROR=1
nlu_p95=$(query_prometheus "histogram_quantile(0.95, sum by(le) (rate(copilot_nlu_classify_duration_seconds_bucket[${HOURS}h])))") || HAS_ERROR=1

rag_total=$(query_prometheus "sum(increase(copilot_rag_search_total[${HOURS}h]))") || HAS_ERROR=1
rag_hit_total=$(query_prometheus "sum(increase(copilot_rag_search_total{has_result=\"true\"}[${HOURS}h]))") || HAS_ERROR=1
rag_p95=$(query_prometheus "histogram_quantile(0.95, sum by(le) (rate(copilot_rag_search_duration_seconds_bucket[${HOURS}h])))") || HAS_ERROR=1

diag_total=$(query_prometheus "sum(increase(copilot_diagnosis_llm_total[${HOURS}h]))") || HAS_ERROR=1
diag_success_total=$(query_prometheus "sum(increase(copilot_diagnosis_llm_total{result=\"success\"}[${HOURS}h]))") || HAS_ERROR=1
diag_fallback_total=$(query_prometheus "sum(increase(copilot_diagnosis_llm_total{result=\"fallback\"}[${HOURS}h]))") || HAS_ERROR=1
diag_p95=$(query_prometheus "histogram_quantile(0.95, sum by(le) (rate(copilot_diagnosis_duration_seconds_bucket[${HOURS}h])))") || HAS_ERROR=1

llm_total=$(query_prometheus "sum(increase(copilot_llm_request_total[${HOURS}h]))") || HAS_ERROR=1
llm_error_total=$(query_prometheus "sum(increase(copilot_llm_request_total{result=\"error\"}[${HOURS}h]))") || HAS_ERROR=1
llm_p95=$(query_prometheus "histogram_quantile(0.95, sum by(le) (rate(copilot_llm_request_duration_seconds_bucket[${HOURS}h])))") || HAS_ERROR=1
llm_input_tokens=$(query_prometheus "sum(increase(copilot_llm_tokens_total{direction=\"input\"}[${HOURS}h]))") || HAS_ERROR=1
llm_output_tokens=$(query_prometheus "sum(increase(copilot_llm_tokens_total{direction=\"output\"}[${HOURS}h]))") || HAS_ERROR=1

alerts_raw=$(query_prometheus_vector "sum by(alertname) (sum_over_time(ALERTS{alertgroup=\"copilot-ai-quality\"}[24h]))") || HAS_ERROR=1

if [ "$HAS_ERROR" -ne 0 ]; then
    exit 1
fi

nlu_rule_rate=$(safe_divide "${nlu_rule_total}" "${nlu_total}")
nlu_low_rate=$(safe_divide "${nlu_rule_low_total}" "${nlu_total}")
rag_hit_rate=$(safe_divide "${rag_hit_total}" "${rag_total}")
rag_no_result_rate="N/A"
if [ "$rag_hit_rate" != "N/A" ]; then
    rag_no_result_rate=$(echo "$rag_hit_rate" | awk '{printf "%.6f", 1 - $1}')
fi
diag_success_rate=$(safe_divide "${diag_success_total}" "${diag_total}")
diag_fallback_rate=$(safe_divide "${diag_fallback_total}" "${diag_total}")
llm_error_rate=$(safe_divide "${llm_error_total}" "${llm_total}")

period_start=$(date -u -d "-${HOURS} hours" "+%Y-%m-%d" 2>/dev/null || date -u -v-${HOURS}H "+%Y-%m-%d")
period_end=$(date -u "+%Y-%m-%d")
generated=$(date -u "+%Y-%m-%d %H:%M:%S UTC")

declare -A alert_counts
alert_counts[CopilotLLMHighErrorRate]="0"
alert_counts[CopilotDiagnosisHighFallbackRate]="0"
alert_counts[CopilotRAGHighNoResultRate]="0"
alert_counts[CopilotNLULowConfidenceRate]="0"

if [ -n "$alerts_raw" ]; then
    while IFS=' ' read -r name count; do
        if [ -n "$name" ] && [ -n "$count" ]; then
            alert_counts["$name"]="$count"
        fi
    done <<< "$alerts_raw"
fi

report=$(cat <<EOF
=====================================
  AI Quality Daily Report
  Period: ${period_start} ~ ${period_end}
  Generated: ${generated}
=====================================

--- NLU Classification ---
  Total calls:           $(format_number "$nlu_total")
  Rule confident rate:    $(format_percent "$nlu_rule_rate")
  Low confidence rate:    $(format_percent "$nlu_low_rate")
  P95 latency:           $(format_latency "$nlu_p95")

--- RAG Search ---
  Total searches:        $(format_number "$rag_total")
  Hit rate:              $(format_percent "$rag_hit_rate")
  No-result rate:        $(format_percent "$rag_no_result_rate")
  P95 latency:           $(format_latency "$rag_p95")

--- Diagnosis ---
  Total diagnoses:       $(format_number "$diag_total")
  LLM success rate:      $(format_percent "$diag_success_rate")
  Fallback rate:         $(format_percent "$diag_fallback_rate")
  P95 latency:           $(format_latency "$diag_p95")

--- LLM Requests ---
  Total requests:        $(format_number "$llm_total")
  Error rate:            $(format_percent "$llm_error_rate")
  P95 latency:           $(format_latency "$llm_p95")
  Input tokens:          $(format_number "$llm_input_tokens")
  Output tokens:         $(format_number "$llm_output_tokens")

--- Alerts Fired (24h) ---
  CopilotLLMHighErrorRate:             ${alert_counts[CopilotLLMHighErrorRate]}
  CopilotDiagnosisHighFallbackRate:    ${alert_counts[CopilotDiagnosisHighFallbackRate]}
  CopilotRAGHighNoResultRate:          ${alert_counts[CopilotRAGHighNoResultRate]}
  CopilotNLULowConfidenceRate:         ${alert_counts[CopilotNLULowConfidenceRate]}

=====================================
EOF
)

if [ -n "$OUTPUT" ]; then
    echo "$report" > "$OUTPUT"
else
    echo "$report"
fi
