# Migration Ledger

本文记录从旧本地 schema 到当前单一语义 baseline 的已执行转换。旧 migration chain 和代际 cutover 设计只存在于 Git history/历史报告，不是当前 runtime 输入。

## 1. Current contract

| 项目 | 当前值 |
|---|---|
| Clean-install migration | `migrations/00001_cloudops_baseline.sql` |
| Goose schema version | `1` |
| Active database | `cloudops` |
| Runtime migration owner | `cloudops-migrate` |
| API/Worker schema mutation | forbidden |
| Backup contract | `cloudops-semantic`, format `2` |

Baseline 包含 31 张领域/runtime 表。所有 table/index/config/task/resource 名称使用语义命名；migration filename、runtime profile 和 data discriminator 不携带交付编号。

## 2. Conversion invariants

- backup-first，源数据在转换前可恢复；
- 保留 public ID、timestamp、cycle、source identity、hash、provenance 与 audit relationship；
- 无法作为当前 native authority 的记录保留 imported provenance，不伪装成实时 Evidence 或当前授权；
- source/target counts、FK、unique identity、timeline order、hash 和 retained Evidence 必须验证；
- 切换后 API/Worker 必须从活动库读取，旧 schema/compatibility code/转换脚本从正常 runtime 删除；
- Git history 和私有备份保存原始 provenance，不重写历史 migration bytes。

## 3. Executed local evidence

当前 `cloudops-local` 数据库已通过一次性转换并在 restore/down-up 后保持：

| Domain | Count |
|---|---:|
| Incident | 8 |
| Agent run | 19 |
| retained Evidence | 11 |

Import audit `37ec2e04-8882-11f1-986d-56daa46a94da` 状态为 `completed`。11 条 retained Evidence 均为 contract version 1；public ID、required hash 和 provenance 审计违规为 0。

稳定 schema identity：

```text
sha256:56cbc891ea6a959184c01ea9a66a5bc917402ff9a26f90f54f5c431bd3e0a315
```

这些结果只限定到当前本地 Task 0 数据集，不代表 production/deployed database inventory。

## 4. Backup provenance

私有 `.cloudops/backups/` 保存源转换、空库、排障、最终 restore 输入和自动 rollback 备份。它们不进入 Git、文档附件或聊天输出。删除、轮换或跨机器导入需要单独的 Owner 操作。

## 5. Ongoing rules

- 后续 schema 变更必须 forward-only、语义命名，并与对应纵向任务同批验证；
- 不得重新引入平行 migration chain、runtime AutoMigrate 或 compatibility route；
- 配置 revision、Evidence schema、query definition 等真实 immutable revision 使用明确 revision/hash，不使用产品代际标签；
- 每次数据变更的实时证据记录在 [实施状态](evidence/cloudops-implementation-status.md)，不复用本 ledger 的旧 count 作为新任务证明。

```bash
make local-backup
make local-doctor
go test ./migrations ./internal/migration
```
