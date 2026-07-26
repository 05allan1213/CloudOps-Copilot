# HighCPU

## 适用告警
- HighCPU

## 关键词
- cpu
- load
- server_monitor_cpu_usage_percent
- server_monitor_load1

## 典型现象
CPU 使用率在 15m 窗口内持续高于阈值，可能伴随 load1 升高、请求延迟变大或批处理任务堆积。

## 关键指标
- server_monitor_cpu_usage_percent
- server_monitor_load1
- server_monitor_process_count

## 排查步骤
1. 查看 CPU 15m 和 1h 趋势，确认是否持续高位而不是瞬时尖峰。
2. 对比 load1 与进程数量，判断是否存在可运行队列同步升高。
3. 查看同实例 24h 告警历史，识别周期性任务、发布窗口或重复抖动。
4. 结合主机最近变更、流量增长和批处理任务，确认是否为预期负载。

## 建议动作
- 低风险：继续观察、通知负责人、收集进程和负载信息。
- 中风险：扩容关联工作负载或迁移流量，必须人工确认。
- 高风险：删除资源、修改 Secret、批量重启，禁止由 AI 自动执行。

## 风险说明
任何写操作必须进入审批和审计流程；Runbook 只提供知识建议，不执行动作。
