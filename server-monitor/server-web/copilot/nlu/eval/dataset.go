package eval

import (
	"fmt"

	"server-web/copilot/nlu"
)

type EvalCase struct {
	Input       string
	WantIntent  string
	Description string
}

var validIntents = map[string]bool{
	nlu.IntentAlertQuery:         true,
	nlu.IntentAlertEventQuery:    true,
	nlu.IntentAlertHistoryQuery:  true,
	nlu.IntentAlertRuleListQuery: true,
	nlu.IntentMetricQuery:        true,
	nlu.IntentHostQuery:          true,
	nlu.IntentDiagnosisRequest:   true,
	nlu.IntentGeneralChat:        true,
}

func (c EvalCase) Validate() error {
	if !validIntents[c.WantIntent] {
		return fmt.Errorf("invalid intent %q", c.WantIntent)
	}
	return nil
}

func GoldenSet() []EvalCase {
	return []EvalCase{
		{
			Input:       "当前有哪些告警",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "中文基本告警查询",
		},
		{
			Input:       "Show firing alerts",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "英文firing状态告警",
		},
		{
			Input:       "告警列表",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "中文告警列表",
		},
		{
			Input:       "critical severity alerts",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "英文critical级别告警",
		},
		{
			Input:       "已恢复的告警",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "中文resolved状态告警",
		},
		{
			Input:       "显示所有告警",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "中文显示所有告警",
		},
		{
			Input:       "What alerts are currently firing?",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "英文firing状态问句",
		},
		{
			Input:       "warning级别的告警",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "中文warning级别告警",
		},
		{
			Input:       "告警状态 firing",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "中文带firing状态",
		},
		{
			Input:       "有多少条告警",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "中文告警数量查询",
		},
		{
			Input:       "最近告警",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "中文最近告警",
		},
		{
			Input:       "active alerts list",
			WantIntent:  nlu.IntentAlertQuery,
			Description: "英文活跃告警列表",
		},
		{
			Input:       "最新5条告警事件",
			WantIntent:  nlu.IntentAlertEventQuery,
			Description: "中文最新N条事件",
		},
		{
			Input:       "Show latest alert events",
			WantIntent:  nlu.IntentAlertEventQuery,
			Description: "英文最新告警事件",
		},
		{
			Input:       "告警事件流",
			WantIntent:  nlu.IntentAlertEventQuery,
			Description: "中文事件流",
		},
		{
			Input:       "最近的事件",
			WantIntent:  nlu.IntentAlertEventQuery,
			Description: "中文最近事件",
		},
		{
			Input:       "latest 10 events",
			WantIntent:  nlu.IntentAlertEventQuery,
			Description: "英文最新10条事件",
		},
		{
			Input:       "告警事件",
			WantIntent:  nlu.IntentAlertEventQuery,
			Description: "中文告警事件简写",
		},
		{
			Input:       "Show recent alert events",
			WantIntent:  nlu.IntentAlertEventQuery,
			Description: "英文最近告警事件",
		},
		{
			Input:       "最新告警事件",
			WantIntent:  nlu.IntentAlertEventQuery,
			Description: "中文最新告警事件",
		},
		{
			Input:       "CPU告警历史",
			WantIntent:  nlu.IntentAlertHistoryQuery,
			Description: "中文CPU告警历史",
		},
		{
			Input:       "Show alert history for the last week",
			WantIntent:  nlu.IntentAlertHistoryQuery,
			Description: "英文上周告警历史",
		},
		{
			Input:       "过去一周的告警历史",
			WantIntent:  nlu.IntentAlertHistoryQuery,
			Description: "中文过去一周告警历史",
		},
		{
			Input:       "alert history",
			WantIntent:  nlu.IntentAlertHistoryQuery,
			Description: "英文告警历史简写",
		},
		{
			Input:       "HighCPU告警历史记录",
			WantIntent:  nlu.IntentAlertHistoryQuery,
			Description: "中文带告警名的历史",
		},
		{
			Input:       "最近一周告警历史",
			WantIntent:  nlu.IntentAlertHistoryQuery,
			Description: "中文最近一周告警历史",
		},
		{
			Input:       "告警历史记录",
			WantIntent:  nlu.IntentAlertHistoryQuery,
			Description: "中文告警历史记录",
		},
		{
			Input:       "Show CPU alerts history",
			WantIntent:  nlu.IntentAlertHistoryQuery,
			Description: "英文CPU告警历史",
		},
		{
			Input:       "告警规则列表",
			WantIntent:  nlu.IntentAlertRuleListQuery,
			Description: "中文告警规则列表",
		},
		{
			Input:       "Show alert rules",
			WantIntent:  nlu.IntentAlertRuleListQuery,
			Description: "英文告警规则",
		},
		{
			Input:       "alert rule list",
			WantIntent:  nlu.IntentAlertRuleListQuery,
			Description: "英文告警规则列表",
		},
		{
			Input:       "显示告警规则",
			WantIntent:  nlu.IntentAlertRuleListQuery,
			Description: "中文显示告警规则",
		},
		{
			Input:       "有哪些告警规则",
			WantIntent:  nlu.IntentAlertRuleListQuery,
			Description: "中文询问告警规则",
		},
		{
			Input:       "List all alert rules",
			WantIntent:  nlu.IntentAlertRuleListQuery,
			Description: "英文列出所有规则",
		},
		{
			Input:       "CPU使用率",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文CPU使用率",
		},
		{
			Input:       "Show CPU trend for node-1",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "英文CPU趋势带instance",
		},
		{
			Input:       "内存使用率趋势",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文内存趋势",
		},
		{
			Input:       "磁盘使用率",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文磁盘使用率",
		},
		{
			Input:       "网络流量趋势",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文网络趋势",
		},
		{
			Input:       "负载情况",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文负载查询",
		},
		{
			Input:       "promql: up{job='server-probe'}",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "PromQL查询",
		},
		{
			Input:       "server_monitor_cpu_usage_percent for 1h",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "英文metric带window",
		},
		{
			Input:       "CPU指标",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文CPU指标",
		},
		{
			Input:       "内存占用",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文内存占用",
		},
		{
			Input:       "Show memory metrics",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "英文内存指标",
		},
		{
			Input:       "磁盘IO",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文磁盘IO",
		},
		{
			Input:       "网络延迟",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文网络延迟",
		},
		{
			Input:       "负载指标 15m",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文负载带window",
		},
		{
			Input:       "query_range cpu_usage 24h",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "query_range查询",
		},
		{
			Input:       "CPU趋势 6h",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文CPU趋势带window",
		},
		{
			Input:       "内存使用率 promql",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文带promql关键词",
		},
		{
			Input:       "服务器CPU负载",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "中文服务器CPU负载",
		},
		{
			Input:       "主机列表",
			WantIntent:  nlu.IntentHostQuery,
			Description: "中文主机列表",
		},
		{
			Input:       "Show offline hosts",
			WantIntent:  nlu.IntentHostQuery,
			Description: "英文离线主机",
		},
		{
			Input:       "离线的主机",
			WantIntent:  nlu.IntentHostQuery,
			Description: "中文离线主机",
		},
		{
			Input:       "在线主机",
			WantIntent:  nlu.IntentHostQuery,
			Description: "中文在线主机",
		},
		{
			Input:       "List all hosts",
			WantIntent:  nlu.IntentHostQuery,
			Description: "英文列出所有主机",
		},
		{
			Input:       "按CPU排序的主机",
			WantIntent:  nlu.IntentHostQuery,
			Description: "中文按CPU排序主机",
		},
		{
			Input:       "高CPU风险的主机",
			WantIntent:  nlu.IntentHostQuery,
			Description: "中文高CPU风险主机",
		},
		{
			Input:       "节点状态",
			WantIntent:  nlu.IntentHostQuery,
			Description: "中文节点状态",
		},
		{
			Input:       "诊断HighCPU告警",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "中文诊断带告警名",
		},
		{
			Input:       "Diagnose alert fingerprint abc123",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "英文诊断带fingerprint",
		},
		{
			Input:       "分析告警",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "中文分析告警",
		},
		{
			Input:       "诊断CPU告警 instance node-1",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "中文诊断带instance",
		},
		{
			Input:       "告警诊断",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "中文告警诊断",
		},
		{
			Input:       "Diagnose this alert",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "英文诊断告警",
		},
		{
			Input:       "帮我分析告警",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "中文帮我分析告警",
		},
		{
			Input:       "诊断告警 history_id 42",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "中文诊断带history_id",
		},
		{
			Input:       "分析这个告警",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "中文分析这个告警",
		},
		{
			Input:       "告警分析诊断",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "中文告警分析诊断",
		},
		{
			Input:       "你好",
			WantIntent:  nlu.IntentGeneralChat,
			Description: "中文问候",
		},
		{
			Input:       "What can you do?",
			WantIntent:  nlu.IntentGeneralChat,
			Description: "英文功能询问",
		},
		{
			Input:       "帮助",
			WantIntent:  nlu.IntentGeneralChat,
			Description: "中文帮助",
		},
		{
			Input:       "Help me",
			WantIntent:  nlu.IntentGeneralChat,
			Description: "英文帮助",
		},
		{
			Input:       "你能做什么",
			WantIntent:  nlu.IntentGeneralChat,
			Description: "中文功能询问",
		},
		{
			Input:       "解释一下你的功能",
			WantIntent:  nlu.IntentGeneralChat,
			Description: "中文解释功能",
		},
		{
			Input:       "Hello",
			WantIntent:  nlu.IntentGeneralChat,
			Description: "英文问候",
		},
		{
			Input:       "介绍一下你自己",
			WantIntent:  nlu.IntentGeneralChat,
			Description: "中文自我介绍",
		},
		{
			Input:       "查告警并诊断",
			WantIntent:  nlu.IntentDiagnosisRequest,
			Description: "多意图混合含诊断优先",
		},
		{
			Input:       "系统变慢了",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "歧义输入含metric关键词",
		},
		{
			Input:       "服务器卡住了",
			WantIntent:  nlu.IntentHostQuery,
			Description: "歧义输入含主机关键词",
		},
		{
			Input:       "帮我修改密码",
			WantIntent:  nlu.IntentGeneralChat,
			Description: "无匹配意图归入general",
		},
		{
			Input:       "CPU和内存都高",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "多metric关键词",
		},
		{
			Input:       "磁盘满了怎么办",
			WantIntent:  nlu.IntentMetricQuery,
			Description: "含磁盘关键词",
		},
		{
			Input:       "机器离线了",
			WantIntent:  nlu.IntentHostQuery,
			Description: "含离线关键词",
		},
		{
			Input:       "查看告警规则和历史",
			WantIntent:  nlu.IntentAlertRuleListQuery,
			Description: "多意图含规则优先",
		},
	}
}

type MultiEvalCase struct {
	Input       string
	WantIntents []string
	Description string
}

func MultiEvalSet() []MultiEvalCase {
	return []MultiEvalCase{
		{Input: "查告警并诊断", WantIntents: []string{"alert_query", "diagnosis_request"}, Description: "双意图-并"},
		{Input: "查看主机和指标", WantIntents: []string{"host_query", "metric_query"}, Description: "双意图-和"},
		{Input: "查告警然后看主机状态", WantIntents: []string{"alert_query", "host_query"}, Description: "双意图-然后"},
		{Input: "查看 node-1 的 CPU 和内存", WantIntents: []string{"metric_query", "metric_query"}, Description: "双意图-同意图不同实体"},
		{Input: "查看告警并看主机", WantIntents: []string{"alert_query", "host_query"}, Description: "双意图-并"},
		{Input: "查告警和诊断", WantIntents: []string{"alert_query", "diagnosis_request"}, Description: "双意图-和"},
		{Input: "查看主机同时查指标", WantIntents: []string{"host_query", "metric_query"}, Description: "双意图-同时"},
		{Input: "查告警然后诊断", WantIntents: []string{"alert_query", "diagnosis_request"}, Description: "双意图-然后"},
		{Input: "查看指标再看主机", WantIntents: []string{"metric_query", "host_query"}, Description: "双意图-再"},
		{Input: "查告警接着看主机", WantIntents: []string{"alert_query", "host_query"}, Description: "双意图-接着"},
		{Input: "查看告警以及主机状态", WantIntents: []string{"alert_query", "host_query"}, Description: "双意图-以及"},
		{Input: "查告警且看主机", WantIntents: []string{"alert_query", "host_query"}, Description: "双意图-且"},
		{Input: "check alerts and host status", WantIntents: []string{"alert_query", "host_query"}, Description: "双意图-英文and"},
		{Input: "show CPU then memory", WantIntents: []string{"metric_query", "metric_query"}, Description: "双意图-英文then"},
		{Input: "check alerts and diagnose", WantIntents: []string{"alert_query", "diagnosis_request"}, Description: "双意图-英文and"},
		{Input: "show hosts also metrics", WantIntents: []string{"host_query", "metric_query"}, Description: "双意图-英文also"},
		{Input: "查告警并看主机然后查指标", WantIntents: []string{"alert_query", "host_query", "metric_query"}, Description: "三意图"},
		{Input: "查看告警和主机以及指标", WantIntents: []string{"alert_query", "host_query", "metric_query"}, Description: "三意图"},
		{Input: "查告警然后看主机再查指标", WantIntents: []string{"alert_query", "host_query", "metric_query"}, Description: "三意图"},
		{Input: "查看告警并诊断然后看主机", WantIntents: []string{"alert_query", "diagnosis_request", "host_query"}, Description: "三意图"},
		{Input: "当前有哪些告警", WantIntents: []string{"alert_query"}, Description: "单意图-不退化"},
		{Input: "主机列表", WantIntents: []string{"host_query"}, Description: "单意图-不退化"},
		{Input: "CPU使用率", WantIntents: []string{"metric_query"}, Description: "单意图-不退化"},
		{Input: "诊断HighCPU告警", WantIntents: []string{"diagnosis_request"}, Description: "单意图-不退化"},
		{Input: "你好", WantIntents: []string{"general_chat"}, Description: "单意图-不退化"},
		{Input: "告警规则列表", WantIntents: []string{"alert_rule_list_query"}, Description: "单意图-不退化"},
		{Input: "CPU告警历史", WantIntents: []string{"alert_history_query"}, Description: "单意图-不退化"},
		{Input: "最新5条告警事件", WantIntents: []string{"alert_event_query"}, Description: "单意图-不退化"},
		{Input: "Show offline hosts", WantIntents: []string{"host_query"}, Description: "单意图-不退化"},
		{Input: "What can you do?", WantIntents: []string{"general_chat"}, Description: "单意图-不退化"},
		{Input: "和运维相关的告警", WantIntents: []string{"alert_query"}, Description: "边界-和非连接词"},
		{Input: "查看告警和", WantIntents: []string{"alert_query"}, Description: "边界-连接词在句末"},
		{Input: "和谐的主机", WantIntents: []string{"host_query"}, Description: "边界-和作为非连接词"},
		{Input: "查看告警同时", WantIntents: []string{"alert_query"}, Description: "边界-同时作为非连接词"},
		{Input: "查看告警然后", WantIntents: []string{"alert_query"}, Description: "边界-然后作为非连接词"},
		{Input: "查看告警再说", WantIntents: []string{"alert_query"}, Description: "边界-再说"},
	}
}
