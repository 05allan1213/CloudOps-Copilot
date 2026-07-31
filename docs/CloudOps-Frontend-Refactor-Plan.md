# CloudOps-Copilot 前端重构实施计划

> 文档状态：`APPROVED_FOR_LOCAL_IMPLEMENTATION`
>
> 生成日期：2026-07-30（Asia/Shanghai）
>
> Gate 4+ 实施节奏修订：2026-07-31（Asia/Shanghai），Owner 已确认并行实现、轻量阶段校验、最终真实联调与延后全面验证
>
> 前置基线：`main@1568a8198f525edcff4aac0f48c81d3ac055c2fb`，已提交全部授权的前置修复、测试、证据与本文
>
> 前置结论：`FRONTEND_PREWORK=PASS`、`OWNER_VISUAL_REVIEW=PASS`
>
> 当前授权：`FRONTEND_REFACTOR_PLAN_APPROVED=YES`、`LOCAL_GATE_COMMITS_AUTHORIZED=YES`；禁止外部写入
>
> 本文只定义实施顺序、边界和验收 Gate；批准不表示生产迁移已经完成。

## 0. 使用与批准规则

### 0.1 权威顺序

实施时按以下顺序解释前端要求：

1. 当前 Owner 指令。
2. `/home/monody/k8s/vue.md`。
3. `/home/monody/k8s/frontend-current-state-audit.md`，只负责当前事实和已确认缺陷，并在实施时复核 live worktree 漂移。
4. `/home/monody/k8s/CloudOps-前端详细设计决策补充与复核.md`，负责修正和补足详细设计。
5. 与以上来源一致的 `/home/monody/k8s/CloudOps-前端详细设计决策记录.md`。
6. `docs/evidence/frontend-redesign/prework/frontend-prework-final-report.md` 与同目录证据，负责更新技术选型和准入状态，不反向改写产品决策。
7. 本计划及其已批准修订，负责把以上来源映射为实施 Slice，不得覆盖更高来源。
8. `docs/CloudOps-Implementation-Spec.md`、Accepted ADR 与当前源码；冲突的旧前端条款必须在 Gate 0 对齐。

当前 `docs/CloudOps-Implementation-Spec.md` 仍保留 Element Plus、手机 Bottom Navigation、320px 手机工作流和旧浏览器矩阵，与已通过的较新前端方向冲突。Gate 0 必须先把这些条款对齐为 Nuxt UI 4、Tailwind CSS 4、桌面产品和本计划的验收矩阵；在此之前不得开始正式页面迁移。当前源码只证明现状，不把已确认缺陷升级为目标契约。

### 0.2 开始实施的必要条件

生成本文不等于授权实施。开始 Gate 0 前，Owner 必须明确给出：

```text
FRONTEND_REFACTOR_PLAN_APPROVED=YES
```

该批准已于 2026-07-30 当前执行指令中收到。以下仍作为批准前的历史边界保留：

- 不修改生产前端依赖、页面、样式或业务代码。
- 不修改 Go 后端、API、数据库、Provider、部署或真实业务状态。
- 不 commit、push、创建 PR、发布或执行外部写入。
- 不把 `OWNER_VISUAL_REVIEW=PASS` 解释成页面迁移或最终视觉验收已经通过。

### 0.3 实施与评审状态

每个 Slice 的技术验证只使用：

- `PASS`：该项在当前 Slice 的最终工作树或精确提交上实际通过。
- `FAIL`：已运行且行为错误、证据不满足或 Owner 不接受。
- `NOT RUN`：未执行、环境不支持、缺少隔离条件或不在该 Slice 范围。

每个页面决策、组件依赖、Token 例外、专业渲染器和前端 PR 还必须引用本文相关章节，并且只给出一个评审结论：

- `COMPLIANT`：实现和证据符合已批准计划。
- `APPROVED_DEVIATION`：写明原因、影响契约、替代验证和 Owner 明确批准。
- `REJECTED`：违反方向、遗漏能力或缺少所需证据；不得合并或进入下一 Slice。

计划修订必须保留历史。不得通过静默编辑计划，把实现偏差改写成原始要求。

### 0.4 阶段验证节奏（Owner 补充）

每个实施阶段完成后，只执行与该阶段变更面直接相关的关键、必要验证；不得把全量测试提前摊到每个阶段。阶段验证的目标是尽早发现当前 Slice 的阻塞回归，不是提前宣称整站通过。

- 根据实际变更选择 focused unit/model/route/API/SSE、局部 lint/typecheck、受影响页面的浏览器 smoke、专业渲染器性能或 bundle 检查；没有直接关系的工具和套件不运行。
- 依赖、lockfile、构建入口或安全配置发生变化时，才追加对应的构建、依赖审计或预算检查；共享 Router/API/SSE/Token 变化时，追加其直接消费者的最小回归检查。
- 浏览器矩阵按本阶段风险选择相关的 B* 项，不默认全跑 B1-B8；写链路仍受第 7 节隔离条件约束。
- 每个阶段报告必须列出实际执行的命令/工具、选择理由、结果和未执行项。未执行项统一记为 `NOT RUN`，不得用 fixture、截图或单个 API 200 替代必要证据。
- 在整个前端彻底重构完成前，不运行也不声称通过完整 lint、完整 unit、完整 E2E、全浏览器、全量可访问性/性能和整站只读集成套件；除依赖或构建入口变更确需确认外，也不运行整站 build。Gate 12B 才执行第 5.3 节的全量验证。

### 0.5 Gate 4+ 并行快速实施修订（2026-07-31 Owner 批准）

本节是 Gate 4 及之后实施节奏、分支所有权、阶段状态和停止条件的当前权威修订。它保留本文后续 Gate 的全部功能范围、文件边界、领域契约、真实数据要求、写链路隔离规则和最终验收清单，只取代以下旧规则：Gate 4-11 全局串行执行、每 Gate 完整浏览器/性能/真实联调退出条件、中间 Owner 视觉阻塞，以及把原 Gate 12 的实现清理与全面验证绑定为同一轮工作。后续各 Gate 中与本节冲突的“进入条件”“验证与浏览器”“退出 Gate”和 Owner 视觉要求保留为历史及 Gate 12B 验证清单，不再阻塞并行页面实现。

#### 0.5.1 产品范围不降级

“前端以展示为主”只用于降低实施期校验强度，不表示把产品改成静态只读 Demo。Gate 4-11 仍须完整实现本文定义的真实 API、SSE、URL/History、Inspector、Agent、Evidence、Approval、Delivery、Verification、写操作入口和错误/异步状态；不得删除能力、使用 fixture 代替生产实现、弱化类型或改变 Go/API/数据库/Provider/Kubernetes 语义。

#### 0.5.2 六条并行实施线

Gate 3 的最终提交 `0b1c6d5c518746d197712e6b6574228d07056471` 已满足所有并行线的共享基础进入条件。六条实施线必须从包含本修订和窗口提示词的同一个“Gate 4+ 并行实施基线”提交创建独立 branch + Git Worktree；不得让多个 Codex 窗口共享同一工作目录。

| 实施线 | Branch | Worktree | Gate 与顺序 | 真正依赖 |
| --- | --- | --- | --- | --- |
| Read-only | `frontend/g4-readonly` | `/home/monody/k8s/CloudOps-Copilot-g4` | Gate 4 | Gate 3 |
| Telemetry | `frontend/g5-g6-telemetry` | `/home/monody/k8s/CloudOps-Copilot-telemetry` | Gate 5 -> Gate 6 | Gate 6 复用 Gate 5 telemetry 基础 |
| Alerts | `frontend/g7-alerts` | `/home/monody/k8s/CloudOps-Copilot-alerts` | Gate 7 | Gate 3；跨 Incident/Agent 使用兼容链接 |
| Agent | `frontend/g8-agent` | `/home/monody/k8s/CloudOps-Copilot-agent` | Gate 8 | Gate 3；跨页面上下文在集成阶段闭合 |
| Incident/DevOps | `frontend/g9-g10-incident-devops` | `/home/monody/k8s/CloudOps-Copilot-incident` | Gate 9 -> Gate 10 | Incident 单一操作面先于 DevOps 去重 |
| Settings | `frontend/g11-settings` | `/home/monody/k8s/CloudOps-Copilot-settings` | Gate 11 | Gate 3 |

每条线内部按表中顺序实现；六条线之间不互相 merge/rebase，也不等待其他页面线的验证结果。跨线链接先保留现有兼容入口，在 Gate 12A 集成时 canonicalize。分支不得 push、创建 PR、发布或执行外部写入。

#### 0.5.3 页面实施期唯一最低校验

每条线只执行以下最低交付门槛，不得自行扩展为旧 Gate 的完整验证矩阵：

1. 对改动文件执行 targeted lint。
2. 执行与改动直接相关的 focused unit/route/API/SSE 测试。
3. 分支交付前执行一次 `npm run typecheck`。
4. 每个改动路由执行一次 Chromium 1440x900 Light smoke，只确认页面可进入、主要内容可渲染、一个核心交互可用且无阻塞 Console 错误。

Dark、多 viewport、zoom、Firefox/WebKit、性能、大数据、完整 lint/unit/build/E2E、真实前后端联调和证据截图矩阵在页面分支统一记为 `NOT RUN`，不得为了取得旧 Gate `PASS` 而运行。阶段状态只能写为：

```text
IMPLEMENTATION=COMPLETE
FOCUSED_SMOKE=PASS
FULL_VALIDATION=DEFERRED
```

页面分支不得提前声明旧 Gate 全量退出条件、`FRONTEND_MIGRATION=PASS` 或 release ready。每条线在自己的唯一 handoff 文件中记录 base/final SHA、改动文件、路由、实际命令、结果、共享变更请求和 `NOT RUN`，不建立截图/trace/performance 证据包。

#### 0.5.4 共享文件唯一 Owner

以下文件或目录由集成工作树唯一拥有，页面分支不得直接修改：

```text
docs/CloudOps-Frontend-Refactor-Plan.md
frontend/package.json
frontend/package-lock.json
frontend/components.d.ts
frontend/src/styles/tokens.css
frontend/src/api/client.ts
frontend/src/components/workspace/
frontend/src/composables/useWorkspace*
```

`frontend/src/router/routes.ts` 只允许 Read-only 线为 additive `/atlas` 及 legacy Atlas Query 修改；其他线不得修改。`frontend/src/api/platform.ts` 由 Settings 线唯一拥有，Read-only 线只能消费现有 typed client；若确需共享变更，必须在 handoff 中记录，不得跨线抢改。新 specialist adapter 和领域内组件属于对应实施线。

确需修改其他共享基础时，页面线创建一个与页面提交分离的“shared change request”提交并在 handoff 标记；集成窗口可以采纳、重做或拒绝。`frontend/components.d.ts` 在 Gate 12A 统一重新生成。

#### 0.5.5 本地提交与集成协议

- 页面线按页面或逻辑 Gate 创建小型本地提交，保留干净工作树后交付；不修改其他实施线代码。
- 每条线只有在 `IMPLEMENTATION=COMPLETE`、`FOCUSED_SMOKE=PASS` 且工作树干净时才能交付。
- 集成工作树使用 `git merge --no-ff` 合并完整实施线；冲突只由集成窗口处理。
- 每合并一条线只执行一次 `npm run typecheck`；全部合并后执行一次全路由 Chromium 1440x900 Light smoke，不提前运行全面套件。
- 当前主 Worktree `/home/monody/k8s/CloudOps-Copilot` 只负责计划、合并、共享文件、Gate 12A、真实联调和最终 Owner 预览，不承担任一页面线的日常实现。

#### 0.5.6 Gate 12A 与 Gate 12B

原 Gate 12 拆分为两个独立阶段：

**Gate 12A（本轮必须完成）**

- 合并六条实施线并闭合跨页面链接、共享文件请求和 route ownership。
- 删除 Element Plus、旧样式、遗留导航、无消费者代码和双体系残留；重新生成声明。
- 因依赖与构建入口发生变化，只运行 `npm run typecheck`、`npm run build`、零残留扫描和全路由 Chromium 1440x900 Light smoke。
- 启动真实前端和后端，对所有公开路由执行真实 UI -> API -> Provider 只读联调；完善真实字段、Loading、Empty、Error、Partial、跨页面上下文和非预期 Console/Network 问题。
- 联调发现的前端缺陷必须修复并提交。必要后端契约缺口只记录 `BACKEND_GAP`，不得越权修改后端。
- 真实写链路只有在隔离目标、受限凭据、初始 identity/hash、cleanup 和单独 Owner 授权全部存在时运行；否则保持 `NOT RUN`，不阻塞前端实现完成。

**Gate 12B（后续单独授权）**

- 执行第 5.3 节完整 lint/unit/build/audit/E2E，以及 B1-B8、Light/Dark、多 viewport、Firefox/WebKit、可访问性、性能、大数据、SSE soak 和完整发布就绪验证。
- 只有 Gate 12B 才能把旧 Gate 的完整退出条件、`FRONTEND_MIGRATION` 和 `FRONTEND_RELEASE_READY` 判为 `PASS`/`YES`。

#### 0.5.7 Owner 视觉与本轮停止条件

Gate 4、8、9、11 的中间 Owner 视觉 Gate 不再阻塞实现。AI 浏览器 smoke 只能判断空白、重叠、裁切、交互和 Console/Network 等技术问题，不能替 Owner 判断审美。Gate 12A 真实联调完成后，集成窗口启动可访问整站并提供 URL；只有 Owner 本人可以给出：

```text
OWNER_FINAL_VISUAL_ACCEPTED=YES
```

本轮只有在六条线合并、Gate 12A 清理完成、真实前后端启动、所有公开路由读取和主要跨页面路径可用、阻塞 Console/Network 问题修复、写链路如实分类且 Owner 最终视觉接受后停止。最终状态为：

```text
FRONTEND_IMPLEMENTATION=COMPLETE
REAL_READONLY_INTEGRATION=PASS
OWNER_FINAL_VISUAL_ACCEPTED=YES
FULL_VALIDATION=DEFERRED
FRONTEND_MIGRATION=PENDING_FULL_VALIDATION
FRONTEND_RELEASE_READY=NOT_ASSESSED
```

若真实 Provider 或必要后端事实不可用，受影响项必须为 `NOT RUN` 或 `BACKEND_GAP`，不得伪造上述 `PASS`。六个窗口的版本化执行提示词和集成交接规范位于 `docs/evidence/frontend-redesign/implementation/parallel-gate-04-11/`。

## 1. 目标、范围与非目标

### 1.1 最终目标

把当前 Vue 3 前端迁移为中文优先、紧凑、低噪声、高信息密度的桌面 CloudOps 运维 Agent 工作台，同时保持真实 Provider 数据、URL、API、SSE、Evidence、Approval、Delivery 和 Verification 契约。

最终状态必须同时满足：

- Vue 3、Vite、TypeScript、Vue Router 和 Pinia 继续作为应用基础。
- Nuxt UI 4.10.0 是唯一通用 UI 体系，Tailwind CSS 4.3.3 是统一 CSS 系统。
- Lucide 是唯一可见图标体系；无 emoji、Element Plus Icons、手绘 SVG 或混合图标风格。
- uPlot 1.6.32 只负责 Monitoring；Three.js 0.185.1 只负责 Atlas；TanStack Vue Virtual 3.13.35 只负责大数据虚拟化。
- Trace 保留当前语义渲染器并增加虚拟化，不引入第二套通用 UI 或未经证明的 Trace 框架。
- 只有一个 CloudOps Token 管线，Light/Dark 具有等价层级、密度、Focus 和状态含义。
- 十个主 Workspace、新增的 `/atlas` 专业视图、详情路由、404、Legacy Query 和浏览器历史契约均无静默遗漏。
- Incident 是事故相关 Approval、Delivery、Verification 的唯一主操作面；DevOps 只保留全局队列、非事故操作、技术明细和兼容跳转。
- Overview 成为 Operations Agent Command Center，只允许 Scope-bound 只读调查入口，不新增 Approval、执行、配置或回滚入口。
- 不建设手机产品；Shell 迁移时删除 Bottom Navigation，但桌面路由能力保持完整。

### 1.2 本计划允许的实施范围

- `frontend/` 下的生产依赖、Vite 配置、Vue 应用、路由、状态、样式和测试。
- `.github/workflows/ci.yaml`、`Makefile` 中与前端静态检查、构建、浏览器测试和预算直接相关的最小改动。
- `docs/CloudOps-Implementation-Spec.md` 中冲突的前端权威条款。
- `docs/evidence/frontend-redesign/implementation/` 下当前 Slice 的证据。
- 为保护现有契约而新增或修改的前端单元、组件、路由和 Playwright 测试。

### 1.3 非目标与硬边界

- 不为前端便利改变 Go API、数据库、领域状态、Provider、Kubernetes 或部署语义。
- 不创建新认证、权限或后端 Draft 模型。
- 不把 fixture、静态截图、组件 Demo 或接口存在性当成真实集成证明。
- 不删除或弱化当前用户能力来适配组件库。
- 不引入第二套通用 UI 库，不复制 Nuxt UI 源码，不自绘成熟基础控件。
- 不进行无关主版本升级、批量格式化、全仓目录重排或历史文档清理。
- 不自动 commit、push、创建 PR、合并、发布或执行真实业务写入；这些动作需要当前 Owner 的单独授权。

如果实现证明后端缺少必要契约，记录 `BACKEND_GAP`、可复现证据、用户影响和最小接口要求，然后停止受影响 Slice。不得在前端推断或伪造后端事实。

## 2. 当前基线与锁定技术决策

### 2.1 前置 Gate

```text
TECHNICAL_PREWORK_A_TO_G=PASS
OWNER_VISUAL_REVIEW=PASS
WRITE_PATH_E2E=NOT RUN
FRONTEND_PREWORK=PASS
READY_FOR_IMPLEMENTATION_PLAN_GENERATION=YES
```

前置证据的版本化入口是：

- `docs/evidence/frontend-redesign/prework/frontend-prework-final-report.md`
- `docs/evidence/frontend-redesign/prework/capability-contract-map.md`
- `docs/evidence/frontend-redesign/prework/authority-conflict-ledger.md`
- `docs/evidence/frontend-redesign/prework/general-ui-prototype.md`
- `docs/evidence/frontend-redesign/prework/design-system-prototype.md`
- `docs/evidence/frontend-redesign/prework/specialist-renderers.md`
- `docs/evidence/frontend-redesign/prework/interaction-prototypes.md`

隔离原型是选型证据，不是生产依赖或可直接整目录复制的实现。

`CloudOps-前端详细设计决策补充与复核.md` 第 15 节的 `NOT_READY_FOR_IMPLEMENTATION_PLANNING` 是 2026-07-30 前置验证完成前的历史状态。后续当前 SHA 前置报告已闭合其中列出的 Nuxt UI、Token、Incident ownership、URL/Inspector、SSE、Settings、专业渲染、数据规模、能力映射和 Owner 视觉 Gate，因此技术准入状态以本节当前 `FRONTEND_PREWORK=PASS` 为准；补充文档的产品修订仍全部有效。

### 2.2 依赖与所有权矩阵

| 能力 | 当前生产状态 | 锁定目标 | 实施规则 |
| --- | --- | --- | --- |
| Vue | 3.5.x | 保留 | 不在本重构中做无关升级 |
| Vue Router | 4.6.x | 保留 | 所有 route 继续 dynamic import |
| Pinia | 3.0.x | 保留 | 只承载跨路由 transient state，不取代 URL |
| Vite | 7.x | 先保留并实测 | 若与 Nuxt UI 4.10.0 不兼容，Gate 1 `FAIL`，批准最小工具链变更后再继续 |
| TypeScript / vue-tsc | 当前严格源码检查 | 保留 | `skipLibCheck: true` 已存在；不得扩大跳过范围或降低源码严格性 |
| 通用 UI | Element Plus 2.14 | `@nuxt/ui` 4.10.0 | 仅迁移期共存，最终完全删除 Element Plus |
| CSS | Sass/CSS 与页面局部覆盖 | Tailwind CSS 4.3.3 + CloudOps canonical CSS variables | 所有映射回到单一 Token 源 |
| 图标 | Lucide + Element Plus 内部能力 | Lucide only | Nuxt UI 只允许 `i-lucide-*` |
| Monitoring | 当前手写 SVG | uPlot 1.6.32 | 路由级懒加载并提供同步表格/键盘路径 |
| Trace | 当前语义实现 | 保留并虚拟化 | 不替换领域语义，不新增通用 UI |
| Atlas | Three.js 0.185.1 | 保留 | 懒加载、结构化等价路径、资源释放和隐藏页暂停 |
| 大数据列表 | 页面自有渲染 | `@tanstack/vue-virtual` 3.13.35 | 仅用于 Logs、Span、长 Timeline 和大型表格边界 |

初次生产接入使用前置原型验证过的精确版本。任何版本变化都必须重新核对官方文档、lockfile、类型、构建、浏览器和 bundle，并按本计划给出 `COMPLIANT` 或 `APPROVED_DEVIATION`。

### 2.3 已知基线风险

- Nuxt UI 4.10 standalone Vue 声明会暴露 Nuxt 内部 `#build` 类型；在当前生产工具链重新验证，不能通过新增源码排除或 `any` 掩盖。
- 当前 lint 实际为 2,564 warnings，而 `lint:no-new-warnings` 上限仍为 2,608。Gate 1 将上限收紧到当前实际值；之后只允许下降。
- Alert 详情仍可能发出 legacy `workload`，而 Logs/Traces 使用 canonical `resource`。
- Incident 排序仍是组件本地状态，刷新和 Back/Forward 不能完整恢复。
- Incident finite-stream 首次回放可能造成投影读取突发，需要按 resource/cursor 合并并提供有界背压。
- Atlas 仍需在生产实现上做长时 GPU/内存生命周期证据。
- 写链路因缺少隔离身份、目标和清理证明保持 `NOT RUN`。

### 2.4 详细设计决策追踪矩阵

本节是实施完整性的规范索引。`保留` 表示按原详细记录实现；`复核后实现` 表示以补充与复核的修订语义实现；`高位取代` 表示原描述与 `vue.md` 冲突，明确实现其高优先级替代方案，而不是机械照搬。像素值是已确认的设计起点和软目标；若在受支持桌面视口、缩放、长内容或可访问性验证中失败，可以在不改变可见产品行为的前提下调整，并记录证据。

#### D-01 至 D-34

| 决策 | 最终实施语义 | 处置 | 实施 Gate |
| --- | --- | --- | --- |
| D-01 | Sidebar + 主内容两列基础，页面按需右推 Inspector | 保留 | 2、3、各 Workspace |
| D-02 | Scope/集群选择器只在 Sidebar 专属行，Header 不重复 | 保留 | 2 |
| D-03 | Agent 从普通导航组移出，固定在 Sidebar 底部；完整 `/agent` 保留 | 保留 | 2、8 |
| D-04 | 快速查看使用 Inspector；复杂调查、多步骤和危险操作进入完整工作页 | 复核后实现，合并 FR-SUP-004/FR-CX-001 | 3、7、9、10 |
| D-05 | 无 Tab 页面使用单行 Toolbar；有 Tab 页面为 Tab 行 + Toolbar 行 | 保留 | 3、4-11 |
| D-06 | 220px Sidebar、64px rail、Inspector 软上限 520px、文字密集区软上限 960px；宽屏扩展主内容 | 保留为浏览器可调软目标 | 2、3、4-11 |
| D-07 | 平衡高密度；表格行高约 48px、正文 13-14px、辅助 12px、区域标题 15-16px | 保留为浏览器可调软目标 | 1、3、4-11 |
| D-08 | 连续画布、1px 克制边框、页面面板无阴影、最多两层、4/6/8px 级圆角；强阴影只给 Overlay | 保留 | 1、3、4-11 |
| D-09 | 严重性用左侧 3px 语义色条 + 文字 Badge，行背景保持中性 | 保留 | 3、7、9 |
| D-10 | Loading/Empty/Error/Partial/Stale 使用低噪声内联表达 | 复核后实现；Skeleton 仅限首次加载，合并 FR-CX-004 | 3、4-11 |
| D-11 | 列表可用相对时间，标识使用 mono，数值使用 tabular numerals | 复核后实现；审计关键位置直接显示 UTC，合并 FR-CX-003 | 1、3、4-11 |
| D-12 | `/overview` 首屏由运维态势主导，Atlas 是真实预览而非首页主体 | 保留 | 4 |
| D-13 | 首页只承担态势与引导，Approval/Delivery/Verification 跳 Incident | 复核后实现；额外允许 FR-SUP-006 的 Scope-bound 只读调查入口 | 4、8、9 |
| D-14 | 首页 Incident 点击进入 `/incidents` 并打开对应 Inspector | 保留，使用 history/selection 兼容规则 | 4、9 |
| D-15 | 未关联 Alert 在首页作为次级列表；无活跃 Incident 时扩展占位 | 保留 | 4、7 |
| D-16 | 首页 Agent 摘要显示结论、Evidence、置信边界和待处理项，只提供上下文跳转 | 复核后实现；不得在首页直接审批 | 4、8、9 |
| D-17 | 无活跃事件时显示健康、近期解决、上次调查，并扩展 Atlas 与交付验证摘要 | 保留，禁止退化为空卡片墙 | 4 |
| D-18 | 主 CTA 在 Toolbar 右侧；危险操作只在行级菜单或完整页 | 复核后实现；确认强度按 FR-SUP-007 的真实后果分级 | 3、7、9-11 |
| D-19 | URL 保存筛选、排序、分页、时间、稳定 Tab 和选中对象 | 复核后实现；合并 FR-SUP-003/FR-CX-002 的 replace/push 与旧链接兼容 | 3、4-11 |
| D-20 | 多栏整栏折叠、不拖拽、折叠偏好不写 URL | 复核后实现；窄桌面按 FR-SUP-002 主任务优先渐进收起 | 2、4、8 |
| D-21 | Agent 入口优先级：上下文触发 > 结构化新建 > 自由查询，三者都保留 | 保留 | 4、7-9 |
| D-22 | Agent 为 History/Conversation/Inspector 三栏，Inspector 随选择变化 | 复核后实现；列宽是软目标，主任务在 1024/缩放下仍可用 | 8 |
| D-23 | Incident 详情为单列阶段流 + 可深链接 ZoneNav | 保留 | 9 |
| D-24 | `/overview` 改为 Command Center，新增 lazy `/atlas`；旧 Atlas Query 兼容跳转 | 复核后实现；新增路径只能做加法，不破坏现有链接 | 3、4 |
| D-25 | Atlas 节点选择右推 Inspector，画布同步 resize，关闭后恢复 | 保留；具体宽度按实测调整 | 4 |
| D-26 | Alerts 与 Incidents 共享列表/Inspector 规律；Ack/Silence 可在 Alert Inspector | 复核后实现；按 FR-SUP-007 分级确认 | 7 |
| D-27 | Infrastructure 使用资源类型 Tab + 表格 + Inspector，无独立资源详情页 | 保留 | 4 |
| D-28 | Logs 顶部查询区 + 虚拟列表 + Log Inspector + Evidence 提交 | 复核后实现；合并 FR-CX-006 长内容规则 | 6 |
| D-29 | Settings 分区表单、左对齐 960px 软上限、无 Inspector、Revision History 为区段 | 复核后实现；合并 FR-SUP-008 | 11 |
| D-30 | Monitoring 图表优先，图表约 440px、同步表格首屏可见 | 保留为浏览器可调软目标 | 5 |
| D-31 | Trace 选择进入全宽详情模式，内部 Span Inspector | 复核后实现；保留现有 `trace_id` Query 为 canonical 兼容入口 | 6 |
| D-32 | DevOps 先显示压缩 Inspector，再进入完整详情；危险操作只在完整详情 | 复核后实现；保留 `view/subject/operation` Query | 10 |
| D-33 | 新行由聚合提示条交给用户加载；现有状态就地更新、无布局跳动；reduced motion 立即切换 | 复核后实现；叠加 FR-SUP-005 的断流和可信度规则 | 3、7-10 |
| D-34 | Header 用 Lucide `Bolt` + `X/Y` 表示 Provider 健康，hover/focus 展开，点击 `#providers` | 复核后实现；原文 emoji 只表意，不进入 UI | 2、11 |

#### FR-SUP-001 至 FR-SUP-010

| 决策 | 必须实现的结果 | 实施 Gate |
| --- | --- | --- |
| FR-SUP-001 | Incident 是事故 Approval/Delivery/Verification 唯一主操作面；DevOps 保留全局/非事故能力和兼容跳转 | 9、10 |
| FR-SUP-002 | 1920/1440 主体验，1280/1024/缩放渐进收起辅助区；无手机产品和主页面横向滚动 | 2、4、8、12 |
| FR-SUP-003 | 选择可分享/刷新恢复，快速扫描 replace history，Back 关闭详情并恢复列表上下文 | 3、4、7、9、10 |
| FR-SUP-004 | 快速 Inspector + 完整工作页边界保持一致 | 3、7、9、10 |
| FR-SUP-005 | 重连不移动阅读位置；无法保证完整性时停止显示 live，并明确 stale/disconnected | 3、7-10 |
| FR-SUP-006 | Overview 可发起带 Scope 的只读调查，不审批、执行、配置或回滚 | 4、8 |
| FR-SUP-007 | acknowledgement、配置、审批、回滚、强制终止使用不同强度的确认 | 3、7、9-11 |
| FR-SUP-008 | Settings 分区 frontend draft、校验、变更摘要、显式 apply、并发 revision、部分结果和离开保护 | 11 |
| FR-SUP-009 | 复杂表格关键列始终可见；次要列按页面本地记忆且不改变分享 URL | 3、4-11 |
| FR-SUP-010 | Atlas 本轮不提供内置图片导出，优先性能、深链接和 structured path | 4 |

#### FR-CX-001 至 FR-CX-008

| 决策 | 必须实现的结果 | 实施 Gate |
| --- | --- | --- |
| FR-CX-001 | Inspector entry/restore Focus、topmost Escape、未应用编辑保护、scroll/context 保留、失效/删除/无权状态 | 3、4、7、9、10 |
| FR-CX-002 | 可恢复 URL 状态与本地临时状态分离；现有 Alert/Incident path、Trace/DevOps Query 和旧 Atlas Query 兼容 | 3、4、6、7、9、10 |
| FR-CX-003 | 列表相对时间可访问；审计关键位置直接显示精确 UTC，并可无歧义复制 | 3、4-11 |
| FR-CX-004 | 首次 Skeleton、按钮内提交、保留内容的后台刷新、长操作阶段/耗时/可用取消、失败保留输入 | 3、4-11 |
| FR-CX-005 | 不创建通用后端状态机；忠实区分 permission、expired、hash changed、partial、Provider disagreement、accepted/observed/verified | 3、4-11 |
| FR-CX-006 | 原始日志、wrap toggle、完整复制、长标识查看/复制、虚拟化、旧请求取消、Timeline/Evidence 顺序与锚点不变 | 3、6、9 |
| FR-CX-007 | 键盘/屏幕阅读器等价、状态非颜色单一表达、reduced motion、经过安全校验且上下文明确的 Provider link | 1、3、4-12 |
| FR-CX-008 | 按真实 UI/API/SSE 建立能力映射；每项只能保留、迁移、兼容替代或获批废弃；Bug 不作契约 | 0、每个 Slice、12 |

#### 被高优先级来源取代的原始描述

| 原始描述 | 最终规则 |
| --- | --- |
| `<768px` Bottom Navigation、手机 Drawer 和手机工作流 | 由 `vue.md` 取代；Gate 2 删除，目标只覆盖桌面与缩放降级 |
| Shell 示例中的铃铛、闪电、太阳/月亮、用户和 Agent emoji | 只保留语义，全部用 Lucide 与文本实现，UI 零 emoji |
| 所有 Loading 一律 Skeleton、不使用 spinner | 由 FR-CX-004 取代；首次内容用 Skeleton，按钮提交用控件内进度，后台刷新保留内容 |
| 所有实体统一新增 `/:entityId` | 由 FR-CX-002 取代；保留既有 path/Query，新路径只能加法兼容 |
| 相对时间只通过鼠标 hover 提供精确值 | 由 FR-CX-003 取代；键盘可访问，审计关键位置直接显示 UTC |
| Atlas 图片导出待定 | 由 FR-SUP-010 关闭；本轮明确不提供内置导出 |
| Atlas 标准快照、跨设备同步列偏好、完整命名保存视图 | 明确留给后续产品决定；本轮不实现，不以半成品入口占位 |
| shared/LAN/multi-user 登录与 route guard | Local Owner 范围不推导该能力；部署边界变化时另立安全 ADR |

每个 Slice 报告必须列出本次覆盖的 `D-*`、`FR-SUP-*` 和 `FR-CX-*`，并给出实际 route/状态/测试/浏览器证据。只在矩阵中出现、不在生产 UI 可达或无验证的决定不能计为实现完成。Gate 12 必须对两份设计文档与本计划执行 ID 差集检查，结果为零后才能声明详细设计覆盖 `PASS`。

## 3. 必须保护的契约

### 3.1 路由、URL 与历史

保留以下全部现有公开路径并新增 lazy `/atlas`；新增路径不得改变现有路径含义：

```text
/
/overview
/atlas
/infrastructure
/monitoring
/alerts
/alerts/:alertId
/logs
/traces
/agent
/incidents
/incidents/:incidentId
/devops
/settings
/:pathMatch(.*)*
```

保护 `docs/evidence/frontend-redesign/prework/capability-contract-map.md` 中逐路由记录的 Query、hash、cursor、分页、时间范围、稳定 Tab、selected entity、legacy link、刷新恢复和 Back/Forward 行为。`/atlas` 是加法视图；现有 `/overview` 中用于 Atlas 的 `view/resource` 链接必须兼容解析并使用 history replace 归一化到 `/atlas`。快速 Inspector 选择使用 history replace；进入完整详情使用 push；关闭后恢复筛选、分页、滚动和触发元素 Focus。

### 3.2 API、SSE 与错误身份

- 组件不得维护第二套 DTO、拼接 Provider authority 或改变 HTTP/SSE 语义。
- 保留 `ApiError` 的 status、code、request ID、trace ID、idempotent replay 和 next steps。
- 保留请求取消、旧响应抑制、SSE cursor、去重、重连、resync、teardown 和 Notification/Agent 独立生命周期。
- 明确显示 connecting、live、reconnecting、disconnected、stale、cursor expired、resync failed；连续性不可证明时停止显示 live。
- 当前缺陷可以修复，但必须用契约测试证明 URL、API 和 SSE 行为未被弱化。

### 3.3 领域与安全真相

- Provider-backed 数据、Provider identity、Partial/Stale/Unavailable/Forbidden/Error 不得被 mock 或乐观状态掩盖。
- Evidence provenance、exact authority、Approval exact hash、Delivery 和 Verification 使用后端真相。
- accepted、dispatched、observed、verified 必须保持不同；只有当前 Verification 与 Evidence 可以支持 verified success。
- Permission Denied、expired authority、exact hash/version changed、Provider disagreement、partial result、accepted-but-not-observed、observed-but-not-verified、Verification Failed 必须可区分且不只依赖颜色。
- Evidence、Approval、Authorization、Delivery、Verification、Revision History 和危险操作结果直接显示精确 UTC 时间。
- 所有外部链接继续经过统一 HTTP/HTTPS 校验，显示明确目标，使用 `target="_blank" rel="noopener noreferrer"`。

### 3.4 产品责任边界

- Incident：事故相关调查、Evidence、Recovery Decision、Approval、Delivery、Verification 和 Resolution 的主操作面。
- DevOps：全局队列、非事故 Operation、Identity/Authority、技术明细，以及跳入 Incident 对应阶段的兼容链接；不保留重复事故写入口。
- Overview：Scope-bound 态势与只读调查入口；不执行审批、配置、交付或回滚。
- Agent：始终携带 Incident、Alert、Service、Scope、时间范围和 Evidence 上下文，不退化为无上下文聊天。
- Settings：前端分区 draft、校验、显式 apply、逐项结果、revision conflict、retry 和 leave protection；不伪造后端 Draft 或原子成功。

### 3.5 可见界面与状态编排合同

- Shell：展开 Sidebar 以 220px 为起点、rail 以 64px 为起点；品牌、Scope、分组导航、Agent pin 和 Local Owner/footer 各自只有一个明确位置。Header 只放 breadcrumb、Provider health、Live/Scenario、Notification、Theme 和 Owner identity，不重复 Scope 或 Agent。
- Toolbar：无 Tab 页面为 H1/筛选/搜索/次要动作/右侧主 CTA；有 Tab 页面为独立 Tab 行加 Toolbar 行。危险操作不占主 CTA。
- Inspector：目标约 460px、软上限 520px；打开、切换、关闭、完整页跳转、失效目标、Back/Forward 和 Focus/scroll 恢复使用同一合同。窄桌面先收起辅助区，不让主任务产生页面级横向滚动。
- 多栏：整栏收为约 24px 可恢复边条，不支持拖拽；折叠属于当前页面 local state，不进入分享 URL。1024/缩放下主工作区优先，其次按业务顺序收起辅助栏。
- 表格：行高约 48px；关键列固定且始终可见，次要列可选择并按页面本地记忆；列偏好不改变 URL。Severity 使用左侧语义色条 + 文本 Badge，不染整行背景。
- 长内容：Log 默认保留原始行结构和 bounded horizontal scroll，并有 wrap toggle；Hash、命令、JSON、资源标识保持结构、完整查看和一键复制。截断只影响显示，不影响复制值。
- 状态反馈：首次内容加载可用 Skeleton；按钮提交使用按钮内进度与防重复提交；后台刷新保留内容；长操作显示阶段、耗时和契约支持的取消；失败保留输入与已加载上下文。
- 实时更新：新行使用节流聚合提示，由用户决定加载；现有对象就地更新且不移动当前阅读位置。默认状态过渡约 250ms、短暂强调最多约 1s、Atlas 节点约 300ms，均由 Motion Token 管理；`prefers-reduced-motion` 立即呈现最终状态。
- 风险确认：低影响 acknowledgement、可逆配置、精确 Approval、rollback 和 forced termination 使用逐级增强且不同的确认内容。确认必须包含适用的 target、effect、authority、hash/version、不可逆后果和恢复限制，不能复用一个空泛通用弹窗。
- 排版：中文文案优先并遵守统一术语；Hash/IP/Kubernetes ID/port/version/JSON/command/log 使用 mono，运营数值用 tabular numerals。扫描列表可显示相对时间；审计关键区直接显示精确 UTC。
- 视觉：连续画布优先，页面内容最多画布/面板两层；面板使用克制边框和小圆角且无阴影，阴影只给 Overlay。禁止 Hero、卡片墙、嵌套卡片、装饰渐变、玻璃、霓虹、粒子、过大标题和无语义胶囊。
- 状态与安全语义不由 Design System 重命名。展示类别只统一视觉，页面继续渲染真实 Alert、Incident、Evidence、Approval、Delivery、Verification、Operation 和 Revision 状态。

### 3.6 Provider Context Link 矩阵

| 来源 | 目标与携带上下文 | 实施 Gate |
| --- | --- | --- |
| Incident / Evidence | GitHub Repository、PR、Commit、Workflow；使用后端可信 identity/SHA | 9 |
| DevOps | GitHub Actions、Argo CD；携带 operation/pipeline/deployment identity | 10 |
| Monitoring | Grafana 或实际 Monitoring Provider；携带 query/resource/time | 5 |
| Logs Inspector | Kibana 或实际 Logs Provider；携带 query/resource/time/trace | 6 |
| Trace / Span | Tempo 或实际 Trace Provider；携带 trace/span/resource/time | 6 |
| Delivery / Verification | Argo Rollouts/Argo CD 或实际 Provider；携带 deployment/version/verification identity | 9、10 |

所有链接必须来自可信后端投影或统一 allowlist 校验，只允许批准的 HTTP/HTTPS 目标；页面显示目标系统、对象与将携带的上下文，不显示裸 URL，不用 iframe。新窗口使用安全 `rel`；返回 CloudOps 后的 Focus 和上下文仍可理解。

## 4. 迁移架构与过渡规则

### 4.1 路由级单一 UI 所有权

迁移期间允许两个依赖暂时存在于同一构建，但必须满足：

1. 已迁移 Shell 使用 Nuxt UI；未迁移 Workspace 可继续使用原 Element Plus 实现。
2. 一个已迁移 Workspace 的页面树内只使用 Nuxt UI 和批准的专业渲染器，不 import、注册或渲染 Element Plus 通用控件。
3. 未迁移 Workspace 不接受新的 Element Plus 控件、图标、主题映射或页面级覆盖。
4. Nuxt UI 与 Element Plus 不在同一个业务组合组件中互相嵌套；过渡边界只在 Shell 的 RouterView 所有权处。
5. 共享状态、Router、API、SSE 和领域模型保持一份；不得为迁移页面复制 Store 或 API client。
6. 每个 route 在 Slice 报告中登记 `LEGACY_ELEMENT_PLUS` 或 `MIGRATED_NUXT_UI`。状态改变必须与该 route 的契约测试和浏览器证据同时提交。
7. 双库状态只是有界迁移例外，不能作为任何发布完成结论；Gate 12 后必须为零。

### 4.2 唯一 Token 管线

生产实现采用以下单向关系：

```text
frontend/src/styles/tokens.css
  -> Primitive variables
  -> Semantic variables for Light/Dark
  -> necessary Component variables
  -> Tailwind @theme mapping
  -> Nuxt UI supported theme mapping
  -> uPlot / Three.js / virtualization adapters
```

`tokens.css` 是 raw value 的唯一规范来源。Nuxt UI 配置、Tailwind、页面和专业渲染器只能引用语义变量，不得维护独立颜色、间距、圆角、阴影或状态含义。Component Token 只有在语义 Token 和 Nuxt UI variant 无法表达稳定跨页要求时才可新增，并需记录到例外账本。

迁移期保留 `style.css`、`styles/variables.scss`、`styles/light.scss`、`styles/dark.scss` 和 `_telemetry-workspace.scss` 仅用于未迁移消费者。每删除一个 legacy variable，先证明消费者为零；不做无差别批量替换。

### 4.3 目录与组件责任

- Nuxt UI：Button、Form、Input、Select、Dialog、Modal、Drawer、Slideover、Tabs、Table、Menu、Tooltip、Popover、Toast、Badge、Skeleton、Progress、Navigation 等基础控件。
- `frontend/src/components/layout/`：Shell 编排，不复制基础控件。
- `frontend/src/components/workspace/`：可复用的 Workspace Header、Context Toolbar、Inspector 边界等稳定组合。
- `frontend/src/components/domain/` 或现有领域目录：Evidence identity、Approval、Delivery、Verification、Command feedback 等领域组合。
- `frontend/src/composables/`：URL/Inspector/异步生命周期的可复用机制，不建立覆盖领域状态的通用状态机。
- `frontend/src/theme/`：专业渲染器从 Semantic Token 读取主题的适配器，不包含第二套 Theme。

新增目录只在首个真实消费者出现时创建，不提前搭建空框架，不包装每一个 Nuxt UI primitive。

## 5. 通用 Gate、证据与回滚

### 5.1 工作树与回滚点

Gate 0 必须先为当前已通过的前置工作、本文和权威修订建立 Owner 认可的不可变基线提交。未获得 commit 授权时保持 `NOT RUN`，不得假定当前 dirty worktree 是可复现回滚点。

每个后续 Slice：

- 开始前记录 base SHA、`git status --short --branch` 和上一 Gate 结果。
- 只改该 Slice 列出的文件或报告中明确补充的直接依赖。
- 完成后形成独立可评审 diff；只有 Owner 授权时才 commit/push/创建 PR。
- 回滚只撤销该 Slice 的独立变更，不能 reset、restore、clean 或覆盖其他 dirty work。
- 依赖或全局配置 Slice 必须先证明回滚后旧 route 仍可构建和启动。

### 5.2 证据目录

每个 Slice 使用：

```text
docs/evidence/frontend-redesign/implementation/gate-XX-<slug>/
  report.md
  commands/
  browser/
  performance/
```

`report.md` 至少记录：状态、base/final SHA 或精确 worktree identity、文件清单、覆盖的 D/FR 决策、路由、浏览器、viewport、theme、数据源、动作、预期、实际、Console/Network、截图/trace、fixture/read-only/isolated-write 分类、剩余限制、回滚点和评审结论。每个页面还要列出 Loading、Empty、Error、Partial、Stale、Disconnected、Permission Denied 等状态的 applicability；不适用时写 `APPLICABLE=NO` 并把运行状态记为 `NOT RUN` 及原因，不新增第四种测试状态。

证据只保留诊断所需内容，不提交 secret、Bearer token、原始敏感配置或无界 Provider 数据。

### 5.3 分层验证命令

每个编辑循环和每个阶段退出只运行第 0.4 节所定义的 focused checks。不得把以下命令清单当作每个 Slice 的固定全套要求；阶段报告必须说明为何选择或跳过每一项。

阶段检查按变更面选择，例如：

| 变更面 | 关键检查示例 |
| --- | --- |
| 组件、模型或组合逻辑 | 受影响的 unit/model/component tests，必要时局部 typecheck |
| Route、URL、Inspector、History | route/query/scroll/Focus focused tests + 一个相关浏览器 smoke |
| API、SSE、异步生命周期 | 对应 client/store/composable contract tests + 相关 reconnect/cancel smoke |
| Token、Shell、页面布局 | 受影响页面的 Light/Dark、viewport、keyboard/Focus 和 Console/overflow smoke |
| 专业 renderer 或大数据 | 相关 renderer/virtualization/performance probe 和必要的 bundle 检查 |
| 依赖、lockfile、构建或安全配置 | `typecheck`、`build`、依赖树/`npm audit`，仅限实际受影响范围 |

依赖或 lockfile 发生变化时额外运行：

```bash
npm audit --audit-level=high --registry=https://registry.npmjs.org
```

只有在整个前端重构完成并获得后续单独授权的 Gate 12B 才运行完整检查：

```bash
cd frontend
npm run lint
npm run lint:no-new-warnings
npm run typecheck
npm run typecheck:e2e
npm test
npm run build
npm audit --audit-level=high --registry=https://registry.npmjs.org
npm run test:e2e:stable
npm run test:e2e
```

不得通过删除断言、放宽类型、增加 warning budget、关闭 Console 检查或更新截图来掩盖回归。视觉快照只有在 Owner 确认新结果正确后才可更新。

### 5.4 浏览器矩阵代码

| 代码 | 必测内容 |
| --- | --- |
| B1 | Chromium 1440x900，Light/Dark，关键流程与 Console/Network |
| B2 | Chromium 1920x1080，Light/Dark，信息密度与首要任务 |
| B3 | Chromium 1280x800、1024x768，渐进收起、无页面级横向滚动 |
| B4 | Chromium 125%/150% zoom、200% text、长中文/ID/URL/error |
| B5 | keyboard、Focus entry/trap/restore、Skip Link、route H1、topmost Escape、reduced motion |
| B6 | Firefox 和 WebKit 关键只读流程；环境不支持时分别记录 `NOT RUN` |
| B7 | real UI -> API -> Provider 只读链路，记录 request/trace/Provider identity |
| B8 | isolated UI -> API -> persistence/Provider 写链路及 cleanup；不满足隔离条件时 `NOT RUN` |

手机 viewport、Bottom Navigation 和手机专用工作流不属于本计划。1024x768 是桌面降级目标，不是平板产品模式。

### 5.5 性能、Bundle 与可访问性预算

- 主 Shell JavaScript：`<= 300 KiB gzip`。
- Three.js：保持 route lazy chunk，`<= 200 KiB gzip`。
- 新增单个专业 renderer：`<= 80 KiB gzip`；超出必须有分包/替代证据与批准。
- 所有 route component 保持 dynamic import。
- Atlas：200 nodes 下验证 nonblank pixels、交互帧、resize、内存、dispose、context loss、hidden-page pause 和 structured fallback。
- Monitoring：至少 7,200 points、三序列、null gap、downsampling、range/tooltip/keyboard/synchronized table 和交互延迟。
- Logs/Traces/Timeline/Table：分别按 10k logs、2.5k spans、5k timeline、20k table 验证少量 DOM rows、滚动、过滤、stale cancellation、完整复制和 Inspector 稳定。
- 长时间 SSE 验证连接数、重复事件、cursor expiry、resync failure、teardown 和内存增长。
- 语义 `header/nav/main`、heading 顺序、visible focus、可访问名称、状态非颜色单一表达和 Light/Dark 对比不能回退。
- 页面不得出现不连贯重叠、关键控件裁切、主任务的页面级横向滚动或实时更新布局跳动。

## 6. 分阶段实施 Slice

除非某个 Gate 明确允许并行，以下 Slice 按顺序执行。一个 Gate 为 `FAIL` 时停止依赖它的后续 Slice；不把局部 `PASS` 扩大为整站完成。

### Gate 0：批准、权威对齐与不可变基线

**进入条件**

- 本文存在且 Owner 明确给出 `FRONTEND_REFACTOR_PLAN_APPROVED=YES`。
- 前置最终报告仍为 `FRONTEND_PREWORK=PASS`，相关 dirty changes 未丢失或被无关改动覆盖。

**文件范围**

- `docs/CloudOps-Frontend-Refactor-Plan.md`
- `docs/CloudOps-Implementation-Spec.md`
- 与冲突条款直接相关的 Accepted ADR，仅在规范要求 ADR 才能完成 refinement 时修改
- `docs/evidence/frontend-redesign/implementation/gate-00-authority/`

**实施动作**

- 把全栈规范中的 Element Plus 保留条款改为 Nuxt UI 4.10.0 + Tailwind CSS 4.3.3 单一体系。
- 把手机/Bottom Navigation/320px 验收改为 1920x1080、1440x900 主验收和 1280x800、1024x768、125%/150% zoom 桌面降级。
- 把 `/overview` Operations Agent Command Center、additive lazy `/atlas`、legacy Atlas Query 兼容和十个主 Workspace 的关系写入版本化权威。
- 把最终清除 Element Plus、mobile navigation、legacy tokens/overrides 写入版本化 DoD。
- 保留全栈规范的 API、Provider、Owner、安全、Scroll 和领域契约；不借机改后端范围。
- 生成 Gate 0 decision coverage，逐项引用 D-01 至 D-34、FR-SUP-001 至 FR-SUP-010 和 FR-CX-001 至 FR-CX-008，并明确高位取代项。
- 在 Owner 授权后，将当前前置修复、证据、本文和权威修订纳入可复现基线提交。

**验证与浏览器**

- 文档链接、路径、状态和冲突关键词扫描 `PASS`。
- 两份详细设计文档到本计划的 decision ID 差集为零；每个 ID 有处置和 Gate。
- `git diff --check` `PASS`。
- 本 Gate 不改变运行页面，B1-B8 均为 `NOT RUN`。

**回滚点**

- `c8e709fd10ea47976b262dea22440e5496385c1e` 加当前已记录的前置 worktree diff；形成新基线提交前不得开始 Gate 1。

**退出 Gate**

```text
PLAN_APPROVAL=PASS
VERSIONED_AUTHORITY_ALIGNMENT=PASS
IMMUTABLE_PREWORK_BASELINE=PASS
```

### Gate 1：生产依赖接入、Token 管线与预算护栏

**进入条件**

- Gate 0 全部 `PASS`。

**文件范围**

- `frontend/package.json`、`frontend/package-lock.json`
- `frontend/vite.config.ts`、`frontend/tsconfig.json`
- `frontend/src/main.ts`、`frontend/src/App.vue`
- 新增 `frontend/src/styles/tokens.css`、`frontend/src/styles/app.css`
- 必要时新增 `frontend/src/app.config.ts` 或等价的 Nuxt UI 官方主题入口
- 新增 `frontend/scripts/check-bundle-budget.mjs` 及其测试
- 新增 `docs/CloudOps-Frontend-Terminology.md`
- `.github/workflows/ci.yaml`
- Gate 1 证据目录

**实施动作**

- 按精确版本接入 `@nuxt/ui` 4.10.0、`@tailwindcss/vite` 4.3.3、Tailwind 4.3.3、`@iconify-json/lucide`、uPlot 1.6.32 与 TanStack Vue Virtual 3.13.35；保留现有 Three.js 0.185.1。
- 在当前生产 Vite/Vue/TypeScript 工具链上重新验证 Nuxt UI standalone 集成；不直接复制隔离原型配置。
- 建立唯一 `tokens.css`，分区定义 Primitive、Semantic、Component，并从同一变量映射 Tailwind、Nuxt UI 和 specialist adapter。
- 把 D-06 至 D-11 的密度、字体、mono/tabular、边框、圆角、阴影、状态、Focus、loading 和 motion 起点编码为可追溯 Token；像素调整必须通过 B1-B5 证据，不得变成 page-local raw value。
- 定义 Light/Dark 等价 Semantic mapping：首次跟随系统、首次绘制前应用、用户选择后持久化；Dark 不是反色，Light 不是 Nuxt UI 默认白色。
- 建立中文优先术语表，统一 Agent、Incident、Trace、Evidence、Deployment、Provider、Approval、Delivery、Verification 及状态/错误/操作文案；专业原文和标识保持准确。
- Element Plus 的注册与 legacy 样式暂时保留给未迁移 route；冻结任何新增使用。
- 把 lint warning 上限从 2,608 收紧到 Gate 开始时的实际 2,564；后续只下降。
- 使用 Vite manifest 等结构化产物建立 bundle budget 检查并接入 CI。

**验证与浏览器**

- 本阶段依赖、Token 和 bundle 相关的 focused checks、official-registry audit、依赖树重复版本检查和 bundle budget `PASS`。
- 代表性 Nuxt UI Table/Form/Modal/Slideover 的生产工具链 smoke `PASS`，不暴露正式路由。
- B1、B3、B5 覆盖现有 route 回归和主题首次绘制；B6 按环境记录。
- 检查 Nuxt UI 内部可见图标均为 `i-lucide-*`。
- 验证 dense Table、Form、Overlay 和 specialist adapter 均只消费 canonical Semantic Token；无平行 raw theme。

**回滚点**

- Gate 0 baseline；撤销依赖、Vite、入口和新增 Token 文件后，旧 Element Plus route 必须恢复构建与启动。

**退出 Gate**

```text
NUXT_UI_PRODUCTION_TOOLCHAIN=PASS
CANONICAL_TOKEN_PIPELINE=PASS
BUNDLE_BUDGET_CI=PASS
NO_NEW_WARNING_GATE=PASS
```

若 Nuxt UI 不能在当前工具链严格编译，记录可复现 `FAIL` 并停止。任何 Vite/vue-tsc 变更必须作为最小、单独批准的偏差处理。

### Gate 2：App Shell、导航、主题与全局浮层

**进入条件**

- Gate 1 全部 `PASS`。

**文件范围**

- `frontend/src/App.vue`
- `frontend/src/components/layout/AppLayout.vue`
- `frontend/src/components/layout/AppHeader.vue`
- `frontend/src/components/layout/AppSidebar.vue`
- `frontend/src/components/layout/SidebarMenu.vue`
- `frontend/src/components/layout/NotificationInbox.vue`
- `frontend/src/components/layout/MobileBottomNav.vue`（删除）
- `frontend/src/components/agent/GlobalAgentPanel.vue`
- `frontend/src/navigation.ts`、`frontend/src/navigation.test.ts`
- `frontend/src/composables/useTheme.ts`
- `frontend/src/router/index.ts`、`frontend/src/router/scrollBehavior.ts` 及测试
- Shell 直接使用的全局样式与 Gate 2 E2E

**实施动作**

- 用 Nuxt UI 重建 grouped Sidebar、compact Header、Scope/Provider 状态、Notification、Agent pin/Slideover 和 overlay 层级。展开/rail 以 220px/64px 为软目标，1440+ 默认展开，1280/1024 按可用空间和用户偏好渐进收起。
- Sidebar 从上到下固定为 CloudOps 品牌行、Scope 专属行、运行态/处置/系统分组、Agent pin、Local Owner/footer 与 collapse command。Agent 不再出现在普通处置分组，但 `/agent` route 保留。
- Header 左侧为 breadcrumb；右侧为 Provider health、Live Mode/Scenario Active、Notification、Theme 和 Owner identity。Header 不重复 Scope 或 Agent。
- Provider health 使用 Lucide `Bolt` 与 `X/Y`；hover 和 keyboard focus 展开 available/partial/unavailable/disabled 明细，`X=Y` 低噪声，`X<Y` 使用 warning，激活后进入 `/settings#providers`。所有原 emoji 示例替换为 Lucide + 文本。
- Live Mode 与 Scenario Active 都是只读真相标签，Scenario 使用 info/accent 而非 Critical，不提供伪操作。
- 保留十个主 Workspace 路径并为 Gate 4 的 hidden/additive `/atlas` 预留 route meta；保留 Sidebar preference、theme preference、Notification SSE 独立生命周期，以及 Agent 关闭态零读取/零 Consultation SSE。
- Notification 保留 list、unread、read、read-all、refresh、typed context link 和独立 reconnect；不在本 Slice发明新通知类型。
- 删除 `MobileBottomNav.vue`、`mobilePrimaryNavigation`、`mobileMoreNavigation` 和手机导航样式；1024px 使用 desktop rail/collapse，不转为手机工作流。
- 保留 Skip Link、语义 landmarks、route H1 focus、Back/Forward scroll/focus restoration 和 topmost-only Escape。
- 在 RouterView 处建立清晰 legacy route boundary，防止 Nuxt UI Theme 和 Element Plus legacy styles 互相污染。

**验证与浏览器**

- navigation/router/theme/Agent lifecycle/Notification SSE 单元或组件测试 `PASS`。
- 本阶段 Shell、Router、Theme 和全局浮层相关的 focused checks `PASS`。
- 选择与 Shell 风险直接相关的 B1-B6 子集；至少抽查一个 migrated shell + legacy Element Plus route 组合。
- Theme 在首次绘制前正确应用，无闪烁、双 scrollbar、页面级横向 overflow 或 overlay Focus 泄漏。
- 验证 Sidebar/Header 每项职责只有一个入口，Provider hover/focus/click、Agent pin、Notification read/read-all、rail Tooltip/accessible name 和 LocalStorage 恢复全部 `PASS`。

**回滚点**

- Gate 1；App Shell 可独立回退，所有 Workspace route 与 API 不变。

**退出 Gate**

```text
APP_SHELL_MIGRATION=PASS
MOBILE_NAV_RETIREMENT=PASS
GLOBAL_OVERLAY_AND_SSE_CONTRACTS=PASS
OWNER_VISUAL_GATE_SHELL=PASS
```

Owner 对 Shell 视觉不接受时，本 Gate 为 `FAIL`，继续在 Gate 2 内修正，不进入 Workspace 迁移。

### Gate 3：共享 Workspace、URL、Inspector、错误与异步基础

**进入条件**

- Gate 2 全部 `PASS`。

**文件范围**

- 按首个消费者新增 `frontend/src/components/workspace/`
- 按领域需要复用现有 `frontend/src/components/incidents/`，不做纯命名搬家
- `frontend/src/composables/` 下新增 URL/Inspector/异步生命周期 helper
- `frontend/src/api/client.ts` 及其测试，仅允许保持/强化现有错误和取消契约
- `frontend/src/router/`、`frontend/src/utils/contextLink.ts` 及测试
- `frontend/tests/e2e/support.ts` 与 Gate 3 契约测试

**实施动作**

- 建立 Workspace header/context toolbar、Inspector surface、dense data table、typed error presentation、loading/empty/partial/stale/disconnected 等稳定业务组合；基础控件仍由 Nuxt UI 提供。
- 固化 D-05 Toolbar 结构：无 Tab 页面单行编排；有 Tab 页面 Tab 行在上、Toolbar 在下；主 CTA 在右侧，危险操作不进入主 Toolbar。
- 建立 URL codec 和 Inspector replace/push/close/restore 机制；筛选、排序、分页、时间、稳定 Tab 和选中对象进入 URL；hover、tooltip、临时菜单、动画、多栏折叠和本地列偏好不进入 URL。
- Inspector 打开后 Focus 标题/首个有意义区域，topmost `Esc` 关闭，关闭恢复 trigger；快速切换保留列表筛选、分页和 scroll。invalid/deleted/denied/expired target 在 Inspector 内显示明确状态，不自动选择第一行。
- 建立复杂表格合同：约 48px 行高，关键列固定，次要列按页面本地记忆，长标识完整查看/复制；严重性使用左侧 3px marker + 文本 Badge，行背景中性。
- 复用 `ApiError`、request/trace identity、cancel 和 Context Link 校验，不创建第二套 client。
- 为 SSE 共享连接状态提供展示与资源清理机制，但由 Incident、Agent、Notification 等领域保留自己的真实状态机。
- 建立实时列表合同：新行节流聚合为“N 条新内容/立即加载”，不自动插入；现有行就地更新不移动阅读位置；断流、cursor expiry、resync failure 停止 live 声明。
- 建立风险分级确认组合，分别覆盖 acknowledgement、可逆配置、Approval exact hash、rollback 和 forced termination；不得用同一个通用确认文案。
- 覆盖首次 Skeleton、按钮内提交、后台刷新保留内容、长操作阶段/耗时/可用取消、失败保留输入，以及 long text、raw copy、exact UTC、reduced motion、Focus entry/restore。

**验证与浏览器**

- URL codec、legacy query、invalid/deleted/denied target、Back/Forward、stale cancellation、ApiError 和 safe link 测试 `PASS`。
- 本阶段 Workspace、URL、Inspector、错误和异步基础相关的 focused checks `PASS`。
- B1、B3-B5；通过专用 fixture 覆盖所有异常状态，明确标为 presentation evidence。
- 20k table fixture 验证关键列、列偏好、keyboard、full copy 和 selection；SSE fault fixture 验证用户主控新行、无布局跳动和 reduced-motion 即时状态。

**回滚点**

- Gate 2；共享组合未被 route 使用时可独立删除，不能影响 legacy route。

**退出 Gate**

```text
SHARED_WORKSPACE_FOUNDATION=PASS
URL_INSPECTOR_CONTRACT=PASS
ERROR_AND_ASYNC_PRESENTATION=PASS
```

### Gate 4：404、Overview、Infrastructure 与 Atlas

**进入条件**

- Gate 3 全部 `PASS`。

**文件范围**

- `frontend/src/pages/NotFoundPage.vue`
- `frontend/src/views/overview/OverviewView.vue`
- 新增 `frontend/src/views/atlas/AtlasView.vue` 与必要的 `frontend/src/components/overview/`
- `frontend/src/views/infrastructure/InfrastructureView.vue`
- `frontend/src/components/infrastructure/OperationsAtlas.vue`
- `frontend/src/components/infrastructure/StructuredResourceView.vue`
- `frontend/src/router/routes.ts`、route tests 与 legacy Atlas Query compatibility
- `frontend/src/api/infrastructure.ts`、`frontend/src/api/platform.ts`，以及 Overview 只读组合实际使用的现有 typed API client；只允许编排和类型/测试保护
- 相关 route、model、unit 和 E2E tests
- 新增 Three.js semantic-theme adapter

**实施动作**

- 将 Overview、Atlas、Infrastructure 和 404 route tree 完整迁移到 Nuxt UI；不得在这些 tree 中保留 Element Plus。
- 把 `/overview` 实现为 Operations Agent Command Center：首屏包含紧凑状态总览条、活跃 Incidents + 未关联 Alerts、Agent 最新调查摘要/关键 Evidence/置信边界/待处理跳转，以及 Atlas 预览 + 最近 Delivery/Verification 摘要。全部来自现有 typed API/Provider/domain projection，不拼接假数据。
- Overview 只提供导航和带明确 Scope 的只读调查入口；Approval/Delivery/Verification 只展示状态并链接 Incident，禁止直接审批、执行、配置、rollback。只读调查入口进入 `/agent` 的上下文新建流程，不在首页创建无上下文聊天。
- Incident 行最终进入 `/incidents?selected=<id>` 并打开 Inspector；在 Gate 9 完成该 Inspector 前，必须使用仍可完成任务的兼容详情链接，不能生成当前页面无法恢复的死 Query。Gate 9 负责切换并闭合 D-14。
- 无活跃 Incident 时显示健康、近期已解决、上次调查摘要，扩展 Atlas 和 Delivery/Verification 摘要；不生成同权空 Card 墙。1440x900 首屏必须看清主态势并保留下一段内容提示。
- 新增 hidden/additive lazy `/atlas` 专业视图，不把它变成第十一个主 Workspace 导航组。`/overview?view=atlas`、现有 `view=canvas|structured` 与 `resource` Atlas 链接使用 replace 语义归一化到 `/atlas`，保留选择和 Back/Forward 上下文。
- `/atlas` 使用全部可用工作区，保留真实 topology、structured/canvas switch、Inspector push/resize、deep link、structured equivalent、WebGL failure、context loss、visibility pause 和 dispose。节点选择右推 Inspector，画布同步 resize；关闭后恢复。具体 68%/460-520px 为软目标，以 B1-B4 为准。
- 本轮 Atlas 不提供内置图片导出，不开启 `preserveDrawingBuffer` 来支持未批准的截图功能。
- Infrastructure 使用 Namespace/Service/Workload/Pod/Node/Gateway 等真实可用资源类型 Tab、dense table 和 Inspector；保留 cluster/namespace/kind/search/time/resource、partial/unavailable、事件错误、长名称、列偏好、Provider identity 和 Monitoring/Logs context link；不新增资源完整详情页。
- 404 保留原未知路径与恢复入口，不做营销页面。

**验证与浏览器**

- route/query/legacy redirect/Command Center composition/model/Atlas lifecycle tests 和本阶段相关 focused checks `PASS`。
- 选择与 Overview/Infrastructure/Atlas/404 直接相关的 B1-B7 子集；B7 必须分别证明真实 `/overview` Command Center 的现有 typed read 组合，以及 `/atlas` -> `/api/v1/overview` -> Kubernetes Provider topology 链路。若任一所需首页事实没有现有 API，记录 `BACKEND_GAP` 并停止，不用 fixture 补成真实 `PASS`。
- 在 1440x900 验证 active/no-active 两种 Overview 首屏、Incident/Alert/Agent/Approval link 边界、Scope handoff 和无 Card 墙；在 1280/1024/zoom 下按主任务优先降级。
- Atlas 运行 200-node 性能、nonblank canvas pixel、5 次 mount/unmount object count 和生产长度 soak；记录 GPU warnings。

**回滚点**

- Gate 3；Overview/Atlas/Infrastructure/404 作为一个只读迁移单元回退；回滚时移除 additive `/atlas` 并恢复 legacy `/overview` Atlas 行为，所有既有 path 继续可用。

**退出 Gate**

```text
OVERVIEW_COMMAND_CENTER=PASS
ATLAS_ADDITIVE_ROUTE_AND_LEGACY_COMPATIBILITY=PASS
INFRASTRUCTURE_MIGRATION=PASS
ATLAS_RUNTIME_CONTRACT=PASS
REAL_READONLY_INTEGRATION=PASS
OWNER_VISUAL_GATE_READONLY=PASS
```

### Gate 5：Monitoring 与 uPlot

**进入条件**

- Gate 4 全部 `PASS`。

**文件范围**

- `frontend/src/views/monitoring/MonitoringView.vue`
- 按职责新增 `frontend/src/components/monitoring/`
- `frontend/src/api/monitoring.ts`，仅在类型和现有契约测试需要时修改
- Monitoring 使用的 telemetry models/styles/tests
- 新增 uPlot semantic-theme adapter

**实施动作**

- 按 query session、result presentation、definition/authorization management 拆分 1,500+ 行页面，不复制 API/URL 规则。
- 用 uPlot 替换手写 SVG；保留 guided/expert、resource/time/query、execution/definition URL、start/cancel、history、save、authorize/revoke 和安全 Provider link。
- 采用图表优先布局：固定查询区约 88px、uPlot 约 440px、同步数据表约 280px 首屏可见，具体高度在 1440/1024/zoom 实测后调整，但不得把图表或表格压成不可用附属区。
- 提供 tooltip、brush/range selection、multiple series、null gaps、downsampling、keyboard path 和 synchronized data table；状态、焦点、Tooltip 和系列颜色全部来自 CloudOps Semantic Token。
- Monitoring 查询快照提供带 Scope/resource/query/time/provider provenance 的 Evidence handoff；没有进行中调查时引导关联或新建，不创建脱离上下文 Evidence。若现有 API 不支持该契约，记录 `BACKEND_GAP`，不得伪造前端成功。
- Save definition 使用受控 Nuxt UI Dialog，覆盖 entry/trap/submit/cancel/Escape/restore；authorize/revoke 使用与实际后果匹配的确认，不复用危险操作通用文案。
- Query 执行按现有契约归类；definition/authorization 等真实写入不得在非隔离环境执行。

**验证与浏览器**

- query cancellation、stale response、URL、Dialog/Focus、Provider URL 和 renderer adapter tests `PASS`。
- 本阶段 Monitoring 相关 focused checks 与 uPlot chunk budget `PASS`。
- 选择与 Monitoring 直接相关的 B1-B7 子集；B8 仅在隔离条件齐全时运行，否则 `NOT RUN`。
- 7,200 points/3 series/Partial/Empty/Light/Dark/keyboard/synchronized table、1440 首屏图表优先和 1024 降级证据 `PASS`。

**回滚点**

- Gate 4；Monitoring route 独立回退，uPlot 依赖保留到 Gate 12 再判断消费者。

**退出 Gate**

```text
MONITORING_MIGRATION=PASS
UPLOT_RENDERER=PASS
```

`MONITORING_WRITE_E2E` 必须单独记录为 `PASS` 或 `NOT RUN`；若为 `FAIL`，不得退出本 Gate。

### Gate 6：Logs 与 Traces 大数据工作区

**进入条件**

- Gate 5 全部阻塞项 `PASS`；写链路 `NOT RUN` 不阻止只读迁移。

**文件范围**

- `frontend/src/views/logs/LogsView.vue`
- `frontend/src/views/traces/TracesView.vue`
- 按职责新增 `frontend/src/components/logs/`、`frontend/src/components/traces/`
- `frontend/src/api/telemetry.ts` 与 telemetry models/tests，仅做契约保护所需改动
- `_telemetry-workspace.scss` 的消费者迁移和 TanStack virtualization adapter

**实施动作**

- Logs 保留顶部固定查询区、guided/expert、time/level/text/trace/tail/wrap/history、row selection、full copy、Evidence/Consultation 和 Provider link。原始行默认保持结构与 bounded horizontal scroll；wrap 是显式 toggle，复制值永不使用视觉截断结果。
- Log 行进入右推 Inspector，显示完整原文、解析字段、Trace 关联、Provider context 和 Evidence handoff；没有调查上下文时引导关联/新建，不静默提交。
- Traces 保留 search/filter/detail/span selection、full copy、Evidence/Consultation 和 Provider link；保留现有 Trace 语义渲染器。选择 Trace 后仍在 canonical `/traces?trace_id=...` 进入全宽详情模式，不强制新增路径。
- Trace 详情以瀑布/Span 主区约 65% + Span Inspector 约 35% 为软目标，显示 parent/child、service semantic color、Tags、Logs、resource context 和 Evidence handoff；Back 恢复搜索条件和列表位置。
- 对 10k logs 和 2.5k spans 使用 TanStack Vue Virtual；虚拟化不能破坏 keyboard、selection、Inspector、copy 或 accessible item count。
- 统一 canonical `resource`；legacy `workload` 只作为兼容输入，不再生成。
- 旧请求取消后不得覆盖新结果；Partial/Stale/Expired history 保留当前内容和恢复动作。

**验证与浏览器**

- query codec、legacy query、full-detail mode、Back restore、wrap/raw copy、cancel、selection、Evidence provenance 和 context link tests `PASS`。
- 本阶段 Logs/Traces 相关 focused checks 与 virtualization chunk budget `PASS`。
- 选择与 Logs/Traces 直接相关的 B1-B7 子集；B8 的 Evidence/Consultation 创建仅在隔离条件满足时运行，否则 `NOT RUN`。
- 10k/2.5k scale 下 `<100` rendered rows、滚动/过滤/Inspector 稳定和 stale cancellation `PASS`。

**回滚点**

- Gate 5；Logs 和 Traces 分别保持独立 route rollback，可先回退失败者而不回退已通过者。

**退出 Gate**

```text
LOGS_MIGRATION=PASS
TRACES_MIGRATION=PASS
LARGE_DATA_VIRTUALIZATION=PASS
```

`TELEMETRY_WRITE_E2E` 必须单独记录为 `PASS` 或 `NOT RUN`；若为 `FAIL`，不得退出本 Gate。

### Gate 7：Alerts

**进入条件**

- Gate 6 全部阻塞项 `PASS`。

**文件范围**

- `frontend/src/views/alerts/AlertsView.vue`
- `frontend/src/views/alerts/AlertDetailView.vue`
- `frontend/src/components/alerts/AlertBadges.vue`
- `frontend/src/api/alerts.ts` 及直接测试
- Alert route/query/context-link E2E

**实施动作**

- 把 list/detail tree 完整迁移到 Nuxt UI，保留 status/severity/namespace/search/incident/cursor/limit 和 alert ID 深链接。
- 列表采用约 48px dense row、左侧 severity marker + 文本 Badge、固定关键列和本地次要列偏好；新 Alert 通过节流聚合提示由用户加载，不自动插入打断扫描。
- 行选择使用 Inspector 快扫，显示 Alert 状态历史、关联 Incident、Provider/版本事实和恢复动作；`/alerts/:alertId` 继续作为可分享完整深链接。
- 保留 alert-local acknowledge/silence/expire；acknowledge 与 silence/expire 使用不同风险确认。事故生命周期操作只链接或迁移到 Incident，不在 Alert 复制 Approval/Delivery/Verification。
- 接受 legacy `workload` 输入并归一化到 `resource`，所有新链接只输出 canonical query。
- 保留 expected version、Idempotency-Key、request/trace identity 和真实命令状态。

**验证与浏览器**

- filter/query/deep-link/Inspector history/new-row prompt/version/idempotency/context-link tests 和本阶段相关 focused checks `PASS`。
- 选择与 Alerts 直接相关的 B1-B7 子集；B8 的 acknowledge/silence/Incident attach/create 仅在隔离条件满足时运行，否则 `NOT RUN`。

**回滚点**

- Gate 6；Alerts list/detail 作为同一 ownership Slice 回退。

**退出 Gate**

```text
ALERTS_MIGRATION=PASS
LEGACY_WORKLOAD_COMPATIBILITY=PASS
```

`ALERT_WRITE_E2E` 必须单独记录为 `PASS` 或 `NOT RUN`；若为 `FAIL`，不得退出本 Gate。

### Gate 8：Agent Workspace

**进入条件**

- Gate 7 全部阻塞项 `PASS`。

**文件范围**

- `frontend/src/views/agent/AgentWorkspaceView.vue`
- `frontend/src/components/agent/AgentConversation.vue`
- `frontend/src/components/agent/AgentHistory.vue`
- `frontend/src/components/agent/AgentInspector.vue`
- `frontend/src/stores/agentWorkspace.ts`
- `frontend/src/api/agent.ts`、`frontend/src/utils/agentContext.ts` 及测试
- Global Agent 与 full Workspace 的集成测试

**实施动作**

- 用 Nuxt UI 重建 History/Conversation/Inspector 三栏工作区；History 约 220px、Conversation 为弹性主区、Inspector 约 340px 是软目标。每个辅助栏可完整收为约 24px 边条，不采用拖拽，不把折叠状态写入 URL。
- 1920/1440 默认保留三栏；1280 优先收起 History，1024/150% zoom 继续保证 Conversation 主任务并按需收起 Inspector。任何宽度都能恢复辅助栏且不出现页面级横向滚动。
- 保留 context-triggered、structured new 和 free query 三种入口，视觉优先级依次降低；自由查询明确标记“无关联事件”及 Evidence 边界。持续显示 Scope/Incident/Alert/Service/time/Evidence 上下文。
- 从 Overview、Alert、Incident 进入时恢复对应 structured context；系统自动调查显示来源但不伪造手动触发。Sidebar pin 浮层只展示当前阶段、最新结论摘要和“进入完整 Workspace”。
- 保留 Investigation/Consultation、send/cancel、Knowledge、plan/action card、exact hash/authority/idempotency 和完整 Tool/Evidence 状态。
- 保证 Global Agent 和 `/agent` 的读取/stream ownership 互斥清晰；关闭或卸载立即 teardown。
- 长消息、Tool timeline、Evidence list 虚拟化不得丢失 full copy 和可访问顺序。

**验证与浏览器**

- Store/API/SSE/context/keyboard/teardown tests 和本阶段相关 focused checks `PASS`。
- 选择与 Agent Workspace 直接相关的 B1-B7 子集；B8 的 message/Knowledge/plan/card 写入仅在隔离条件满足时运行，否则 `NOT RUN`。
- 连接、重连、取消、重复事件、历史恢复、三种入口、上下文切换和各宽度折叠无丢失 `PASS`。

**回滚点**

- Gate 7；full Agent route 独立回退，Gate 2 的 Global Agent Shell 入口仍可使用兼容 store。

**退出 Gate**

```text
AGENT_WORKSPACE_MIGRATION=PASS
AGENT_CONTEXT_AND_SSE=PASS
OWNER_VISUAL_GATE_AGENT=PASS
```

`AGENT_WRITE_E2E` 必须单独记录为 `PASS` 或 `NOT RUN`；若为 `FAIL`，不得退出本 Gate。

### Gate 9：Incident 列表与事故生命周期单一操作面

**进入条件**

- Gate 8 全部阻塞项 `PASS`。

**文件范围**

- `frontend/src/views/incidents/IncidentListView.vue`
- `frontend/src/views/incidents/IncidentDetailView.vue`
- `frontend/src/components/incidents/` 全部当前消费者
- `frontend/src/composables/incidents/`
- `frontend/src/api/incidents.ts`
- `frontend/src/models/incidents.ts`、`workbench.ts`、`recovery.ts`、`commands.ts` 及测试
- Incident route/SSE/E2E tests

**实施动作**

- 先迁移 Incident list，再迁移 detail；两个子步骤分别保持可运行和可回滚。
- 列表使用标准 Toolbar、约 48px dense row、左侧 severity marker + Badge、固定关键列和本地次要列偏好。把 stable sort/direction 放入 URL，保留 status/severity/service/attention/resource/alert/from/to/limit/cursor、load more、selection、refresh 和 Back restore。
- 行选择打开 Incident Inspector 摘要：状态、严重性、Agent 结论、Evidence 数量、Approval/Delivery/Verification 摘要和完整详情入口。完成后把 Overview Incident 链接 canonicalize 为 `/incidents?selected=<id>`，Back 恢复 Overview 或原列表上下文。
- Detail 保留 sticky context header 与单列线性流；ZoneNav 最终顺序为 Agent 调查、Evidence、Approval、Delivery、Verification、Timeline、Resolution，保留 persisted context、signals、Recovery Decision、related resources 及既有 hash/legacy anchor 兼容。
- 只在 Incident 挂载并完善 Approval、Delivery、Verification；不得把历史未挂载组件误报为已保留的用户界面。
- 新 Incident 使用聚合提示由用户加载；现有 Incident/Approval/Delivery/Verification 就地更新且不移动阅读位置。对 finite-stream 首次回放按 resource/cursor 合并，增加有界背压；保留 cursor 去重、reconnect、resync、teardown 和当前 Evidence 真相。
- 5k timeline 使用虚拟化或有证据的渐进渲染，同时保留锚点、selection、exact UTC 和完整复制。
- 危险确认按 consequence 显示 target、authority、exact hash/version、影响和恢复限制；前端不是安全边界。

**验证与浏览器**

- model/command/query/sort/SSE/backpressure/zone/hash/idempotency tests 和本阶段相关 focused checks `PASS`。
- 选择与 Incident 只读流程直接相关的 B1-B7 子集；B8 覆盖 investigation/recovery/close/remediation/Approval/Delivery/Verification 时必须使用隔离身份、目标和 cleanup，否则逐项 `NOT RUN`。
- Firefox 关键 Incident 只读流必须 `PASS`；WebKit 按环境记录。

**回滚点**

- Gate 8；list 与 detail 使用两个连续独立 rollback points，detail 不通过不得改变 DevOps ownership。

**退出 Gate**

```text
INCIDENT_LIST_MIGRATION=PASS
INCIDENT_DETAIL_MIGRATION=PASS
INCIDENT_LIFECYCLE_SINGLE_OWNER=PASS
INCIDENT_SSE_BACKPRESSURE=PASS
OWNER_VISUAL_GATE_INCIDENT=PASS
```

`INCIDENT_WRITE_E2E` 必须单独记录为 `PASS` 或 `NOT RUN`；若为 `FAIL`，不得退出本 Gate。

### Gate 10：DevOps 责任收敛

**进入条件**

- Gate 9 的 Incident 单一操作面 `PASS`。

**文件范围**

- `frontend/src/views/devops/DevOpsWorkspaceView.vue`
- `frontend/src/stores/devOpsWorkspace.ts` 及测试
- `frontend/src/api/devops.ts`
- Incident/DevOps cross-link 和 ownership E2E

**实施动作**

- 用 Nuxt UI 迁移 Operations/Identity、subject/operation selection、global queue、non-incident plan/card 和 technical detail。
- 列表行先打开压缩 Inspector，展示 ExactIdentity、Authority、当前 execution/Delivery/Verification 摘要；完整详情使用现有 `view/subject/operation` Query 进入全宽模式，不强制新增不兼容 route。
- 完整详情保留线性 `DeliveryRail` 与 `VerificationMatrix`；当前流程不引入 DAG/Flow 图库。rollback/forced termination 等危险操作只在完整详情出现，并使用最高强度精确确认。
- 删除重复的 incident Approval/Delivery/Verification 写入口，替换为带稳定上下文的 Incident stage link。
- 保留 freeze/candidate/baseline/delivery/provider branch、exact expected hash 和 authority truth。
- accepted/dispatched/observed/verified 呈现沿用同一领域组合和 Token，不把技术执行状态样式成恢复成功。

**验证与浏览器**

- Store/API/Inspector/full-detail/Query compatibility/ownership/link/hash/authority tests 和本阶段相关 focused checks `PASS`。
- 选择与 DevOps ownership 和详情流程直接相关的 B1-B7 子集；B8 的 global/non-incident execution 仅在隔离条件满足时运行，否则 `NOT RUN`。
- 自动检查 DevOps 无 incident write action，Incident link 可直接恢复对应阶段和上下文。

**回滚点**

- Gate 9；若 DevOps 回退，必须临时隐藏而不是恢复重复事故写入口，直至修复通过。

**退出 Gate**

```text
DEVOPS_MIGRATION=PASS
INCIDENT_DEVOPS_OWNERSHIP=PASS
```

`DEVOPS_WRITE_E2E` 必须单独记录为 `PASS` 或 `NOT RUN`；若为 `FAIL`，不得退出本 Gate。

### Gate 11：Settings draft/apply 工作流

**进入条件**

- Gate 10 全部阻塞项 `PASS`。

**文件范围**

- `frontend/src/views/settings/SettingsView.vue`
- `frontend/src/api/platform.ts` 及现有 settings/scopes/storage types/tests
- 必要的 Settings 领域组合、route anchor 和 leave-guard tests

**实施动作**

- 用 Nuxt UI Form/Field/validation 迁移 Provider、Scope、system、policy、secret reference、revision/history/storage 区段。
- 内容区左对齐并使用约 960px 软上限；Settings 不使用 Inspector。Revision History 是同页独立区段，不另建全屏工作流。
- 每个 section 保持独立 frontend draft；显式 validate 和 apply，显示 change summary、validation identity/hash、revision conflict、partial worker result、retry 和 leave protection。
- 不把 local draft 描述成 backend Draft，不把逐项成功描述成 atomic success，不回显 secret。
- 保留 `#providers` 深链接并接收 Header Provider health 跳转；1024 桌面降级、first-error Focus、unapplied edit dismissal guard 和 Provider test truth 均保持。配置 apply 使用逐项后果确认，secret 与 Provider test 使用各自准确文案。

**验证与浏览器**

- draft/validation/revision/partial/retry/leave/anchor tests 和本阶段相关 focused checks `PASS`。
- 选择与 Settings draft/apply 直接相关的 B1-B7 子集；B8 的 validate/apply/secret/provider test 只有隔离目标、凭据与 cleanup 齐全时运行，否则逐项 `NOT RUN`。
- 1024x768、150% zoom、200% text 和长错误无裁切或 page overflow。

**回滚点**

- Gate 10；Settings route 独立回退，未应用 draft 不产生后端副作用。

**退出 Gate**

```text
SETTINGS_MIGRATION=PASS
SETTINGS_DRAFT_AND_PARTIAL_TRUTH=PASS
OWNER_VISUAL_GATE_SETTINGS=PASS
```

`SETTINGS_WRITE_E2E` 必须单独记录为 `PASS` 或 `NOT RUN`；若为 `FAIL`，不得退出本 Gate。

### Gate 12：历史合并定义（当前按 0.5 拆分为 Gate 12A 与 Gate 12B）

本节保留完整清理和验证清单。当前执行时，实施动作与依赖清理归 Gate 12A，完整静态、自动化、浏览器、性能、真实集成和发布判断归 Gate 12B；进入、验证和退出状态以第 0.5 节为准。

**进入条件**

- Gate 1-11 的所有阻塞项 `PASS`。
- capability map 中十个主 Workspace、详情、404 与 additive `/atlas` 均已登记 `MIGRATED_NUXT_UI`。

**文件范围**

- `frontend/package.json`、`frontend/package-lock.json`
- `frontend/src/main.ts`、`frontend/src/style.css`、`frontend/src/styles/`
- 所有仍被扫描命中的 Element Plus、mobile navigation、legacy override 或 orphan token 消费者
- frontend CI、bundle checks、Playwright suites
- `docs/CloudOps-Implementation-Spec.md`、本计划状态、实现最终报告和必要 current-state docs

**实施动作**

- 删除 Element Plus 依赖、全局注册、style import、组件 import、`<el-*>`、message/message-box 调用和 theme mapping。
- 删除已无消费者的 `variables.scss`、`light.scss`、`dark.scss`、`_telemetry-workspace.scss` 内容或文件；保留语义价值必须迁入 canonical Token，不保留平行 raw source。
- 删除 MobileBottomNav、mobile navigation exports/styles/tests 和所有失效引用。
- 清点 `:deep()`、`:global()`、`!important`、raw color、任意尺寸和 library DOM selector；删除或为极少数剩余项提供有测试的例外账本。
- 确认所有图标为 Lucide、所有 route lazy、无第二通用 UI、无空 wrapper framework、无 prototype import。
- 更新 capability map/result，加入 `/atlas`、Overview Command Center 和 legacy Atlas Query disposition，使 `UNMAPPED_CAPABILITIES=0` 对应最终生产树，而非历史前置树。
- 逐项核对 D-01 至 D-34、FR-SUP-001 至 FR-SUP-010、FR-CX-001 至 FR-CX-008；每项必须有可达生产 UI 或明确高位取代、对应测试和浏览器证据。
- 审查最终中文术语和状态文案，确保 CloudOps/CloudOps-Copilot 品牌、专业英文、状态含义和错误恢复建议一致，无 emoji 或营销说明。
- 明确生产 source map 策略：默认不公开 `.map`；若保留，必须有受控访问和 Owner 批准，不能随静态前端公开分发。
- 运行完整静态、unit、build、audit、浏览器、视觉、性能、真实只读集成和可用的 isolated-write 验证。

**必须为零的扫描**

```bash
rg -n 'element-plus|@element-plus|<el-|El(Button|Dialog|Drawer|Dropdown|Icon|Input|Message|MessageBox|Option|Result|Select)' frontend/src frontend/package.json frontend/package-lock.json
rg -n 'MobileBottomNav|mobilePrimaryNavigation|mobileMoreNavigation' frontend/src
npm ls element-plus @element-plus/icons-vue --depth=0
```

前两个 `rg` 以 exit 1 且无输出表示零命中；`npm ls` 必须证明顶层依赖不存在。对 `:deep()`、`:global()`、`!important`、raw color 和非 Lucide icon 使用单独扫描，任何非零命中必须进入已批准例外账本，不能静默忽略。

**详细设计 ID 覆盖扫描**

```bash
comm -23 \
  <(rg -o 'D-[0-9]{2}|FR-(SUP|CX)-[0-9]{3}' /home/monody/k8s/CloudOps-前端详细设计决策记录.md /home/monody/k8s/CloudOps-前端详细设计决策补充与复核.md | sed 's/.*://' | sort -u) \
  <(rg -o 'D-[0-9]{2}|FR-(SUP|CX)-[0-9]{3}' docs/CloudOps-Frontend-Refactor-Plan.md | sort -u)
```

输出必须为空。该扫描只证明 ID 已映射，不能替代对应 route、状态、浏览器和真实数据证据。

**验证与浏览器**

- 完整验证命令全部 `PASS`，没有新增或未解释的 ESLint、Vue、Router、Console、build warning。
- B1-B7 覆盖所有关键 read-only route；B6 分浏览器报告，不能从 Chromium 推断 Firefox/WebKit。
- B8 按第 7 节执行；缺少隔离条件时保持 `NOT RUN`，并将 release readiness 保持为 `NO`。
- 重新测量主 entry、Three.js、uPlot、virtualization、route chunks、Atlas、SSE 和 large-data budgets。
- 对每个关键页面记录 Lighthouse Accessibility 诊断，目标 `>=95`，同时要求已确认的 ARIA/Focus/Dialog/overflow 缺陷为零；分数不能覆盖具体 DOM 失败。
- 验证 `/overview` Command Center、`/atlas`、Trace full-detail Query、DevOps full-detail Query、Incident Inspector 和全部 legacy links 的直接进入、刷新与 Back/Forward。
- Owner 审查 Shell、Incident、Monitoring、Atlas、Agent、Settings、异常状态和桌面降级的最终 Light/Dark 截图与交互记录。

**回滚点**

- Gate 11；在 Element Plus 删除提交前保留最后一个全部 route 可运行的双库迁移点。若删除后任一 route `FAIL`，只回退 Gate 12 cleanup，不回退已经通过的业务迁移。

**退出 Gate**

```text
ELEMENT_PLUS_REMOVAL=PASS
MOBILE_NAV_REMOVAL=PASS
SINGLE_GENERAL_UI_SYSTEM=PASS
LUCIDE_ONLY=PASS
CAPABILITY_CONTRACT_MAP=PASS
UNMAPPED_CAPABILITIES=0
DETAILED_DESIGN_COVERAGE=PASS
FULL_STATIC_AND_AUTOMATED_VALIDATION=PASS
REAL_READONLY_INTEGRATION=PASS
OWNER_FINAL_VISUAL_ACCEPTED=YES
FRONTEND_MIGRATION=PASS
```

`WRITE_PATH_E2E` 必须单独记录为 `PASS` 或 `NOT RUN`；若任何已运行写链路为 `FAIL`，不得退出本 Gate。

若 `WRITE_PATH_E2E=NOT RUN`，可以如实声明视觉/代码迁移和只读集成已完成，但必须同时声明：

```text
FRONTEND_RELEASE_READY=NO
```

不得把该状态缩写为“全面联调通过”或“可发布”。

### Gate 12 外部发布依赖

以下来源于详细设计记录的发布前要求不属于本前端计划可直接修改的代码范围，但不得静默遗漏：

| 项目 | 本计划处理 | 发布状态影响 |
| --- | --- | --- |
| HTML `Cache-Control`、`Content-Security-Policy`、`X-Content-Type-Options` | 记录当前响应头证据；缺失时提出最小 Go/Ingress `BACKEND_GAP`，不在前端 Slice 越权修改 | 对外暴露前必须解决；未验证为 `NOT RUN` |
| Production source map | Gate 12 在 Vite build 中禁止公开或证明受控访问 | 未闭合时 release `FAIL` |
| Provider URL allowlist | 前端统一校验并消费后端可信投影；若完整 target allowlist 必须由后端控制，单独记录契约 | 任何不安全入口为 `FAIL` |
| Shared/LAN/multi-user deployment | 不从 Local Owner UI 推导登录、route guard 或多租户授权 | 范围变化时必须新 ADR；当前为 `NOT RUN` |

## 7. 写链路隔离与验证策略

### 7.1 运行前置条件

真实写链路 E2E 只有在以下条件同时存在时才能运行：

1. 明确命名、与真实环境隔离的测试目标和 Provider resources。
2. 只对该目标有权限的测试身份/凭据，且 secret 不进入 Git、日志、截图或报告。
3. 每个 command family 的初始状态、expected revision/version/hash 和唯一 idempotency key。
4. 自动或可验证的 cleanup，能够证明 CloudOps persistence 和 Provider side effect 均恢复或删除。
5. Owner 对测试范围和外部写入的明确授权。

任一条件缺失时，该 command family 为 `NOT RUN`，不得使用 mock 或 API 200 替代。

### 7.2 命令族与断言

| 命令族 | 必须断言 | Cleanup 证明 |
| --- | --- | --- |
| Scope activation | scope ID、Bootstrap refresh、partial refresh truth | 恢复原 active scope/revision |
| Notification read/read-all | notification identity、cursor、UI/SSE consistency | 恢复 fixture 或删除隔离通知 |
| Monitoring definition/authorization | execution/definition identity、explicit revoke、request identity | revoke/delete 隔离定义和授权 |
| Alert commands | expected version、unique idempotency、Provider/Incident result | expire/restore 隔离 alert 或销毁 scenario |
| Logs/Trace Evidence/Consultation | query/span/time/scope provenance、persisted identity | 删除隔离 Evidence/Consultation 或销毁 DB |
| Agent/Knowledge/plan/card | context、exact hash、authority、idempotency、SSE truth | 删除隔离记录并撤销 Provider side effect |
| Incident commands | expected version/hash、reason、202、request/trace、replay truth | scenario teardown 和持久记录审计 |
| DevOps execution | exact expected hash、authority、accepted/dispatched/observed/verified | Provider rollback/cleanup 和 current Evidence |
| Settings | validation ID/draft hash/revision、partial items、secret non-disclosure | 恢复原 revision/provider config |

测试报告必须同时给出前端可见结果、Network identity、持久化或 Provider 结果及 cleanup 后状态。只看到 Toast、accepted 或 dispatched 不构成通过。

## 8. Owner 视觉 Gate

前置原型已获得 `OWNER_VISUAL_ACCEPTED=YES`，它锁定产品方向，不替代生产实现判断。实施期至少保留以下 Owner Gate：

| Gate | 审查范围 | 必须材料 |
| --- | --- | --- |
| Gate 2 | Shell、导航、主题、Notification、Agent pin、legacy route boundary | 1920/1440 Light/Dark，1024 collapse，keyboard/Focus 录像 |
| Gate 4 | Overview、Infrastructure、Atlas、404 | 真实 Atlas、structured fallback、Inspector、错误/Partial |
| Gate 8 | Agent | 三栏、各桌面宽度、长内容、SSE/上下文状态 |
| Gate 9 | Incident | list、detail、Evidence、Approval、Delivery、Verification 和危险状态 |
| Gate 11 | Settings | draft、invalid、partial、conflict、leave guard、1024/zoom |
| Gate 12 | 整站最终 | 关键路由 Light/Dark、专业渲染、异常状态、性能与交互记录 |

Owner 未审查为 `NOT RUN`；明确拒绝为 `FAIL`。自动截图 diff、Lighthouse、axe 或 Playwright 不能把 Owner 的 `FAIL` 改成 `PASS`。

## 9. 停止条件

出现以下任一项时，停止受影响 Slice，不进入依赖它的下一 Gate：

- 未收到计划批准，或版本化权威仍冲突。
- 当前 dirty worktree 与 Slice 文件范围重叠且无法确认归属。
- 一个 migrated route 引入 Element Plus，或引入第二套通用 UI。
- 公开路径、legacy query、Back/Forward、ApiError、SSE、Provider、Evidence 或安全契约回归。
- 必要交互只能通过修改未授权的 Go/API/数据库/Provider 语义完成。
- route 不再 lazy，warning budget 增加，source strictness 被弱化，bundle 超预算且无批准偏差。
- Console/Network 有阻塞错误，Focus/keyboard/overlay 行为失败，或页面产生关键裁切、重叠、主滚动冲突。
- fixture 被用来声称真实 read/write integration `PASS`。
- 真实写测试缺少隔离目标、测试身份、cleanup 或 Owner 授权。
- Owner 视觉 Gate 为 `FAIL`，或 PR review 为 `REJECTED`。

独立且不依赖失败项的只读诊断可以继续，但不得扩大实施范围或越过 Gate。

## 10. 最终交付与状态模板

最终报告至少包含：

1. exact final SHA、branch、worktree 和完整文件/依赖变化。
2. 每个 route 的迁移 ownership、能力映射和零遗漏结果。
3. 静态、typecheck、unit、build、audit、E2E 和 warning 结果。
4. 浏览器、viewport、theme、keyboard、Focus、accessibility 和视觉证据。
5. bundle、Atlas、uPlot、virtualization、SSE 和长时运行结果。
6. 真实 UI -> API -> Provider 只读链路。
7. 每个写 command family 的 `PASS`/`FAIL`/`NOT RUN` 与 cleanup 证据。
8. Element Plus、mobile navigation、legacy token/override 和非 Lucide icon 零残留证明。
9. 所有 `BACKEND_GAP`、`APPROVED_DEVIATION`、剩余风险与 release blocker。
10. Owner 最终视觉原文。

本计划生成时的状态已由 2026-07-30 Owner 批准和 Gate 0 权威对齐取代。当前状态为：

```text
PLAN_STATUS=APPROVED_AND_IN_LOCAL_IMPLEMENTATION
PLAN_GENERATED=YES
PHASE_VALIDATION=MINIMUM_LANE_CHECKS_ONLY
FULL_FRONTEND_VALIDATION=DEFERRED_TO_GATE_12B
DESIGN_DECISION_MAPPING=PASS
SOURCE_DESIGN_DECISIONS=52
UNMAPPED_DESIGN_DECISIONS=0
FRONTEND_REFACTOR_PLAN_APPROVED=YES
FRONTEND_PARALLEL_IMPLEMENTATION_AUTHORIZED=YES
VERSIONED_AUTHORITY_ALIGNMENT=PASS
IMMUTABLE_PREWORK_BASELINE=PASS
GATE_00=PASS
GATE_01=PASS
GATE_02=PASS
GATE_03=PASS
NUXT_UI_PRODUCTION_TOOLCHAIN=PASS
CANONICAL_TOKEN_PIPELINE=PASS
BUNDLE_BUDGET_CI=PASS
NO_NEW_WARNING_GATE=PASS
CURRENT_GATE=GATE_04_TO_11_PARALLEL_BASELINE_READY
PRODUCTION_MIGRATION=IN_PROGRESS
DETAILED_DESIGN_IMPLEMENTATION=IN_PROGRESS
WRITE_PATH_E2E=NOT RUN
OWNER_PREWORK_VISUAL_REVIEW=PASS
READY_FOR_IMPLEMENTATION=YES
```
