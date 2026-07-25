# CloudOps 实施任务书

> 状态：`READY`
>
> 产品契约：V1
>
> 规范来源：[`CloudOps-Implementation-Spec.md`](CloudOps-Implementation-Spec.md)
>
> 用途：将实施规范中的每个 Phase 转换为可独立执行的全栈任务和 Codex 提示词

## 1. 使用方式

本文不是第二份产品设计。领域、架构、API、数据和验收细节以实施规范、`CONTEXT.md` 和 Accepted ADR 为准；本文只负责任务编排、依赖判断和开工提示词。发生冲突时，以实施规范和更高编号且明确 supersede/refine 的 ADR 为准。

Phase 编号和任务编号只允许出现在文档、任务状态和验收报告中，不得成为源码、测试、文件名、配置、数据库、脚本、Chart、资源名或持久数据的一部分。

每次可以使用本文最后的总控提示词让 Codex 持续实施，也可以单独复制某个任务的提示词。单任务完成后，应在 `docs/evidence/cloudops-implementation-status.md` 记录状态、实际证据、阻塞和新解锁任务；该状态文件由首次实施任务创建。

任务状态只使用：

- `READY`：硬依赖已满足，可以实施。
- `IN_PROGRESS`：正在进行全栈实现。
- `BLOCKED`：存在无法在当前权限或环境中解决的阻塞。
- `VALIDATING`：功能已可运行，正在进行该任务的一次 MCP 联调。
- `DONE`：任务范围和代表性真实联调均完成。
- `DONE_WITH_NOT_RUN`：本地核心能力完成，但明确列出的外部 Provider 分支未运行。

## 2. 全局执行约束

所有任务共同遵守以下规则：

1. 工作目录固定为 `/home/monody/k8s/CloudOps-Copilot`。开始前读取适用的 `AGENTS.md`，执行 `git status --short --branch`，以当前工作树和源码为准，保留已有修改。
2. 必读 `CONTEXT.md`、`CloudOps-Implementation-Spec.md`、`docs/adr/README.md`、ADR 0045，以及当前任务引用的 ADR。旧 generation-labelled 设计只作为 superseded 历史迁移输入。
3. 产品只有唯一 V1 契约。`/api/v1` 和 OpenAPI 可以表达契约身份；内部实现使用语义命名，不添加 V1/V2/V3 或 numbered phase 前缀。
4. 每个任务按前端、后端、API、MySQL 数据、Provider 和运行方式的纵向能力交付。禁止只做前端壳、mock 页面、孤立 API 或手动外部链接。
5. 中文优先、Lucide-only、禁止 emoji。Live Mode 只显示真实 Provider 和领域数据，Unavailable/Partial/Empty 必须如实呈现。
6. 在每个任务开始时检查当前可用 Skill 与 MCP，完整读取适用 Skill 的 `SKILL.md` 后使用。优先使用领域建模、前端实现或审查、云原生、浏览器和联调相关能力；不为形式调用无关 Skill。
7. 实现中只运行使当前能力可编译、可启动或定位故障所需的 focused checks。任务整体完成后再运行一次真实 UI -> API -> Provider MCP 联调，并覆盖一个最相关的失败或不可用状态。
8. Fixture、静态截图和接口存在性不能代替真实联调。结果严格写为 `PASS`、`FAIL` 或 `NOT RUN`，不得扩大为整项目结论。
9. 允许在任务完成边界对本任务的精确文件执行 `git add`、本地 `git commit`，并将当前任务的精确提交正常 push 到当前非默认实施分支，无需等待 Owner 回复。提交信息使用语义描述；不得用宽泛暂存命令把无关工作树修改带入提交。push 前核对 remote、branch、commit list 和工作树，push 后记录远端 SHA 与检查结果。禁止 force push、改写远端历史、推送 tag、推送无关提交或直接推送默认分支。创建 PR、其他外部写入和不可逆清理仍需明确授权。不打印、回显或提交 secret。
10. 不因某个可选 Provider 不可用而停止所有工作。记录精确阻塞后，继续其他依赖已满足的任务。

## 3. 依赖与非绝对顺序

下表中的硬依赖约束任务的最终验收，不禁止提前完成接口研究、无冲突的 Provider adapter 或 UI 结构准备。任何提前工作都不能绕过真实数据和 MCP 验收后宣称任务完成。

| 任务 | 对应 Phase | 硬依赖 | 可以交错推进 |
|---|---|---|---|
| 任务 0：语义基线与本地生命周期 | Phase 0 | 无 | 只允许先做只读审计；数据迁移必须由本任务统一执行 |
| 任务 1：平台 Shell 与 Settings | Phase 1 | 任务 0 的 API、数据和生命周期契约稳定 | 可先准备视觉 token、路由信息架构和表单结构 |
| 任务 2：Infrastructure 与 Atlas | Phase 2 | 任务 0；任务 1 的 Operational Scope 和 Shell contract | 可与任务 3、任务 5 并行 |
| 任务 3：Monitoring | Phase 3 | 任务 0；任务 1 的 Provider、Scope 和 Query contract | 可与任务 2、任务 5 并行 |
| 任务 4：Logs 与 Traces | Phase 4 | 任务 1；任务 2/3 的 resource/time context contract | Provider adapter 可提前；跨 Workspace 验收需任务 2/3 可用 |
| 任务 5：Alerts | Phase 5 | 任务 0；任务 1 的通知、Settings 和 Context Link contract | 可与任务 2、任务 3、任务 4 并行 |
| 任务 6：Agent | Phase 6 | 任务 1；任务 2-5 至少提供可用的真实 Evidence sources | Agent persistence 可提前；完整验收等待 Evidence 链 |
| 任务 7：Incidents 与 Verify | Phase 7 | 任务 5、任务 6；任务 2-4 的 Context Link contract | UI projection 可与任务 8 的独立 groundwork 交错 |
| 任务 8：Operations 与 DevOps | Phase 8 | 任务 6；Incident-bound execution/verify 需要任务 7 | local reversible action 可先做；外部写始终单独授权 |
| 任务 9：Scenario 与最终收敛 | Phase 9 | 任务 0-8 的本地必需能力完成 | 不提前宣称全链 PASS；可提前维护 Scenario workload |

建议调度不是固定数字顺序：任务 0 和任务 1 建立公共底座后，任务 2、3、5 可根据 Provider readiness 选择；任务 4 在 resource/time contract 稳定后接入；随后汇入任务 6、7、8；任务 9 最终证明完整主链。

## 4. 分任务提示词

### 任务 0：语义基线与本地生命周期

```text
在 /home/monody/k8s/CloudOps-Copilot 中执行《CloudOps 实施任务书》的任务 0，对应实施规范 Phase 0。

先完整读取任务书第 1-3 节、实施规范 Phase 0、CONTEXT.md、ADR 0040/0042/0045 和适用 AGENTS.md，并核对当前工作树。不要停留在审计或计划，确认迁移边界后直接完成前端、后端、API、数据与本地运行入口的纵向实现。

本任务必须 backup-first：建立可验证的 local backup/restore/doctor，再迁移到唯一 /api/v1 和语义化内部命名，保留现有领域数据，删除登录/RBAC/阶段化 runtime 与平行公开启动路径。V1 只用于明确契约边界，Phase 不得进入实现名称。

主动使用适用 Skill；完成后使用可用 MCP 验证 make local-up、无登录 UI、`/api/v1` Network 请求、保留的 Incident/Evidence、重启持久性和一次真实 restore。只在任务整体完成后做该次联调。

更新实施状态文件，报告修改、数据审计、MCP 证据及 PASS/FAIL/NOT RUN；达到退出条件后停止本任务，不自动扩大范围。
```

### 任务 1：平台 Shell、Operational Scope 与 Settings

```text
在 /home/monody/k8s/CloudOps-Copilot 中执行《CloudOps 实施任务书》的任务 1，对应实施规范 Phase 1。先检查任务 0 的实际契约和状态，再读取实施规范对应章节、ADR 0024/0027/0028/0030/0032/0037/0038/0044 及适用 AGENTS.md。

直接纵向实现平台 Shell、十个 Workspace 路由、Operational Scope、Settings、Configuration Revision、Provider health、通知入口、统一错误/SSE/Context Link contract。前端中文优先、Lucide-only，修复滚动所有权和 Back/Forward；后端必须真正保存并应用配置，secret 不得回显。

使用适用的前端设计/审查 Skill 和浏览器类 MCP。任务完成后只做一次代表性联调：从 Settings 修改并应用配置，确认 API、持久 revision 和 Worker 边界生效，同时验证失败 apply、长页面滚动和 mobile 导航。

更新实施状态文件并按 PASS/FAIL/NOT RUN 报告；不得用静态页面或 mock API 作为完成证明。
```

### 任务 2：Infrastructure 与 Operations Atlas

```text
在 /home/monody/k8s/CloudOps-Copilot 中执行《CloudOps 实施任务书》的任务 2，对应实施规范 Phase 2。核实任务 0 已完成，并确认任务 1 的 Shell、Operational Scope 与 Provider contract 可用；读取实施规范对应章节和 ADR 0021/0029/0031/0037/0038。

纵向实现真实 Kubernetes typed reader、topology/resource/event API、Infrastructure Workspace、Overview Operations Atlas 和结构化 fallback。Atlas 使用 Three.js，数据必须来自当前活动集群；不得制作装饰性拓扑或把 kubectl/Grafana 页面当作原生功能。

使用适用的前端/Three.js/界面审查 Skill，以及浏览器和 Kubernetes 相关 MCP。完成后验证真实 Workload -> Pod/Event -> Namespace -> Back 的上下文连续性，并验证 Provider unavailable、desktop/mobile、canvas nonblank pixel 和 structured fallback。

更新实施状态文件并如实报告任务级结果，不扩大为整项目 PASS。
```

### 任务 3：Monitoring

```text
在 /home/monody/k8s/CloudOps-Copilot 中执行《CloudOps 实施任务书》的任务 3，对应实施规范 Phase 3。确认任务 0 完成，任务 1 的 Scope、Settings 和 Query contract 可用；读取实施规范对应章节及 ADR 0021/0034/0038。

纵向实现 Prometheus bounded adapter、Query Definition/Execution、审计、Monitoring Workspace、guided/expert query、chart/table/history 和精确 Context Link。用户查询和 Agent 查询必须共享受限合同，但保留明确授权边界。

使用适用 Skill 与浏览器/Prometheus 相关 MCP。任务完成后验证真实 Workload 的 guided query、expert PromQL、保存定义、精确 Grafana link，以及非法或过大查询和 Provider unavailable 状态。

只接受真实 UI -> `/api/v1` -> Prometheus 证据；更新实施状态文件并报告 PASS/FAIL/NOT RUN。
```

### 任务 4：Logs 与 Traces

```text
在 /home/monody/k8s/CloudOps-Copilot 中执行《CloudOps 实施任务书》的任务 4，对应实施规范 Phase 4。确认任务 1 的共享合同可用，并核对任务 2/3 的 resource/time context；读取实施规范对应章节及 ADR 0021/0034/0035/0038。

纵向实现 Elasticsearch bounded logs、Tempo trace search/detail、Logs/Traces Workspaces、虚拟化长列表、waterfall、Kubernetes resource correlation、trace_id 关联、Evidence 保存和 Context Snapshot。不得复制完整遥测数据库或用 fixture 伪装 Live Mode。

使用适用前端/可观测性 Skill 与浏览器、Logs、Traces 相关 MCP。完成后验证 Monitoring 时间点 -> Logs -> trace_id -> Trace waterfall -> Workload -> 冻结上下文，并覆盖 empty、truncated 和 Provider unavailable。

更新实施状态文件，只对实际运行的 Provider 链报告 PASS。
```

### 任务 5：Alerts 与显式 Incident escalation

```text
在 /home/monody/k8s/CloudOps-Copilot 中执行《CloudOps 实施任务书》的任务 5，对应实施规范 Phase 5。确认任务 0 完成且任务 1 的通知、Settings、Context Link contract 可用；读取实施规范对应章节及 ADR 0020/0033/0038/0044。

纵向实现 Signal-to-Alert normalization、独立 Alert lifecycle、ack、bounded silence、Investigation/Incident link、Escalation Policy、Alerts Workspace 和 Owner Notification。迁移当前 automatic Signal-to-Incident 行为并保留 provenance；默认不得自动创建 Incident。

使用适用领域建模 Skill，以及浏览器和 Alertmanager 相关 MCP。完成后验证真实 firing -> Alert -> acknowledge -> silence -> Investigation 或显式 Incident -> resolved Signal，并验证重复 webhook 不产生重复领域记录。

更新实施状态文件，准确报告 UI、API、MySQL 和 Alertmanager 证据。
```

### 任务 6：Agent Investigation、Consultation 与 Knowledge

```text
在 /home/monody/k8s/CloudOps-Copilot 中执行《CloudOps 实施任务书》的任务 6，对应实施规范 Phase 6。核对任务 1 的配置/上下文合同，以及任务 2-5 已可提供的真实 Evidence source；读取实施规范对应章节及 ADR 0025/0026/0034/0035/0036/0039/0043。

纵向实现 Investigation、Consultation、message/SSE、Context Snapshot、bounded tools、Evidence citation、Agent Workspace/global panel、Knowledge Item、Runbook Guidance、action card 和三层 authority。不得展示 Chain of Thought，不得让旧聊天、Knowledge 或 Runbook 冒充当前 Evidence，未授权不得执行 mutation。

使用适用领域/Agent Skill 和浏览器及 Provider MCP。任务完成后验证真实 Alert Investigation、tool progress/Evidence/outcome、Logs Consultation、导航后上下文、显式新 snapshot、Owner-confirmed Knowledge 及后续 exact revision 引用。

模型或 Provider 不可用时精确标记 NOT RUN，不得用 fixture 声称真实 Agent 链 PASS；更新实施状态文件。
```

### 任务 7：Incidents 与 Verify 回路

```text
在 /home/monody/k8s/CloudOps-Copilot 中执行《CloudOps 实施任务书》的任务 7，对应实施规范 Phase 7。确认任务 5/6 可用，并核对任务 2-4 的 Context Link contract；读取实施规范对应章节及 ADR 0018/0020/0025/0039。

纵向收敛 `/api/v1` Incident projection、Alert relations、timeline、Evidence、Investigation、decision、recovery、cycle isolation、attention、Verification、ResolutionReport 和 close。删除 Incident-only 品牌与被独立 Workspace 取代的旧区块，但保留历史数据和 provenance。

使用适用领域/前端 Skill 与浏览器 MCP。完成后验证 Alert -> Incident -> 第二 Alert -> Investigation -> decision -> Verification failure 返回 Investigate -> common window PASS -> ResolutionReport -> close，确认 Delivery 不会直接产生 Resolved。

更新实施状态文件并提供真实 UI/API/data 证据。
```

### 任务 8：受控 Operations 与 DevOps

```text
在 /home/monody/k8s/CloudOps-Copilot 中执行《CloudOps 实施任务书》的任务 8，对应实施规范 Phase 8。确认任务 6 的 Operation Plan/authority contract 可用；涉及 Incident-bound verification 时同时要求任务 7 可用。读取实施规范对应章节及 ADR 0006/0007/0008/0036/0039。

纵向实现 immutable Operation Plan、exact Action Authorization、execution/audit/expiry/precondition、local reversible actions、ChangeCandidate、DeploymentBaseline、DevOps Workspace 和 Verification links。GitHub/Argo 只是可选分支，不能成为云原生与 Agent 主链的前置条件。

使用适用 Skill 与浏览器、Kubernetes、GitHub、Argo 相关 MCP。源码实施分支的正常 push 遵守任务书的预授权边界；产品运行时对 Kubernetes、GitHub 或 Argo 的写入仍必须绑定当前任务内的精确 Operation Plan 和 Action Authorization，没有凭据或授权时保持 NOT RUN。完成后验证 Plan -> Owner review -> execution -> current Evidence Verify，以及 material field 变化使旧授权失效。

更新实施状态文件，保证产品操作零授权零外部写，不以 mock 替代外部链。
```

### 任务 9：真实 Scenario、视觉质量与最终收敛

```text
在 /home/monody/k8s/CloudOps-Copilot 中执行《CloudOps 实施任务书》的任务 9，对应实施规范 Phase 9。先审计任务 0-8 的实际状态和未运行项；只有本地必需能力已完成，才能开始最终验收。读取实施规范 Phase 9、Definition of Done 及所有仍适用 ADR。

完成真实 scenario-up/status/down、受控 workload/traffic/fault、完整 Observe -> Detect -> Investigate -> Decide -> Act -> Verify browser flow，以及视觉、responsive、scroll、accessibility、performance、failure-state 和历史文档收敛。删除临时 adapter、假 fixture claim、dead link、平行部署路径与 inactive UI。

必须使用适用前端设计/审查 Skill 和可用 MCP；按规范视口完成 screenshot、canvas pixel、交互和 console/network 检查。Scenario 必须真实产生 Kubernetes、Metrics、Alerts、Logs、Traces 和 Agent 数据，DevOps 分支可选。

更新实施状态文件和最终 evidence report。逐项报告 Definition of Done 的 PASS/FAIL/NOT RUN；任一外部未运行项不得被包装为整项目 PASS。
```

## 5. Codex 总控提示词

纯文本复制入口：[`CloudOps-Codex-Master-Prompt.txt`](CloudOps-Codex-Master-Prompt.txt)

```text
你是 CloudOps-Copilot 的全栈实施负责人。工作目录是 /home/monody/k8s/CloudOps-Copilot。

完整读取并严格执行：
- CONTEXT.md
- docs/CloudOps-Implementation-Spec.md
- docs/CloudOps-Implementation-Taskbook.md
- docs/adr/README.md
- docs/adr/0045-single-v1-contract-and-semantic-code.md
- 每个当前任务引用的 ADR 与所有适用 AGENTS.md

先执行 git status --short --branch 并检查实际源码、配置、数据迁移和可用 Provider。以当前工作树为准，保留所有已有修改。旧 generation-labelled 设计只是 superseded 历史迁移输入，不得覆盖当前规范。

按任务书维护 docs/evidence/cloudops-implementation-status.md。先根据真实依赖把任务标记为 READY/BLOCKED，再选择一个依赖已满足且价值最高的任务实施。顺序不要求机械遵循 Phase 编号：公共契约稳定后可根据 Provider readiness 调整任务 2、3、4、5 的顺序，也可提前做无冲突 groundwork；但不得绕过硬依赖、真实数据和 MCP 验收宣称任务 DONE。

不要只做审计、计划或重复文档。确定当前任务后直接完成前端、后端、`/api/v1`、MySQL 数据、Provider 与 Make 运行入口的纵向实现。产品只有唯一 V1 契约；内部代码使用语义命名，V2/V3 和 numbered phase 不得进入第一方实现。Phase/任务编号只存在于文档和报告。

每个任务开始时检查可用 Skill 和 MCP，完整读取相关 SKILL.md 并实际使用适合的领域、前端、云原生、浏览器或联调能力。不可用时记录原因并继续，不得伪造调用或结果。

实现期间只运行必要的 focused checks。每个大任务整体完成后，再通过可用 MCP 做一次真实 UI -> /api/v1 -> Provider 联调，并覆盖一个关键 failure/unavailable path。Fixture、静态截图、旧证据和接口存在性不能代替真实联调。

每完成一个任务：
1. 更新状态文件和实际文件清单。
2. 记录 Browser、Network、Data、Provider、Console 证据。
3. 严格报告 PASS/FAIL/NOT RUN。
4. 重新计算已解锁任务并立即继续，不需要等待人工确认。
5. 必要时只对当前任务的精确文件执行 git add，并创建语义化本地 commit；不得暂存或覆盖无关修改。
6. 本地 commit 完成后，可直接正常 push 当前非默认实施分支；记录 remote、branch、本地/远端 SHA 和检查结果，不得 force push 或夹带无关提交。

只有遇到需要新权限、secret、创建 PR、除上述源码分支 push 之外的外部写入、不可逆操作，或同一阻塞经充分排查仍无法解决时才暂停并询问。可选 Provider 阻塞不能阻止其他 READY 任务。允许精确 git add、本地 git commit，并正常 push 当前非默认实施分支，无需等待 Owner 回复；禁止 force push、改写远端历史、推送 tag、默认分支或无关提交。

持续执行，直到任务 0-9 全部 DONE/DONE_WITH_NOT_RUN，或出现确实必须由用户处理的阻塞。最终按实施规范 Definition of Done 汇总，不能把局部 PASS 扩大为整项目 PASS。
```
