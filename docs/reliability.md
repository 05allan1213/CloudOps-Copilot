# Reliability

本文记录当前本地 runtime 的可靠执行、恢复与 Scenario 清理合同。它不是 production SLO、跨区域 DR 或外部 Provider 可用性声明。

## 1. Durable truth

MySQL 是 Incident、Alert、Evidence、Agent、Configuration、Task、Decision、Operation 与 Verification 的 durable truth。正常 API/Worker 启动不修改 schema；`cloudops-migrate` 是唯一 migration owner。

Task、attempt、checkpoint、lease、generation、deadline、dedupe 与 logical operation key 均持久化。Kafka、Redis、内存 queue 和旧 lease loop 不属于当前 claim path。

## 2. Claim, fencing and shutdown

Worker 的 Investigate、Deliver、Observe、Verify 与 Operation pool 分别限制 concurrency、lease、heartbeat、handler/external deadline 和 retry。Claim/heartbeat/complete/retry 必须匹配 owner 与 lease generation；过期 task 可 takeover，旧 owner 后续写入被拒绝。

Operation 在 effect 前重新验证 exact Authorization、Plan hash、expiry、Configuration Revision 与 provider precondition。未知外部结果不会在 shutdown timeout 时被伪造成成功或失败；后续执行必须按稳定 operation key reconcile。

Shutdown 顺序为停止新 claim、bounded drain、取消剩余 handler/heartbeat、关闭 management listener 和 MySQL。

## 3. Local and Scenario lifecycle

顶层 lifecycle 固定到 `cloudops-local` / `kind-cloudops-local` / `cloudops-system` / `cloudops`：

- `local-up`：build/load、migrate、Helm reconcile、Provider readiness 与 loopback access；
- `local-restart`：保留 durable data 后重启 API/Worker；
- `local-down`：停止 workload，保留 PVC 与 `.cloudops/`；
- `local-doctor`：分别诊断 prerequisite、cluster、release、schema、Provider、port 与 backup；
- `scenario-up`：创建 identity，等待 Evidence Plane，再注入 bounded fault；
- `scenario-status`：只读核对 runtime、fault、Provider、Agent、history 与 write gate；
- `scenario-down`：先恢复/等待 Alert resolved，再删除 Scenario runtime、关闭 write gate/RBAC，保留 history。

Active Scenario 会跨 `local-up` 保持同一 identity 与 recovered/degraded 状态，避免升级期间静默替换运行对象。

## 4. Backup, restore and retention

`local-backup` 记录 format、semantic contract、source SHA、schema version/identity、逐表 row count、configuration/secret manifest 与 checksums。

`local-restore`：

1. 验证 private path、format 与 checksum；
2. 创建隔离 staging database；
3. 导入并比对 schema identity、version 和 row counts；
4. 自动创建活动库 rollback backup；
5. 停止 API/Worker、恢复活动库、重启并验证；
6. 任一步失败则恢复原活动数据并保留诊断材料。

Raw telemetry 由 Provider retention 与 Settings bounds 控制；durable Evidence/domain history 受独立 retention contract 约束。`scenario-down` 不删除 tagged Alert、Context Snapshot、Plan 或 Resolution history。

## 5. Failure and unavailable behavior

- Provider partial/stale/unavailable 作为显式状态返回，不用 fixture 填补。
- 端口转发失败与 release/Pod failure 分开诊断；可重建同一 Service 的前台 loopback 转发。
- WebGL failure 使用 structured topology fallback，不能出现空白页。
- Offline browser refresh 保留 stale projection，并显示 error code、next action 与 retry；网络恢复后重新读取权威 projection。
- GitHub/Argo/hosted/staging/production 未运行时保持 `NOT RUN`，不会升级为“基本通过”。

## 6. Verification

```bash
make local-status
make local-doctor
make scenario-status
go test -race -count=1 ./...
```

Scenario 流程还应通过真实浏览器集成测试验证 Provider effect、持久化结果、刷新回显和 runtime cleanup；生成的日志、截图与 trace 不进入源码仓库。
