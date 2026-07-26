# Historical End-to-End Harness Notice

旧 external GitOps/Golden harness 已随平行 runtime、Compose、raw manifests 和旧脚本删除。本文档不再提供可执行入口，也不能作为当前 CloudOps 联调证据。

当前真实 Scenario 由 [实施规范 Task 9](../CloudOps-Implementation-Spec.md#phase-9真实-scenario视觉质量与最终收敛) 定义，前置依赖是 Task 0-8 的当前能力。计划入口为 `make scenario-up`、`make scenario-status` 和 `make scenario-down`；在这些命令实际实现并完成当前 UI -> `/api/v1` -> Provider 验收前，Scenario、GitHub/Argo integration、human merge、hosted Actions、LLM 和完整 Observe-to-Verify 一律为 `NOT RUN`。

历史逐阶段报告保留其 exact revision 的 provenance，但不允许复用为当前 worktree 的 PASS。当前结果只记录在 [实施状态](cloudops-implementation-status.md)。
