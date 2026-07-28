# Operations

本文是当前 Local Owner runtime 的操作入口。所有命令固定作用于 `cloudops-local` / `kind-cloudops-local` / `cloudops-system` / Helm release `cloudops`，不会接受任意 cluster、Namespace 或 release 覆盖。

## 1. Lifecycle

```bash
make local-up
make local-status
make local-open
make local-logs COMPONENT=api
make local-restart
make local-doctor
make local-down
```

`local-up` 构建并加载本工作树的 API、Worker、Migrate 与 Demo 镜像，reconcile 唯一 Chart，等待 migration 与 runtime readiness，并建立 loopback port-forward。命令返回后的 port-forward 是独立可诊断进程；若浏览器工具运行期间因 rollout 断开，可对同一 Service 建立前台转发：

```bash
kubectl --context kind-cloudops-local \
  -n cloudops-system port-forward --address=127.0.0.1 \
  service/cloudops-api 18080:8080
```

端口转发只改变访问通道，不改变 Helm、Pod、Provider 或 durable data 状态。

## 2. Scenario lifecycle

```bash
make scenario-up
make scenario-status
make scenario-down
make scenario-status
```

`scenario-up` 在 canonical release 中生成唯一 Scenario ID，部署 healthy/fault/traffic workload，等待 Kubernetes 与 Metrics/Alert/Logs/Traces 数据真实可见，再注入 bounded fault。已有 active Scenario 时命令保持原 identity，拒绝静默替换。

`scenario-status` 为只读检查。Active 状态应报告：

```text
scenario_state=active
scenario_write_gate=true
scenario_runtime_resources=6
scenario_kubernetes=PASS
scenario_metrics=PASS
scenario_alert=PASS or PASS_RESOLVED
scenario_logs=PASS
scenario_traces=PASS
scenario_agent=PASS
```

`scenario-down` 先确保 fault recovery 与 Alert resolution，再关闭 Scenario、移除其 Deployment/Service/ServiceAccount、关闭 Worker write gate 与 scale RBAC。成功后必须为：

```text
scenario_state=inactive
scenario_write_gate=false
scenario_runtime_resources=0
scenario_stale_firing_alerts=0
```

Alert、Context Snapshot、Plan 和关联领域 history 按 retention policy 保留；删除 retained Scenario history 是另一项显式操作，不属于 `scenario-down`。

## 3. Backup, restore, reset

```bash
make local-backup
make local-restore BACKUP=.cloudops/backups/<backup-id>
make local-reset
```

- Backup 保存 checksummed schema/data、source revision、schema identity、configuration/secret manifest 与 row counts。
- Restore 先导入隔离 staging database，验证后才切换活动库；失败自动 rollback。
- Reset 必须 backup-first、显式确认，并保持固定 target boundary。
- `local-down`、`scenario-down` 都不会删除 backup 或 retained domain data。

## 4. Diagnostics

```bash
make local-status
make local-doctor
kubectl --context kind-cloudops-local -n cloudops-system get pods
helm --kube-context kind-cloudops-local -n cloudops-system status cloudops
```

Provider unavailable、partial 或 stale 必须保留具体 source、time、request/trace ID 与 next action。端口连接失败不能直接推断为 release failure；先分别核对 port-forward、Service endpoints、Pod readiness 与 `/readyz`。

## 5. External boundary

GitHub、Argo、Registry、hosted Actions、staging 与 production 不由本地 Scenario 隐式授权。没有当前 credential、exact Plan/Authorization 和用户明确外部写权限时，结果必须是 `NOT RUN`。本地 Kubernetes scale 证据不能替代 Git PR、human merge、Argo exact revision 或 hosted signing evidence。

当前实际 run、对象 ID、截图与 `PASS`/`FAIL`/`NOT RUN` 见 [Phase 9 最终证据](evidence/phase-9-scenario/final-evidence-report.md)。
