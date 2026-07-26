# HighErrorRate

## 适用告警
- HighErrorRate

## 关键词
- error_rate
- 5xx
- http_errors
- server_monitor_http_error_rate_percent

## 典型现象
HTTP 5xx 错误率在观测窗口内持续高于阈值，可能伴随请求延迟升高、上游服务不可用或数据库连接池耗尽。

## 关键指标
- server_monitor_http_error_rate_percent
- server_monitor_http_requests_total
- server_monitor_http_request_duration_seconds

## 排查步骤
1. 查看 5xx 错误率 15m 和 1h 趋势，确认是否持续高位而不是瞬时尖峰。
2. 检查关联上游服务健康状态和响应延迟，判断是否为级联故障。
3. 查看同实例 24h 告警历史，识别发布窗口、配置变更或流量突增。
4. 结合日志中的异常堆栈和错误分类，确认根因（超时、熔断、资源耗尽等）。

## 建议动作
- 低风险：继续观察、通知负责人、收集错误分布和上游状态。
- 中风险：回滚最近发布或切换流量到健康实例，必须人工确认。
- 高风险：修改配置、重启服务，禁止由 AI 自动执行。

## 风险说明
任何写操作必须进入审批和审计流程；Runbook 只提供知识建议，不执行动作。
