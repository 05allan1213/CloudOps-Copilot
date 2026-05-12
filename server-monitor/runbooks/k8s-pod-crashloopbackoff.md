# K8s Pod CrashLoopBackOff

## 适用告警
- K8sPodCrashLoopBackOff

## 关键词
- crashloopbackoff
- pod
- restart
- container
- k8s

## 典型现象
Pod 反复重启进入 CrashLoopBackOff 状态，容器启动后立即退出，Back-off 间隔逐渐增大。

## 关键指标
- kube_pod_container_status_restarts_total
- kube_pod_container_status_waiting_reason

## 排查步骤
1. 查看 Pod 最近事件和状态，确认 CrashLoopBackOff 原因（OOMKilled、Error、ConfigError 等）。
2. 获取 Pod 最近日志，定位容器退出原因（启动失败、配置错误、依赖不可用等）。
3. 检查 Pod 资源限制和实际使用，判断是否为 OOMKilled。
4. 检查 ConfigMap 和 Secret 挂载是否正确，环境变量是否完整。
5. 检查就绪/存活探针配置是否合理，是否存在探针超时导致重启。

## 建议动作
- 低风险：继续观察、通知负责人、收集 Pod 日志和事件。
- 中风险：调整资源限制或探针配置后重新部署，必须人工确认。
- 高风险：删除 Pod、修改 Secret，禁止由 AI 自动执行。

## 风险说明
任何写操作必须进入审批和审计流程；Phase 4 只提供知识建议，不执行动作。
