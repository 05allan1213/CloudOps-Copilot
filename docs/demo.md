# Demonstration

当前可演示范围是 Task 0 的本地 Incident baseline，不是完整 Observe-to-Verify Scenario。

## Current workflow

```bash
make local-up
make local-status
make local-open
```

浏览器可在 loopback 打开保留的 Incident，查看 MySQL-backed Signal、Timeline、Evidence、Investigation、Remediation、Delivery 和 Verification projection。Network 必须只使用 `/api/v1`，无 login/session 请求。

持久性演示：

```bash
make local-restart
make local-down
make local-up
```

以上操作应保留同一 Incident/Agent/Evidence 数据。备份与 restore 使用：

```bash
make local-backup
make local-restore BACKUP=.cloudops/backups/<backup-id>
```

## Not implemented yet

实施规范中的 `scenario-up`、`scenario-status`、`scenario-down`，以及真实 Kubernetes fault、Metrics/Alerts/Logs/Traces、Agent、Owner-authorized recovery 和 Verify 主链属于 Task 9 与其前置任务。当前 Makefile 没有这些入口，不能使用旧脚本、Compose、fixture、静态截图或历史 Golden 报告替代。

GitHub/Argo/Registry/LLM 等外部系统默认关闭。任何需要 secret、human merge、PR、Registry publish 或外部 mutation 的演示都必须在对应任务获得明确权限并产生当前 exact-worktree 证据；否则结果是 `NOT RUN`。

当前验收记录见 [实施状态](evidence/cloudops-implementation-status.md)。
