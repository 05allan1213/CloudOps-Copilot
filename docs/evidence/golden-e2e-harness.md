# Historical Golden Harness Notice

旧 external GitOps/Golden harness、demo GitOps contract、Argo manifests、Compose、raw manifests 与平行 runtime 已删除。本文不再提供可执行入口，也不能作为当前 CloudOps 联调证据。

当前真实端到端入口只有：

```bash
make local-up
make scenario-up
make scenario-status
make scenario-down
```

Phase 9 已在 canonical Helm release 上完成 Kubernetes、Metrics、Alerts、Logs、Traces、Agent、Owner-authorized recovery、Verify、retained history 与 post-down Live Mode 验收。当前证据见 [Phase 9 最终报告](phase-9-scenario/final-evidence-report.md)。

GitHub App write、human merge、Argo exact revision/sync observation、hosted Actions、Registry publish/sign/attest、staging 与 production 没有在本地 Scenario 中运行，均保持 `NOT RUN`。历史逐阶段报告只证明其 exact revision/run，不能复用为当前 worktree 的 PASS。
