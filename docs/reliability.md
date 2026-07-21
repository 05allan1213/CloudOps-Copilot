# Reliability

本文记录 V3 的可靠执行、事务、shutdown 和 cutover 边界。它不是生产 SLO、备份或灾备声明。

## 1. MySQL 是唯一 durable runtime

当前 V3 queue 使用 [async task repository](../internal/asyncjob/repository.go) 和 `async_tasks` / `async_task_attempts`。Kafka、Redis、内存队列和 legacy lease loop 都不是 V3 claim path。

每个 Task 固定：

- Incident/cycle/subject/type/transition。
- expected subject version、dedupe key、logical operation key。
- attempt/max attempts、lease generation、available time 和 priority。
- bounded payload、checkpoint metadata 和 replay generation。

业务状态、Task terminal result、Timeline/Event 和下一 Task 必须在同一数据库事务内提交。外部网络调用不放入长事务；timeout 后先按稳定 logical operation key reconcile，不能盲目重放写操作。

## 2. Claim、lease 与隔离

[runner](../internal/asyncjob/runner.go) 为 `investigate`、`deliver`、`observe`、`verify` 各运行一个 claim loop，并分别配置并发、lease、heartbeat、handler deadline 和 external deadline。[worker bootstrap](../internal/bootstrap/worker.go) 只有在 MySQL、runtime-generation guard、operation registry 和四池 runner 均 ready 后才返回 ready。

关键规则：

- claim 使用 MySQL row lock / `SKIP LOCKED` 与 lease generation fencing。
- heartbeat、resolve、retry 和 dead transition 必须匹配 owner、generation 和未过期 lease。
- expired running Task 可由另一 Worker takeover；旧 owner 后续写入返回 lease lost。
- queue、task type、subject 和 transition 的非法组合由代码与数据库约束共同拒绝。
- 一个 poison/dead Task 不阻塞其他 queue 或同 queue 的健康 Task。

## 3. Retry、dead 与 replay

错误分为 transient、dependency unavailable、invalid、policy/security、stale/lease lost。只有 transient 类使用同一 Task 的 bounded retry/backoff；业务 retry 创建新的 AgentRun、Plan 或 VerificationRun。

Dead replay：

- 原 dead row 保持终态。
- 新 row 使用 `replay_generation + 1` 并记录 `replayed_from_task_id`。
- replay 前重新验证 Incident/cycle/subject version。
- 外部副作用仍使用跨 replay 稳定的 logical operation key。

## 4. Bounded shutdown

[async runner shutdown](../internal/asyncjob/runner.go) 的顺序是：

1. 同步停止全部 claim loop。
2. 在 drain window 内等待 in-flight handlers。
3. 超时后取消 handler/heartbeat context。
4. 未完成 Task 不伪造 succeeded/failed，等待 lease expiration 和 takeover。
5. management server 与 MySQL 在总 exit deadline 内关闭。

这保证 shutdown timeout 不会把未知外部结果误写成业务终态。

## 5. Runtime generation 与不可逆 marker

[cutover reader/guard](../internal/cutover/marker.go) 在 API、Worker 和 migrate startup/readiness 路径读取唯一 `CUTOVER-V3` ledger row。当前源码的 `CurrentRuntimeGeneration` 仍是 `compatibility`；出现 marker 后旧 binary 必须拒绝启动。

[cutover writer](../internal/cutover/writer.go) 由 `cloudops-migrate cutover-write` 暴露，并要求显式绑定：

- source exact SHA、binary image digest、source/target schema version。
- passed quiesce、external reconciliation、converter audit ledger UUID。
- 数据库内真实查询得到的零 legacy active lease。
- 显式且为零的 old Worker inventory。
- `--confirm-irreversible CUTOVER-V3`。

Writer 在 serializable transaction 和 MySQL advisory lock 下拒绝重复 marker、running/failed/missing prerequisite、release identity mismatch、schema mismatch、未知/非零 old Worker 或 active lease；它只写一个 passed ledger row。它不会切换 `CurrentRuntimeGeneration`、转换数据、恢复 ingress、删除 legacy schema 或调用外部系统。

命令形状：

```bash
go run ./cmd/cloudops-migrate cutover-write \
  --plan-version 7 \
  --source-exact-sha "$SOURCE_SHA" \
  --binary-image-digest "$MIGRATE_IMAGE_DIGEST" \
  --source-schema-version "$SCHEMA_VERSION" \
  --target-schema-version "$SCHEMA_VERSION" \
  --quiesce-ledger-id "$QUIESCE_LEDGER_ID" \
  --reconciliation-ledger-id "$RECONCILIATION_LEDGER_ID" \
  --converter-audit-ledger-id "$CONVERTER_AUDIT_LEDGER_ID" \
  --old-worker-count 0 \
  --confirm-irreversible CUTOVER-V3
```

缺少任何输入会在数据库连接前失败。实际执行是不可逆数据库写，不能用于 dry run。

## 6. 当前状态

| 控制 | 状态 | 说明 |
|---|---|---|
| MySQL queue/lease/fencing/replay source contract | `PASS` | `internal/asyncjob/` 与 [Phase 2 report](evidence/phase-2-async-runtime-domain-convergence-report.md) |
| Four-pool bounded shutdown source contract | `PASS` | `internal/asyncjob/runner.go`、`internal/bootstrap/worker.go` |
| Runtime marker reader/compatibility refusal | `PASS` | `internal/cutover/marker.go` |
| Fail-closed marker writer/CLI source contract | `PASS` | `internal/cutover/writer.go`、`internal/bootstrap/migrate/migrate.go` |
| 当前 HEAD 全量 MySQL integration/race | `NOT RUN` | 本文档切片只做静态检查 |
| Live quiesce/reconciliation/converter/backfill | `NOT RUN` | Phase 7A owning components/ledger evidence尚未执行 |
| `CUTOVER-V3` live marker write | `NOT RUN` | 未对任何数据库执行 |
| V3-only generation switch / ingress restore | `NOT RUN` | 当前 binary 仍为 compatibility generation |
| Phase 7B `CONTRACT-V3` deletion | `NOT RUN` | 必须晚于独立接受的 Release A Golden/audit |
| Backup/restore/DR/production retention | `NOT RUN` | V3 Demo 不作这些声明 |

## 7. 验证命令

```bash
go test ./internal/asyncjob ./internal/cutover ./internal/bootstrap/migrate
go test ./internal/migration -run 'Test.*(Fresh|Existing|Concurrent|Version)'
go run ./cmd/cloudops-migrate cutover-check
```

需要真实 MySQL 的检查必须使用 disposable database；没有数据库证据时保持 `NOT RUN`。
