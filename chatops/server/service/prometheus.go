package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

var prometheusURL string

func InitPrometheus() {
	prometheusURL = os.Getenv("PROMETHEUS_URL")
	if prometheusURL == "" {
		prometheusURL = "http://prometheus:9090"
	}
}

// 即时查询响应
type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// 范围查询响应
type promRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// QueryPrometheus 查询最近 10 条数据（范围查询，步长 5 秒）
func QueryPrometheus(queryType string) (string, error) {
	var promQL string
	switch queryType {
	case "cpu":
		promQL = `probe_cpu_usage_percent`
	case "memory":
		promQL = `probe_mem_usage_percent`
	default:
		promQL = queryType
	}

	now := time.Now().Unix()
	start := now - 50 // 最近 50 秒，步长 5 秒 = 约 10 条数据

	apiURL := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=5",
		prometheusURL, url.QueryEscape(promQL), start, now)

	resp, err := http.Get(apiURL)
	if err != nil {
		return "Prometheus 暂不可用（未部署或地址不可达），部署到 K8s 后可复用项目一的 Prometheus", nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result promRangeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 Prometheus 响应失败: %v", err)
	}

	if result.Status != "success" {
		return "Prometheus 查询失败", nil
	}

	if len(result.Data.Result) == 0 {
		return "暂无监控数据", nil
	}

	var label string
	switch queryType {
	case "cpu":
		label = "CPU 使用率"
	case "memory":
		label = "内存使用率"
	default:
		label = "查询结果"
	}

	var output string
	for _, series := range result.Data.Result {
		instance := series.Metric["instance"]
		output += fmt.Sprintf("%s（%s）最近 %d 条数据：\n", label, instance, len(series.Values))
		for i, v := range series.Values {
			ts := "N/A"
			val := "N/A"
			if len(v) > 0 {
				if f, ok := v[0].(float64); ok {
					t := time.Unix(int64(f), 0)
					ts = t.Format("15:04:05")
				}
			}
			if len(v) > 1 {
				val = fmt.Sprintf("%v%%", v[1])
			}
			output += fmt.Sprintf("  %d. [%s] %s\n", i+1, ts, val)
		}
	}
	return output, nil
}
