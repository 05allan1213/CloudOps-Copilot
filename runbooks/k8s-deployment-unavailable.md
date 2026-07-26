# K8s Deployment Unavailable

## 适用告警
- K8sDeploymentUnavailable

## 关键词
- deployment
- unavailable
- replicas
- rollout
- k8s

## 典型现象
Deployment 的 ReadyReplicas 少于 Replicas，新版本 Pod 无法就绪，Rollout 处于 Progressing 或 Degraded 状态。

## 关键指标
- kube_deployment_status_replicas_available
- kube_deployment_status_replicas_unavailable
- kube_deployment_status_condition

## 排查步骤
1. 查看 Deployment 状态和 Rollout 历史，确认是否正在滚动更新。
2. 检查新版本 Pod 状态和事件，确认就绪失败原因（镜像拉取失败、探针失败、启动错误等）。
3. 获取新版本 Pod 日志，定位应用启动失败原因。
4. 检查资源配额和节点资源是否充足，排除调度失败。
5. 查看最近 ReplicaSet 变更，确认是否为配置或镜像变更触发。

## 建议动作
- 低风险：继续观察、通知负责人、收集 Deployment 状态和 Pod 事件。
- 中风险：回滚到上一稳定版本或扩容可用副本，必须人工确认。
- 高风险：删除 Deployment、修改关键配置，禁止由 AI 自动执行。

## 风险说明
任何写操作必须进入审批和审计流程；Runbook 只提供知识建议，不执行动作。
