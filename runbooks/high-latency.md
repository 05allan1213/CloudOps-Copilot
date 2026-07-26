# HighLatency

## 适用告警
- HighLatency

## 关键词
- latency
- delay
- p99
- response_time
- server_monitor_http_request_duration_seconds

## 典型现象
HTTP 请求延迟（P99/P95）在观测窗口内持续高于阈值，可能伴随 CPU 升高、数据库慢查询或下游服务响应变慢。

## 关键指标
- server_monitor_http_request_duration_seconds
- server_monitor_cpu_usage_percent
- server_monitor_memory_usage_percent

## 排查步骤
1. 查看延迟 P99/P95 的 15m 和 1h 趋势，确认是否持续高位而不是瞬时尖峰。
2. 检查关联实例 CPU、内存和磁盘 IO，判断是否为资源瓶颈。
3. 查看数据库慢查询日志和连接池使用率，排除数据库侧延迟。
4. 检查下游依赖服务延迟和错误率，确认是否为级联影响。
5. 查看同实例 24h 告警历史，识别发布窗口或流量模式变化。

## 建议动作
- 低风险：继续观察、通知负责人、收集延迟分布和依赖状态。
- 中风险：扩容关联工作负载或降级非核心功能，必须人工确认。
- 高风险：重启服务、修改超时配置，禁止由 AI 自动执行。

## 风险说明
任何写操作必须进入审批和审计流程；Runbook 只提供知识建议，不执行动作。
