package suggestion

import (
	"strings"

	"server-web/internal/copilot/nlu"
)

type Suggestion struct {
	Text   string            `json:"text"`
	Action string            `json:"action,omitempty"`
	Intent string            `json:"intent,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

func Build(result nlu.Result) []Suggestion {
	switch result.Intent {
	case nlu.IntentAlertQuery:
		return []Suggestion{
			New("查看当前活跃告警", nlu.IntentAlertQuery, map[string]string{"status": "firing"}),
			New("查看严重级别告警", nlu.IntentAlertQuery, map[string]string{"severity": "critical"}),
		}
	case nlu.IntentAlertEventQuery:
		return []Suggestion{
			New("查看最新告警事件", nlu.IntentAlertEventQuery, nil),
			New("查看最近已恢复告警", nlu.IntentAlertEventQuery, map[string]string{"status": "resolved"}),
		}
	case nlu.IntentAlertHistoryQuery:
		return []Suggestion{
			New("查看最近一周CPU告警历史", nlu.IntentAlertHistoryQuery, map[string]string{"window": "7d", "alert_name": "cpu"}),
			New("查看警告级别告警历史", nlu.IntentAlertHistoryQuery, map[string]string{"severity": "warning"}),
		}
	case nlu.IntentHostQuery:
		return []Suggestion{
			New("查看当前主机列表", nlu.IntentHostQuery, nil),
			New("查看离线主机", nlu.IntentHostQuery, map[string]string{"status": "down"}),
		}
	case nlu.IntentMetricQuery:
		return []Suggestion{
			New("查看 node-1 最近1小时CPU", nlu.IntentMetricQuery, map[string]string{"instance": "node-1", "window": "1h", "metric_type": "cpu"}),
			New("查看最近24小时内存趋势", nlu.IntentMetricQuery, map[string]string{"window": "24h", "metric_type": "memory"}),
		}
	case nlu.IntentDiagnosisRequest:
		return []Suggestion{
			New("显示最近 5 条告警历史", nlu.IntentAlertHistoryQuery, map[string]string{"count": "5"}),
			New("显示当前 firing 告警", nlu.IntentAlertQuery, map[string]string{"status": "firing"}),
		}
	case nlu.IntentGeneralChat:
		return defaultSuggestions()
	default:
		return defaultSuggestions()
	}
}

func New(text, intent string, params map[string]string) Suggestion {
	text = strings.TrimSpace(text)
	cleanParams := make(map[string]string, len(params))
	for key, value := range params {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			cleanParams[key] = value
		}
	}
	if len(cleanParams) == 0 {
		cleanParams = nil
	}
	return Suggestion{
		Text:   text,
		Action: text,
		Intent: strings.TrimSpace(intent),
		Params: cleanParams,
	}
}

func Normalize(values []Suggestion) []Suggestion {
	result := make([]Suggestion, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value.Text)
		if text == "" {
			continue
		}
		action := strings.TrimSpace(value.Action)
		if action == "" {
			action = text
		}
		params := make(map[string]string, len(value.Params))
		for key, paramValue := range value.Params {
			key = strings.TrimSpace(key)
			paramValue = strings.TrimSpace(paramValue)
			if key != "" && paramValue != "" {
				params[key] = paramValue
			}
		}
		if len(params) == 0 {
			params = nil
		}
		result = append(result, Suggestion{
			Text:   text,
			Action: action,
			Intent: strings.TrimSpace(value.Intent),
			Params: params,
		})
	}
	return result
}

func defaultSuggestions() []Suggestion {
	return []Suggestion{
		New("当前有哪些活跃告警？", nlu.IntentAlertQuery, map[string]string{"status": "firing"}),
		New("哪些主机离线了？", nlu.IntentHostQuery, map[string]string{"status": "down"}),
		New("查看 node-1 的CPU趋势", nlu.IntentMetricQuery, map[string]string{"instance": "node-1", "metric_type": "cpu"}),
	}
}
