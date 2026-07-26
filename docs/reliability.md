# Reliability

本文记录当前本地 runtime 的可靠执行与恢复合同。它不是 production SLO、跨区域灾备或外部 Provider 可用性声明。

## 1. Durable truth

MySQL 是 Incident、Evidence、Configuration、Task、Decision、Change 和 Verification 的 durable truth。正常 API/Worker 启动不修改 schema；`cloudops-migrate` 是唯一 migration owner。

`async_tasks` / `async_task_attempts` 保存 subject、cycle、expected version、payload/checkpoint identity、attempt、lease、dedupe 与 logical operation key。Kafka、Redis、内存 queue 和旧 lease loop 都不是当前 claim path。

## 2. Claim and shutdown

Worker 的 Investigate、Deliver、Observe、Verify pool 分别限制 concurrency、lease、heartbeat、handler deadline 与 external deadline。Claim/heartbeat/complete/retry 必须匹配 owner 和 lease generation；过期 task 可 takeover，旧 owner 后续写入被拒绝。

Shutdown 顺序为停止新 claim、bounded drain、取消剩余 handler/heartbeat、关闭 management listener 和 MySQL。未知外部结果不会在 shutdown timeout 时被伪造成 succeeded/failed；后续执行必须先按稳定 operation key reconcile。

Provider Gateway 未配置时 standby runner 不 claim，因此 unavailable Provider 不消耗 attempt，也不制造 dead/success 结果。

## 3. Local lifecycle

顶层 lifecycle 固定到 `cloudops-local` / `kind-cloudops-local` / `cloudops-system` / `cloudops`，拒绝宽泛 target override。

- `local-up`：build/load、migrate、Helm reconcile、readiness、loopback access；
- `local-restart`：保留 durable data 后重启 API/Worker；
- `local-down`：停止 workload，保留 PVC 与 `.cloudops/`；
- `local-doctor`：分别诊断 prerequisite、cluster、release、schema、Provider、port 和 backup；
- `local-status`：只读输出 runtime/data/Provider 摘要。

## 4. Backup and restore

`local-backup` 记录 backup format、semantic contract、source SHA、schema version/identity、逐表 row count、configuration/secret manifest 和 checksums。

`local-restore`：

1. 验证 private path、format 与所有 checksum；
2. 创建隔离 staging database；
3. 导入并比对 schema identity、version 和 row counts；
4. 自动创建活动库 rollback backup；
5. 停止 API/Worker、恢复活动库、重新启动并验证；
6. 任一步失败则恢复原活动数据并保留诊断材料。

Schema-only identity 会移除数据相关 `AUTO_INCREMENT` 值，避免相同 schema 因 row history 产生假漂移。

## 5. Current status

当前 Task 0 的 local backup、隔离 restore、rollback、restart、down/up 数据持久性与 doctor 已有本工作树运行证据。External Provider recovery、Scenario、容量/retention、production backup/DR 和 hosted execution 仍按实施状态保持 `NOT RUN`。

```bash
make local-status
make local-doctor
go test ./internal/asyncjob ./internal/bootstrap/... ./internal/migration
```

精确结果见 [实施状态](evidence/cloudops-implementation-status.md)。
