# HighMemory

## 适用告警
- HighMemory

## 关键词
- memory
- mem
- server_monitor_memory_usage_percent
- server_monitor_memory_available_bytes

## 典型现象
内存使用率持续升高，可用内存下降，可能伴随缓存膨胀、进程泄漏或批处理任务占用。

## 关键指标
- server_monitor_memory_usage_percent
- server_monitor_memory_available_bytes

## 排查步骤
1. 查看 memory usage 15m 和 1h 趋势，判断是否单调增长。
2. 对比 available bytes，确认是否接近业务安全水位。
3. 查看告警历史，判断是否为周期性任务或持续泄漏。
4. 结合近期发布和进程列表，定位高内存进程或缓存策略变化。

## 建议动作
- 低风险：收集内存趋势、进程占用和告警历史。
- 中风险：调整流量、扩容实例或优化缓存策略，需要人工确认。
- 高风险：强制 kill 进程、重启服务、修改配置必须走审批和回滚预案。

## 风险说明
Runbook 不能替代现场确认；内存类操作容易造成状态丢失或请求失败，必须保留人工审批。
