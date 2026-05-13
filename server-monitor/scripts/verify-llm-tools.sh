#!/usr/bin/env bash
set -euo pipefail

LLM_API_URL="${LLM_API_URL:-https://api.deepseek.com/v1/chat/completions}"
LLM_API_KEY="${LLM_API_KEY:?ERROR: LLM_API_KEY is required}"
LLM_MODEL="${LLM_MODEL:-deepseek-chat}"

tools_json='[
  {
    "type": "function",
    "function": {
      "name": "alert_list_active",
      "description": "List active alerts in the monitoring system",
      "parameters": {
        "type": "object",
        "properties": {
          "severity": {
            "type": "string",
            "enum": ["critical", "warning", "info"],
            "description": "Filter alerts by severity level"
          }
        },
        "required": []
      }
    }
  },
  {
    "type": "function",
    "function": {
      "name": "host_list",
      "description": "List hosts in the infrastructure",
      "parameters": {
        "type": "object",
        "properties": {
          "status": {
            "type": "string",
            "enum": ["running", "stopped", "all"],
            "description": "Filter hosts by status"
          }
        },
        "required": []
      }
    }
  }
]'

call_llm() {
    local user_message="$1"
    curl -sf --max-time 30 "$LLM_API_URL" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $LLM_API_KEY" \
        -d "$(jq -n \
            --arg model "$LLM_MODEL" \
            --arg user "$user_message" \
            --argjson tools "$tools_json" \
            '{
                model: $model,
                messages: [{role: "user", content: $user}],
                tools: $tools,
                tool_choice: "auto"
            }')"
}

passed=0
failed=0

assert_tool_call() {
    local response="$1"
    local expected_tool="$2"
    local test_name="$3"

    local tool_name
    tool_name=$(echo "$response" | jq -r '.choices[0].message.tool_calls[0].function.name // empty')

    if [ "$tool_name" = "$expected_tool" ]; then
        echo "  PASS: $test_name (selected: $tool_name)"
        ((passed++)) || true
    else
        echo "  FAIL: $test_name (expected: $expected_tool, got: ${tool_name:-none})"
        ((failed++)) || true
    fi
}

assert_no_tool_call() {
    local response="$1"
    local test_name="$2"

    local has_tool_calls
    has_tool_calls=$(echo "$response" | jq '.choices[0].message.tool_calls // empty')

    if [ -z "$has_tool_calls" ]; then
        echo "  PASS: $test_name (no tool_calls, content returned)"
        ((passed++)) || true
    else
        echo "  FAIL: $test_name (expected no tool_calls, but got some)"
        ((failed++)) || true
    fi
}

echo "=== Test 1: Tool selection - alert_list_active ==="
resp1=$(call_llm "查看当前告警")
assert_tool_call "$resp1" "alert_list_active" "查看当前告警 -> alert_list_active"

echo ""
echo "=== Test 2: Tool selection - host_list ==="
resp2=$(call_llm "查看主机列表")
assert_tool_call "$resp2" "host_list" "查看主机列表 -> host_list"

echo ""
echo "=== Test 3: No tool match ==="
resp3=$(call_llm "你好")
assert_no_tool_call "$resp3" "你好 -> no tool call"

echo ""
echo "=== Test 4: Format stability (5 runs) ==="
stability_passed=0
for i in $(seq 1 5); do
    resp=$(call_llm "查看严重告警")

    tool_name=$(echo "$resp" | jq -r '.choices[0].message.tool_calls[0].function.name // empty')
    if [ "$tool_name" != "alert_list_active" ]; then
        echo "  FAIL: run $i - expected alert_list_active, got: ${tool_name:-none}"
        ((failed++)) || true
        continue
    fi

    args_str=$(echo "$resp" | jq -r '.choices[0].message.tool_calls[0].function.arguments // empty')
    if [ -z "$args_str" ]; then
        echo "  FAIL: run $i - no arguments returned"
        ((failed++)) || true
        continue
    fi

    if ! echo "$args_str" | jq -e . > /dev/null 2>&1; then
        echo "  FAIL: run $i - arguments are not valid JSON"
        ((failed++)) || true
        continue
    fi

    echo "  PASS: run $i - alert_list_active selected, valid JSON arguments"
    ((stability_passed++)) || true
done

((passed += stability_passed)) || true
((failed += (5 - stability_passed))) || true

echo ""
echo "======================================"
echo "  Results: $passed passed, $failed failed"
echo "======================================"

if [ "$failed" -gt 0 ]; then
    exit 1
fi
