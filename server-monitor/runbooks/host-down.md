# HostDown

## 适用告警
- HostDown

## 关键词
- down
- offline
- server-probe
- prometheus
- up

## 典型现象
Prometheus `up{job="server-probe"}` 为 0，目标主机或 server-probe 探针不可达，可能导致主机指标和告警数据中断。

## 关键指标
- up
- server_monitor_cpu_usage_percent
- server_monitor_memory_usage_percent

## 排查步骤
1. 查看告警状态是否仍为 firing，并确认 startsAt 与最近 scrape 时间。
2. 检查 Prometheus targets 中 server-probe 目标是否 down。
3. 验证目标主机网络连通性、防火墙、端口监听和 server-probe 进程状态。
4. 对比同主机告警历史，排除短暂网络抖动或 Prometheus scrape 配置错误。

## 建议动作
- 低风险：记录 targets 状态、错误信息和最近 scrape 时间。
- 中风险：重启探针或切换流量前必须由人工确认。
- 高风险：修改网络策略、重启宿主机或批量变更探针配置必须走审批流程。

## 风险说明
HostDown 可能是真实故障，也可能是监控链路误报；诊断必须区分业务不可用和观测不可用。
