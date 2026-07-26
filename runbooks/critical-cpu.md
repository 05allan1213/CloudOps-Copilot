# CriticalCPU

## 适用告警
- CriticalCPU

## 关键词
- cpu
- critical
- load
- server_monitor_cpu_usage_percent

## 典型现象
CPU 使用率长时间接近饱和，可能影响核心服务响应，并伴随 load1 明显升高或进程数异常增长。

## 关键指标
- server_monitor_cpu_usage_percent
- server_monitor_load1
- server_monitor_process_count

## 排查步骤
1. 优先确认告警仍处于 firing，排除已恢复告警导致的重复处理。
2. 查看 CPU、load1、进程数量在 15m 窗口内是否同时异常。
3. 检查同实例最近是否有 HighCPU 或 CriticalCPU 连续触发。
4. 联系业务负责人确认近期发布、压测、定时任务或异常流量。

## 建议动作
- 低风险：立即通知负责人并保留指标、告警历史和进程快照。
- 中风险：执行限流、摘流、扩容或迁移前必须由人工确认。
- 高风险：强制重启、删除容器、修改配置和批量变更必须走审批流程。

## 风险说明
CriticalCPU 表示影响风险更高，但 Runbook 仍只是参考知识；当前 Runbook 能力不自动执行任何写操作。
