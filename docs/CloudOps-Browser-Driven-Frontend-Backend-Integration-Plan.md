# CloudOps-Copilot 浏览器驱动真实前后端联调实施方案

> 状态：`PLAN_APPROVED=YES`
>
> Owner 共同理解确认：2026-08-02（Asia/Shanghai）
>
> 目标仓库：`/home/monody/k8s/CloudOps-Copilot`
>
> 规划时代码基线：`main@a8076f97a8b252d595f5040a618cd4c408873fe1`；执行时必须重新解析当前 HEAD、dirty worktree 与运行时 identity
>
> 本文档性质：前端全部实现后，由真实浏览器操作驱动的前后端功能联调执行权威
>
> 当前授权：`DOCUMENT_CREATION_ONLY`；是否以及何时开始联调由 Owner 通过后续执行提示词决定
>
> 外部交付：`NOT_AUTHORIZED`；不得 push、创建 PR、发布或部署到 staging/production

## 0. 文档定位

### 0.1 这次联调是什么

本轮联调只认可下面这一条完整证据链：

```text
真实 Chromium 中点击前端控件
-> 前端发出真实 /api/v1 请求
-> 当前 API / Worker 执行业务逻辑
-> 当前 MySQL 或本地 Provider 产生真实结果
-> 浏览器重新读取并正确展示权威结果
```

一次操作只有同时满足以下条件才可记为 `PASS`：

1. 真实页面中的按钮、表单、菜单或其他用户入口可以触发该操作。
2. 请求方法、参数、Scope、resource identity、absolute time window、`expected_version`、`expected_hash` 与 `Idempotency-Key` 等适用合同正确。
3. 后端确实执行了对应业务逻辑，而不是只返回前端预设状态。
4. 结果真实持久化到 MySQL，或真实作用于本地受控 Provider。
5. 页面刷新、直接重新进入或跨页返回后，UI 仍从后端权威投影展示正确结果。

HTTP `200/202`、Toast、前端临时状态、直接 API 调用、fixture、单元测试、后端测试或静态页面都不能单独构成本轮联调 `PASS`。

### 0.2 与现有方案的关系

冲突按以下顺序解释：

1. Owner 最新明确指令。
2. 本文档对最终真实联调、修复授权、无人值守行为和安全收口的规定。
3. 执行时的 live worktree、真实浏览器、真实运行时与真实数据事实。
4. `docs/CloudOps-Frontend-Detail-Optimization-Implementation-Plan.md` 对前端实现完成状态和冻结视觉的规定。
5. `docs/CloudOps-CPA-Frontend-Rebuild-Implementation-Plan.md` 中仍有效的页面能力、Router、API、SSE、领域与安全合同。
6. `docs/api-v1-openapi.yaml`、`docs/api.md`、`docs/security.md`、`docs/operations.md` 和 `docs/demo.md`。
7. 旧 evidence 只作为历史 provenance，不能证明当前 SHA 的联调结果。

本文档经 Owner 最新确认后，取代 CPA 重建方案第 6 节中“只修前端、不修改后端”和“默认不做真实写入”的旧边界。本轮允许按根因修复前端或后端，并允许执行本文限定的本地、隔离、可收口真实写链路。

### 0.3 启动授权

本方案完成不自动开始联调。只有 Owner 后续主动给出执行提示词，才表示：

```text
OWNER_START_AUTHORIZATION=YES
OWNER_DECIDES_FRONTEND_READY=YES
```

执行者仍须只读核对前端实现、构建和 focused checks 的客观完成事实。若明显仍有未实现能力、未闭合编译错误或运行阻塞，报告事实并停止，不得擅自提前联调，也不得自行把未完成实现解释成 Owner 已接受。

本轮不重新进行视觉判断。Owner 已完成视觉检查，执行者不得借联调重新设计页面。

## 1. 已批准决定

```text
INTEGRATION_DRIVER=REAL_BROWSER_USER_ACTIONS
PRIMARY_OBJECTIVE=BACKEND_FUNCTIONAL_BEHAVIOR_THROUGH_FRONTEND
REAL_UI_API_PERSISTENCE_PROVIDER_CHAIN_REQUIRED=YES
STANDALONE_FRONTEND_TEST_IS_INTEGRATION_PROOF=NO
STANDALONE_BACKEND_TEST_IS_INTEGRATION_PROOF=NO
DIRECT_API_CALL_IS_INTEGRATION_PROOF=NO

COVERAGE_UNIT=INDEPENDENT_USER_TRIGGERABLE_BACKEND_CAPABILITY
DUPLICATE_SIDE_EFFECTS=DEDUPLICATED
MISSING_PROMISED_UI_ENTRY=FRONTEND_DEFECT
INTERNAL_ONLY_ENDPOINT_REQUIRES_UI=NO

LOCAL_SCENARIO_REAL_WRITES=AUTHORIZED
EXISTING_DOMAIN_OBJECTS=MUTATION_FORBIDDEN
RUN_SCOPED_OBJECTS_REQUIRED=YES
RETAINED_TEST_HISTORY=ALLOWED
PRE_RUN_BACKUP=REQUIRED

FRONTEND_ROOT_CAUSE_FIX=AUTHORIZED
BACKEND_ROOT_CAUSE_FIX=AUTHORIZED
OPENAPI_TYPED_CLIENT_FIX=AUTHORIZED
FORWARD_ONLY_MIGRATION_FIX=AUTHORIZED
DESTRUCTIVE_DATABASE_CHANGE=NOT_AUTHORIZED

GITHUB_ARGO_EXISTING_READS=CONDITIONAL
GITHUB_ARGO_EXTERNAL_WRITES=NOT_AUTHORIZED
HUMAN_REAUTHENTICATION_DURING_RUN=FORBIDDEN
STAGING_PRODUCTION_WRITES=NOT_AUTHORIZED

REAL_LLM_PROVIDER=DEEPSEEK_V4_FLASH
REAL_LLM_CALLS=AUTHORIZED
LLM_SEMANTIC_QUALITY_REQUIRED=YES

BROWSER=CHROMIUM_ONLY
HEADLESS_UNATTENDED_RUN=YES
VISUAL_REVIEW=NOT RUN
MULTI_BROWSER=NOT RUN
SCREENSHOT=FAILURE_EVIDENCE_ONLY

MAX_RUNTIME=UNTIL_CONVERGED
MAX_FIX_ATTEMPTS_PER_ROOT_CAUSE=3
CONTINUE_INDEPENDENT_CAPABILITIES_AFTER_FAILURE=YES
LOCAL_FIX_COMMITS=AUTHORIZED
PUSH_PR_RELEASE=NOT_AUTHORIZED
```

## 2. 状态与总体结论

### 2.1 唯一状态词

- `PASS`：本项完整真实链路在当前精确代码和运行时上实际运行并通过。
- `FAIL`：本项已运行，但同一根因经过最多 3 轮修复、重建和重跑后仍未闭合。
- `NOT RUN`：本项明确排除、没有所需人工授权、前置依赖未满足，或执行结束时尚未涉及。
- `BACKEND_GAP`：前端已正确触发，但必要后端合同不存在、语义不足或不能满足产品能力；不得以前端伪造掩盖。

`blocked_by`、`root_cause`、`attempts` 和 `evidence` 是状态的附加字段，不新增含糊的 `BLOCKED` 或 `PASS_WITH_GAPS` 状态。

### 2.2 严格通过门槛

总体 `PASS` 必须同时满足：

- 所有范围内独立前端操作最终完成完整 UI -> API -> backend -> persistence/Provider -> UI 链路。
- 前端与后端阻塞缺陷均为 0。
- 必需的本地 Kubernetes、Prometheus、Alertmanager、Elasticsearch、Tempo、MySQL、Worker 和 LLM 链路可用并完成要求。
- 本地写链路、关键保护分支和最终安全收口全部通过。
- 没有未闭合的范围内 `BACKEND_GAP`。
- 最终完整浏览器功能回归通过。

只有预先明确排除的 GitHub/Argo 外部写、人工重新授权、staging、production、发布和多浏览器项目可以保持 `NOT RUN` 而不阻止本地联调总体结论。

没有通过、没有涉及、因依赖未运行或被明确排除的项目都必须逐项如实记录，不能为了得到整齐结果隐藏或提升状态。

## 3. 覆盖模型

### 3.1 覆盖单位

覆盖按“每项独立、可由用户从前端触发的后端能力”计算，而不是机械按按钮数量计算：

- 多个入口调用同一后端合同：分别确认入口能发出正确请求，但只制造一次真实副作用。
- 一个入口触发多个独立后端阶段：每个阶段分别保留证据。
- 纯本地 UI 控件、主题切换和没有后端行为的展示切换：不作为本轮重点。
- 自动页面读取、SSE 和后台刷新虽然不是按钮，也属于前端使用的真实后端能力，必须随用户流程验证。
- 方案承诺的用户能力如果没有可达入口、永久 disabled 或无法完成流程，判为前端缺陷并补齐。
- internal listener、Alertmanager webhook、迁移进程或运维脚本专用接口不强行添加前端入口。

### 3.2 执行时双向盘点

开始真实点击前，必须重新生成能力矩阵并双向核对：

```text
公开 Router 与实际可达页面
<-> 页面可操作控件与事件处理
<-> frontend/src/api typed client
<-> docs/api-v1-openapi.yaml
<-> internal/api runtime routes
```

矩阵至少包含：`capability_id`、页面/控件、API 合同、后端 owner、预期持久化/Provider 结果、UI 回显、对象隔离策略、状态、证据和 `blocked_by`。

本文第 4 节是规划时最低矩阵，不是执行时可跳过重新盘点的静态白名单。前端继续实现后新增的真实能力必须自动纳入。

## 4. 最低真实能力矩阵

### 4.1 Shell、Scope 与 Notifications

- Bootstrap、Overview、Provider health 和 active Scope 的真实读取。
- 从前端切换 Operational Scope，验证后端 active identity、跨页面查询和刷新回显，再恢复原 Scope。
- Notification 列表、分页、context link 和 Notification SSE。
- 对本轮 `run_id` 产生的通知执行单项“标为已读”，刷新后仍为已读。
- “全部标为已读”只有在 preflight 证明运行前无未读通知，或当前合同能严格限定本轮对象时才执行；否则为 `NOT RUN`，理由为 `OBJECT_ISOLATION_UNAVAILABLE`，不得改写历史未读状态。

### 4.2 Overview、Infrastructure 与 Atlas

- Overview 真实聚合、状态和跨域 context link。
- Infrastructure topology、资源列表、搜索、分页、详情和 events。
- Atlas Canvas 与 Structured View 使用同一真实 Kubernetes projection。
- 从 Overview、Infrastructure 或 Atlas 选择本轮 Scenario 对象，跳转到相关 telemetry、Alert、Incident 和 Agent 上下文，保持 resource identity 与绝对时间窗。

Three.js 非空只作功能断言，不做审美、像素差异或多 viewport 验收。

### 4.3 Monitoring

- Catalog 读取、真实查询创建、运行状态、结果和历史回显。
- 通过前端触发一次可取消查询并验证取消终态；若真实数据规模无法形成可取消窗口，记录实际限制，不通过直接 API 替代。
- 保存 Query Definition，刷新后仍可读取。
- 创建一次精确 Query Authorization，并通过前端撤销，验证终态。
- uPlot 必须展示真实结果且选择/Tooltip 不阻塞后续操作；不做视觉判断。

### 4.4 Logs 与 Traces

- Logs catalog、历史查询和 live 模式的真实后端链路。
- 从前端运行 Logs 查询、选择真实结果并保存 Evidence，刷新后从 owning subject 读取 Evidence。
- Traces catalog、真实 Search、Trace detail、Waterfall/Span 选择和历史。
- 从前端保存 Trace/Span Evidence，并验证 provenance、resource identity 和绝对时间窗。
- 从 Logs/Trace 进入 Agent Consultation，验证上下文没有被替换或丢失。

### 4.5 Alerts

所有 mutation 只作用于本轮 Scenario 生成的 Alert：

- 列表、筛选、详情、Inspector、实时状态和 context links。
- acknowledge 成功及刷新持久化。
- 创建 silence、观察生效、expire silence、观察终态。
- 从 Alert 创建本轮 Incident。
- 若本轮存在第二个适合对象，从 UI 验证 attach 到本轮 Incident；不得关联运行前 Incident。
- 从 Alert 启动真实 Investigation，并验证 Agent、Evidence 与 Alert/Incident 关联。

### 4.6 Agent 与 DeepSeek V4 Flash

- 一次真实 Investigation 和一次真实 Consultation。
- Consultation 至少发送一条初始消息和一条上下文相关追问，验证多轮上下文。
- Snapshot、SSE chunk、完成/取消终态、历史、刷新和直接进入。
- Knowledge Item 的创建、更新、禁用/启用与删除，全部使用带 `run_id` 的非敏感合成内容。
- Action Card/Operation Plan 的创建、精确 hash 授权和适用的本地执行链。

DeepSeek V4 Flash 同时接受技术链路与语义质量验收，不使用固定措辞断言。回答必须：

- 正确识别本轮 Scenario 的异常对象和主要症状。
- 引用真实 Metrics、Alert、Logs、Trace 或 Evidence，不编造事实、ID 或已执行动作。
- 分析与证据一致，建议具体、相关且可执行。
- 明确区分建议、计划、授权、执行和 Verification，不能把未执行事项表述为已完成。
- Consultation 追问保持上下文并回答到点。

空泛、答非所问、明显幻觉或越权声称即使 HTTP、SSE 和持久化正常也记为 `FAIL`。模型回答质量一般但事实正确、相关且合同完整，不因措辞风格被误判。

### 4.7 Incidents

只操作本轮新建 Incident：

- 列表、筛选、Inspector、详情、关系资源、Timeline、Evidence 与 SSE。
- 从 UI 启动 Investigation 并观察 durable Agent run。
- 对 immutable Remediation Plan 执行 approve/reject 所需的独立能力；不同决定使用不同本轮 Plan，不能改写旧对象。
- Recovery decision、Delivery/Verification 投影、ResolutionReport 和 Close 的完整合法生命周期。
- 刷新、重新进入和 Back/Forward 后仍展示权威终态。

### 4.8 DevOps 与本地 Scenario 写链路

DevOps 本轮核心只覆盖本地 Kubernetes Scenario：

```text
Provider read
-> Change/Candidate
-> immutable Operation Plan
-> exact Authorization
-> Execution
-> current Provider Verification
-> Deployment Baseline / Incident projection
```

必须从真实前端完成 Scenario 的计划、审查、精确授权、allowlisted Deployment scale、异步 Worker 执行和真实 Verification。`202 accepted` 不等于执行或验证成功。

GitHub、Argo、Registry、hosted workflow、PR、merge、sync、publish、sign、staging 和 production 的所有写操作均为 `NOT RUN`，即使现有凭据有效也不得执行。

GitHub/Argo 等外部 Provider 只读能力可以在以下条件同时满足时从前端验证：

- 现有配置和凭据仍有效。
- 全程不需要登录、重新授权、输入 token 或 Owner 介入。
- 只访问列表、详情、状态和安全链接，不产生外部副作用。

一旦出现登录、授权、凭据更新或人工确认要求，立即记录实际状态并继续其他能力，不等待 Owner。

### 4.9 Settings

- Settings、Storage Status、active revision、五个 Draft section、Provider health 和 Revision history 的真实读取。
- 每个独立 section 的本地校验、服务端 validate、Diff、确认和 apply 接线。
- 只需制造能够证明各 section 后端合同的一组最小 Revision，重复副作用去重。
- Provider test 使用当前可用本地 Provider；外部 Provider 仍遵守只读/无需人工边界。
- 创建一个带 `run_id` 的非敏感测试 Secret Version，验证 write-only、fingerprint、权限和无回显；允许保留该审计记录。
- 使用两个 Chromium Context 从真实 UI 制造一次 stale base revision/hash 冲突，验证 fail-closed、rebase/重试和最终回显。
- 开始前记录原 active revision/hash 和等价配置；结束前通过前端创建后续 Revision 恢复原等价有效配置。历史测试 Revision 允许保留。

若 `POST /api/v1/configuration-revisions` 的 atomic expected-active-revision compare-and-set 缺口仍存在，应按本方案授权修复后端、OpenAPI 和 typed client，并从原前端控件重跑；不能用前端 preflight 冒充原子保证。

## 5. 测试对象与数据边界

### 5.1 唯一运行身份

每轮生成唯一、可搜索且不含敏感信息的：

```text
run_id=ui-int-<UTC timestamp>-<short random suffix>
```

所有可命名输入在合同允许时包含 `run_id`。同时记录 Scenario ID、Scope ID、Alert ID、Incident ID、Agent run/consultation ID、Evidence ID、Plan/Card ID、Authorization ID、Execution ID、Verification ID、Configuration Revision ID 和 Secret Version ID。

### 5.2 允许写入的对象

- 本轮 Scenario 生成或明确关联的 Kubernetes、Alert、Incident、Agent、Evidence、Plan、Operation 和 Verification 对象。
- 本轮创建的 Monitoring definition/authorization、Knowledge Item、Settings Revision 和 Secret Version。
- 可恢复的 active Scope 与 active Configuration 状态。

### 5.3 禁止写入的对象

- 运行前已经存在的 Incident、Alert、Agent run、Operation、Plan、Evidence 和 Settings 历史。
- 无法证明属于当前 `run_id` 的任何业务对象。
- staging、production 或未经批准 Provider 中的对象。

运行前对象只允许读取和关联验证。若某个批量 UI 操作无法限定当前运行对象且会污染历史数据，该能力保持 `NOT RUN`，不得为了覆盖率越过对象边界。

### 5.4 允许保留的历史

本轮允许保留 Incident、Alert、Agent、Evidence、Plan、Authorization、Operation、Verification、Configuration Revision 和一个测试 Secret Version 的审计记录。结束时恢复的是安全运行状态和等价 active config，不要求把数据库恢复到零新增行。

不得执行 `local-reset`，不得为了清理测试痕迹回滚整个数据库，也不得删除或覆盖原有历史。

## 6. 无人值守运行与安全收口

### 6.1 运行时间

没有固定小时上限。执行持续到：

1. 范围内能力全部闭合；或
2. 剩余每个失败根因都已完成最多 3 轮修复、重建和重跑，且证据齐全。

普通失败不能让整轮提前结束。不依赖失败链路的能力继续执行。

### 6.2 无人工等待

夜间运行不得等待 Owner：

- 不请求 GitHub/Argo 登录、授权或 token。
- 不请求外部凭据、审批或产品决策。
- 需要人工介入的路径立即记为 `NOT RUN` 并继续。
- DeepSeek V4 Flash 使用当前已配置凭据，不输出或复制 secret。

### 6.3 必须全局停止的安全条件

只有以下情况停止产生新副作用，并立即进入安全收口：

- 无法确认真实写入目标属于本地固定 kind/Namespace/Scenario。
- 备份失败或无法验证。
- 可能损坏、覆盖或泄露现有数据。
- migration 需要破坏性变更或无法无损升级。
- Scenario cleanup 失败。
- write gate 无法关闭或 Worker scale RBAC 仍为 `yes`。

停止后仍须尽最大努力执行只读诊断、`scenario-down` 和安全状态核验，并把未执行项记为 `NOT RUN`、已失败项记为 `FAIL`。

### 6.4 最终安全状态

无论总体成功或失败，结束前必须证明：

```text
scenario_state=inactive
scenario_write_gate=false
scenario_runtime_resources=0
scenario_stale_firing_alerts=0
worker_scale_rbac=no
active_scope=pre_run_or_explicit_equivalent
active_configuration=pre_run_equivalent
```

随后从浏览器验证回到 Live Mode，本轮 runtime 对象不再显示为 active，保留的历史仍可读取。

## 7. 关键成功与保护分支

普通读取、搜索和查询至少验证真实成功路径。高风险写操作除成功路径外，还必须从前端验证以下适用保护：

- 重复提交或可见 Retry 使用同一幂等 identity，并返回同一 durable 结果。
- 相同 Idempotency-Key 不得接受不同 payload。
- 两个浏览器 Context 制造旧 `expected_version` 或 `expected_hash` 冲突，UI 必须 fail closed 并能刷新后恢复。
- write gate 关闭后，从前端发起的本地执行必须被后端拒绝且无 Provider effect。
- API/Worker 重启后，已接受任务、结果和历史仍可从 UI 恢复。
- SSE 断开/重连保留 cursor、去重、权威 resync 和 teardown，不制造请求风暴或重复 UI 状态。

这些失败分支仍必须从实际页面控件触发。浏览器 Network、后端日志、Kubernetes 和只读 MySQL 查询可以证明发生了什么，但直接调用 command API 不能替代前端入口。

若产品现有 UI 根本无法重放同一幂等 identity 或制造合同要求的保护分支，应先判断是缺失的用户能力、前端缺陷还是只需后端 contract test 的内部细节；不得伪造一次浏览器联调 `PASS`。

## 8. 根因定位、修复与重跑

### 8.1 归因规则

页面失败不自动等于前端根因：

- 没有发出请求、参数/identity 错误、按钮状态错误、回显错误或刷新丢失：前端。
- 请求正确，但 API、Worker、持久化或 Provider 结果错误：后端。
- 双方字段、状态或错误语义不一致：跨端合同问题，按实际需要同时修复。

后端日志、Kubernetes、只读 MySQL/API 查询和 trace/request identity 可用于定位与佐证，但只能依附于对应浏览器动作。

### 8.2 修复授权

允许直接修复：

- Vue、Router、Pinia、typed API client、SSE、状态同步和真实数据适配。
- Go API、Worker、async job、MySQL persistence 和本地 Provider adapter。
- OpenAPI、前端类型与双方 contract tests。
- 必要的 forward-only database migration。

不允许：

- 删除或弱化用户能力来规避失败。
- 用 fixture、假状态或前端成功提示掩盖后端问题。
- 删表、清库、`local-reset`、破坏性字段变更或无损性未知的 migration。
- 在没有 Owner 决策时重新定义产品语义。
- 为修复功能主动重排页面、改视觉语言或重新设计已验收布局。

若修复必须明显改变信息架构或整体布局，停止该项并如实记录。缺失入口、控件不可操作、遮挡导致无法点击或错误反馈阻塞流程属于功能缺陷，可以做必要的最小可见修复。

### 8.3 三轮收敛

每个独立根因最多执行 3 轮：

```text
定位根因
-> 修复对应端
-> 运行相关最小代码级回归
-> 重建受影响运行组件
-> 从原前端控件重跑完整链路
```

三轮后仍失败：保存 UI、Network、Console、request/trace ID、日志、数据库/Provider 状态和代码 identity，记为 `FAIL` 或 `BACKEND_GAP`，继续其他独立能力。

### 8.4 代码级验证边界

此前后端大部分功能已经验证，本轮不机械重跑后端全量测试：

- 前端修复：相关 lint、focused Vitest、typecheck 和必要 build。
- Go 修复：受影响 package tests 和合同测试。
- API/OpenAPI 修改：runtime route、OpenAPI、typed client 与 contract tests 同步验证。
- migration 修改：forward-only 升级、现有数据保留、schema identity 和相关 MySQL integration tests。
- 跨端修改：两端最小回归后重新构建两端。

全量 race、性能、soak、供应链、release suite 和多浏览器不默认运行。任何代码级测试都不能替代最终从原前端控件重跑。

## 9. 真实浏览器自动化交付物

### 9.1 与 fixture E2E 隔离

执行时建立可重复运行的真实联调套件，建议边界为：

```text
frontend/playwright.real-integration.config.ts
frontend/tests/real-integration/
scripts/run-real-ui-integration.sh
.cloudops/integration/<run_id>/
docs/evidence/frontend-backend-integration/<run_id>.md
```

实际路径可按执行时仓库结构做等价调整，但必须与现有 `frontend/tests/e2e/fixture-server.mjs` 和 presentation fixture suite 明确隔离。命名、配置和报告中必须包含 `real-integration`，不得让 fixture 结果混入。

### 9.2 自动化合同

- Chromium、单 worker、串行副作用链。
- 用户动作只通过 Playwright locator 点击、填写、选择和提交。
- 禁止在测试中用 `page.request`、Axios、`fetch` 或 shell `curl` 代替业务按钮。
- 允许只读探针核对后端、MySQL、Kubernetes 和 Provider 事实。
- 使用唯一 `run_id`、对象 ledger、能力 checkpoint 和失败续跑。
- 每项记录发起控件、request/trace identity、后端结果、durable identity、最终 UI assertion 和状态。
- Console/Page error、failed request 和非预期 HTTP response 作为功能证据采集。
- trace、Network log 和截图只在失败时保留；不得做截图审美或视觉 diff。
- 不运行 Firefox、WebKit、响应式矩阵或视觉快照。

脚本必须支持修复后的定向重跑和最终完整矩阵回归。定向重跑不能覆盖或删除之前失败证据，最终报告要能追溯每轮尝试。

### 9.3 可恢复性

每完成一个 capability 即原子写入 checkpoint。执行进程、端口转发或浏览器中断后，必须先重新核对：

- 当前代码/worktree identity 是否变化。
- 当前部署镜像、Helm revision 和 Pod readiness。
- `run_id`、Scenario ID 和已创建对象是否仍为同一轮。
- 上一步后端结果是否已经 durable 完成。

确认后才可从下一个未完成能力继续，不能盲目重放写操作。

## 10. 执行顺序

### 10.1 Phase 0：运行前事实与备份

1. 读取本文档和当前权威文件。
2. 记录 `git status --short --branch`、HEAD、diff、Node/Go/Chromium 版本。
3. 只读确认前端已实现；未完成则停止。
4. 运行 `make local-doctor`，记录 kind context、Namespace、release、schema、Provider 和 port 状态。
5. 若已有 active Scenario，先识别并安全收口，不能静默复用未知副作用状态。
6. 执行 `make local-backup`，记录并验证 checksummed backup identity。
7. 记录原 active Scope、configuration revision/hash、Provider states、未读通知数和现有对象边界。
8. 生成唯一 `run_id` 和能力 ledger。

### 10.2 Phase 1：当前源码全量重建部署

1. 使用 `make local-up` 从当前工作树重建 API、Worker、Migrate、Demo 和前端并部署 canonical kind release。
2. 运行 `make local-status`，核对 API ready、schema、Provider、storage 和 loopback。
3. 核对浏览器静态资源、部署镜像/Helm revision 与当前源码一致，不复用旧 Vite、旧 Pod 或旧镜像冒充当前运行时。
4. 建立稳定 loopback port-forward；转发失败与 release 失败必须分别诊断。

### 10.3 Phase 2：能力盘点与真实读链路

1. 生成第 3.2 节双向能力矩阵。
2. 从 Chromium 遍历全部公开路由、直接进入、刷新和关键 Back/Forward。
3. 验证 Bootstrap、Scope、Provider health、通知/SSE、Overview、Infrastructure/Atlas、Monitoring/Logs/Traces、Alerts/Incidents/Agent、DevOps 和 Settings 真实读取。
4. 验证现有有效 GitHub/Argo 只读入口；需要人工时立即 `NOT RUN`。
5. 缺失的承诺入口按前端缺陷修复后重跑。

### 10.4 Phase 3：创建本轮对象并完成成功写链

1. `make scenario-up`，记录唯一 Scenario identity 和 active write gate。
2. 从 UI 完成 Observe -> Detect -> Correlate -> Investigate -> Decide -> Authorize -> Act -> Verify -> Resolve。
3. 按第 4 节完成 Notifications、Monitoring、Logs/Traces Evidence、Alerts、Agent、Incidents、DevOps 和 Settings 的独立写能力。
4. 所有成功项验证 durable state 和刷新/重进 UI 回显。

### 10.5 Phase 4：关键保护分支

1. 从 UI 验证幂等重放/重复 payload 保护。
2. 使用两个 Browser Context 验证 version/hash/revision conflict。
3. 验证 SSE reconnect、dedupe 和权威 resync。
4. 通过安全 lifecycle 关闭 Scenario write gate 后，从 UI 验证未授权/失效执行没有 effect。
5. 重启 API/Worker，验证异步任务与持久历史仍可由 UI 恢复。

### 10.6 Phase 5：持续修复与定向回归

发现缺陷立即按第 8 节归因、修复、测试、重建并从原控件重跑。同一根因最多 3 轮；失败后继续不依赖能力。运行直到所有能力闭合或剩余项全部达到重试上限。

### 10.7 Phase 6：安全收口与最终完整回归

1. 恢复原等价 active Settings 和 active Scope。
2. 执行 `make scenario-down` 和 `make scenario-status`。
3. 核对 runtime 0、write gate false、RBAC no、stale firing 0、history retained。
4. 从 Chromium 验证 Live Mode、保留历史、原配置和 Provider 状态。
5. 在收口状态下执行一次完整真实浏览器功能矩阵回归；需要 active Scenario 的已通过副作用链不重新制造，改为验证其 retained durable result 和已保存证据。
6. 输出逐项状态和总体严格结论。

## 11. Git 与提交规则

- 运行前 dirty worktree 属于 Owner，必须记录并保留。
- 不使用 `git add .`，不暂存 `.playwright-cli/`、运行 artifact、secret、trace 或无关 dirty 文件。
- 每个闭合根因创建一个小型本地提交，按真实归属提交前端、后端或跨端合同修复。
- 提交前运行对应最小回归和 `git diff --check`。
- 可复用 real-integration harness 独立提交。
- 最终联调报告独立提交。
- 不 push、不创建 PR、不发布、不触发 hosted/staging/production。

建议提交消息：

```text
test(integration): add real browser capability matrix
fix(frontend): close <capability> integration defect
fix(backend): close <capability> integration defect
fix(contract): align <capability> UI and API semantics
docs(integration): record real browser integration results
```

## 12. 报告与证据

### 12.1 每项记录

| 字段 | 要求 |
| --- | --- |
| capability | 稳定能力 ID 与用户目的 |
| UI action | 路由、控件 accessible name 和实际输入 |
| request | method/path、status、request/trace ID；敏感值必须隐藏 |
| backend | API/Worker 业务终态与根因 owner |
| durable effect | MySQL public ID、Provider object/state 或明确无副作用 |
| UI result | 刷新/重进后的权威展示断言 |
| attempts | 初次运行与最多 3 轮修复记录 |
| status | `PASS`、`FAIL`、`NOT RUN` 或 `BACKEND_GAP` |
| blocked_by | 仅在适用时记录依赖或人工授权边界 |
| commit | 适用的本地修复 SHA |

### 12.2 最终状态模板

```text
REAL_BROWSER_UI_API_INTEGRATION=PASS|FAIL
IN_SCOPE_CAPABILITIES=<count>
CAPABILITIES_PASS=<count>
CAPABILITIES_FAIL=<count>
CAPABILITIES_NOT_RUN=<count>
CAPABILITIES_BACKEND_GAP=<count>
FRONTEND_BLOCKING_DEFECTS=<count>
BACKEND_BLOCKING_DEFECTS=<count>
BACKEND_GAP=<NONE|explicit capability ids>
LOCAL_SCENARIO_WRITE_PATH=PASS|FAIL|NOT RUN
DEEPSEEK_V4_FLASH_CHAIN=PASS|FAIL|NOT RUN
DEEPSEEK_V4_FLASH_SEMANTIC_QUALITY=PASS|FAIL|NOT RUN
SETTINGS_WRITE_CONFLICT_RESTORE=PASS|FAIL|NOT RUN
SSE_RECONNECT_RESYNC=PASS|FAIL|NOT RUN
PERSISTENCE_AFTER_RESTART=PASS|FAIL|NOT RUN
FINAL_SAFETY_CLEANUP=PASS|FAIL
EXTERNAL_PROVIDER_READS=PASS|FAIL|NOT RUN
GITHUB_ARGO_EXTERNAL_WRITES=NOT RUN
MULTI_BROWSER=NOT RUN
VISUAL_REVIEW=NOT RUN
FULL_BACKEND_REGRESSION=NOT RUN
PERFORMANCE_SOAK=NOT RUN
PUSH_PR_RELEASE_DEPLOY=NOT RUN
```

总体结果后必须列出所有非 `PASS` 项、真实原因、`blocked_by`、已尝试修复次数和下一步。不得只给汇总数字。

## 13. 完成定义

本轮只有两种诚实终态：

### 13.1 完整通过

- 全部范围内能力满足第 0.1 节完整链路。
- 前后端阻塞缺陷和 `BACKEND_GAP` 均为 0。
- DeepSeek V4 Flash 技术与语义质量通过。
- 本地真实写、保护分支、重启持久化和安全 cleanup 通过。
- 只有预先排除的外部写、人工授权、多浏览器、视觉、性能和发布项为 `NOT RUN`。

### 13.2 有失败结束

- 所有仍失败根因均已达到最多 3 轮修复重跑。
- 其他独立能力已继续并完成。
- 所有 `FAIL`、`NOT RUN` 和 `BACKEND_GAP` 有完整证据，未被提升为通过。
- 最终安全 cleanup 仍必须通过；若 cleanup 失败，总体必为 `FAIL` 并置于报告首位。

本方案追求的不是前端和后端各跑一套测试，而是让真实用户动作穿透当前完整系统，逐项证明后端功能在前端入口下真实可用，并把发现的两端缺陷收敛到不能再安全自动推进为止。
