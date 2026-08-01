# CloudOps-Copilot CPA 前端最终重建实施方案

> 状态：`PLAN_APPROVED=YES`
>
> Owner 共同理解确认：2026-08-01（Asia/Shanghai）
>
> 目标仓库：`/home/monody/k8s/CloudOps-Copilot`
>
> 当前代码基线：`main@dae990be63593fdcbeb281a0807368e0cc623343`，实施开始时必须重新解析当前 HEAD
>
> CPA 固定参考：`router-for-me/Cli-Proxy-API-Management-Center@7976b16f6c2fb957a050c0593e571c59dc836f9b`
>
> 外部写入：`NOT_AUTHORIZED`；不得 push、创建 PR、发布或部署

## 0. 本方案如何使用

### 0.1 唯一执行权威

本方案取代 `docs/CloudOps-Frontend-Refactor-Plan.md` 的全部实施 Gate、页面结构、验证节奏和视觉验收规则。旧计划、旧 evidence、旧 handoff 与旧提交继续保留历史 provenance，但不得作为当前实施指令。

解释冲突时按以下顺序处理：

1. Owner 最新明确指令。
2. Owner 已认可的当前 Shell、`/overview`、`/agent` 实际运行效果。
3. `/home/monody/k8s/vue.md`。
4. 固定 CPA SHA 的实际页面、源码、Token 和交互行为。
5. `/home/monody/k8s/cloudops-cpa-style-full-redesign-spec.md` 中仍与以上来源一致的要求。
6. 本方案的实施顺序、所有权、完成定义和验证边界。
7. 当前 Router、typed API、SSE、领域模型、后端合同与安全语义。
8. 旧计划和历史 evidence，仅用于查找能力与已知缺口。

当前样例没有覆盖的通用交互才回到 CPA 固定 SHA 查证。不得根据个人印象实现一个“类似 CPA 的暖灰后台”，也不得因 CPA 后续 `main` 漂移而自动改变本轮设计。

### 0.2 已批准决定

```text
CURRENT_SAMPLE_IS_FINAL_VISUAL_REFERENCE=YES
CPA_ONE_TO_ONE_BASELINE=YES
CLOUDOPS_DOMAIN_ENHANCEMENT_TARGET=120_PERCENT
SETTINGS_FOLLOWS_PINNED_CPA=YES
FULL_USER_CAPABILITY_RETENTION=YES
PRODUCTION_FIXTURES=FORBIDDEN
IMPLEMENTATION_VALIDATION=FOCUSED_MINIMUM_ONLY
INTERMEDIATE_OWNER_VISUAL_GATES=REMOVED
FINAL_OWNER_VISUAL_GATE_BEFORE_INTEGRATION=REQUIRED
FINAL_FUNCTION_INTEGRATION_AFTER_VISUAL_PASS_ONLY=YES
LOCAL_BASELINE_COMMIT_AUTHORIZED=YES
LOCAL_STAGE_COMMITS_AUTHORIZED=YES
PUSH_PR_RELEASE_DEPLOY=NOT_AUTHORIZED
```

### 0.3 状态词

实施和验证只使用以下状态：

- `PASS`：在当前精确工作树或提交上实际运行并通过。
- `FAIL`：已运行且行为、证据或 Owner 视觉判断不满足要求。
- `NOT RUN`：未运行、条件不满足、没有授权或不属于当前阶段。
- `BACKEND_GAP`：真实联调证明前端所需合同不存在或存在后端缺陷；不得用 fixture、推测或前端伪造掩盖。

不得把 fixture smoke、静态截图、单个 HTTP 200、历史报告或旧 SHA 的结果提升为真实联调 `PASS`。

## 1. 最终目标与产品范围

### 1.1 目标

最终产品必须同时满足：

1. Shell、导航、通用控件、页面构图、反馈和动效达到固定 CPA 版本的一比一视觉与交互一致性。
2. Overview、Agent、Atlas、Incident、可观测性和 Delivery/Verification 在同一语言中形成更强的 CloudOps 领域表达。
3. 所有现有用户能力、真实数据、URL、API、SSE、Evidence、Approval、Delivery、Verification 和安全语义完整保留。
4. 页面围绕用户任务和可读结论组织，不再围绕后端字段、长表单和宽表组织。
5. 所有页面代码完成后先由 Owner 判断视觉。只有 Owner 明确通过，才能开始最后的全面前后端功能联调。

### 1.2 当前最终视觉样例

当前工作树中的以下 13 个未提交文件共同组成 Owner 已认可的样例，不是待废弃原型：

```text
frontend/src/components/agent/AgentConversation.vue
frontend/src/components/agent/AgentHistory.vue
frontend/src/components/agent/AgentInspector.vue
frontend/src/components/agent/GlobalAgentPanel.vue
frontend/src/components/layout/AppHeader.vue
frontend/src/components/layout/AppLayout.vue
frontend/src/components/layout/AppSidebar.vue
frontend/src/components/layout/SidebarMenu.vue
frontend/src/composables/useTheme.ts
frontend/src/style.css
frontend/src/styles/tokens.css
frontend/src/views/agent/AgentWorkspaceView.vue
frontend/src/views/overview/OverviewView.vue
```

实施规则：

- 冻结的是样例的实际视觉、密度、层级、动效和交互结果，不是当前文件结构。
- 允许等效重构并提取 Token、Shell 和共享模式，但不得产生未经说明的可见退化。
- `/overview` 与 `/agent` 后续只做功能完整性补齐、真实数据修复、契约修复和 Owner 明确要求的微调。
- 其他页面必须以样例为直接标尺。不得恢复旧页面骨架，也不得另起第二种设计语言。
- CPA 与文字规范用于补足样例未覆盖的 Settings、列表、表单、弹层、Inspector 和专业工作区模式。

### 1.3 必须覆盖的公开路由

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

同时保留：

- Legacy Atlas Query 到 `/atlas` 的 replace 兼容。
- URL 同步的筛选、分页、时间范围、Tab、selected entity、cursor 与详情身份。
- 刷新、直接进入、前进、后退、关闭 Inspector 后的 scroll 与 Focus 恢复。
- Overview、Alert、Infrastructure、Monitoring、Logs、Traces、Incident、Agent、DevOps 与 Settings 之间的 canonical context links。
- Loading、Empty、Error、Partial、Stale、Disconnected、Permission Denied，以及各领域适用的 conflict、expired、denied、invalid、WebGL failure 等状态。

### 1.4 不可降级能力

以下能力不能因布局、开发速度或最终联调延后而删除、Mock 化或静态化：

- Operational Scope、Provider health、Notification SSE 与主题持久化。
- Kubernetes topology、资源、事件与 Provider context。
- Monitoring 查询、历史、定义和授权表面。
- Logs 搜索/实时模式、虚拟化、完整复制、Evidence 与 Agent 上下文。
- Traces 搜索、Trace 详情、Waterfall、Span 选择、Evidence 与 Agent 上下文。
- Alerts 筛选、详情、Inspector、acknowledge、silence、Incident 与 Agent 入口。
- Agent Consultation/Investigation、消息、SSE、上下文快照、Evidence、Knowledge、Plan、Card、Authority 与 teardown。
- Incident 列表、Inspector、完整生命周期、Evidence、Approval、Delivery、Verification、Resolution 与 SSE。
- DevOps Provider、Candidate、Operation、Authority、Execution、Verification 与 Deployment Baseline。
- Settings 本地 Draft、校验、Diff、Revision、apply、冲突、重试、leave protection、Provider test 与 Secret hygiene。

写操作的 UI、确认、进行中、成功、失败、冲突和重试状态必须实现。真实写入验证仍受第 7.5 节隔离和授权条件约束。

## 2. 技术与视觉合同

### 2.1 保留与重建边界

优先保留并复用：

- Vue Router 路径、Query/hash codec、scroll behavior 与 lazy route。
- typed API client、错误 identity、request/trace metadata 与安全链接校验。
- Pinia Store、SSE 状态机、重连、去重、取消和 teardown。
- 领域 model、exact hash/authority、Evidence provenance、Delivery/Verification 真相。
- Three.js、uPlot、TanStack Vue Virtual 的数据适配与生命周期逻辑。
- 仍能保护以上合同的 unit、route、API、SSE 和 E2E 测试。

允许重建：

- 除当前样例外的所有页面模板、布局、内容编排和页面级样式。
- 旧宽表、长表单、字段堆叠、卡片墙、重复 Toolbar 和无价值空状态。
- 只绑定旧 DOM 或旧视觉、不能保护业务合同的测试。

复用业务逻辑不等于复用旧页面骨架。若逻辑与旧 SFC 强耦合，应先抽离到 typed model、composable、Store 或领域组件，再重新编排页面。

### 2.2 单一 UI 系统

```text
Vue 3 + Vite + TypeScript
Vue Router + Pinia
Nuxt UI 4.10.0 + Tailwind CSS 4.3.3
Lucide
CloudOps Design Token 与业务组合组件
必要的 Three.js / uPlot / TanStack Vue Virtual 专业渲染器
```

硬约束：

- Nuxt UI 是唯一通用组件系统，不引入第二套通用 UI 库。
- 不自行重写 Button、Input、Select、Dialog、Drawer、Tabs、Table、Tooltip、Toast 等成熟基础控件。
- 不手绘 SVG 图标，不使用 emoji；可见图标统一使用 Lucide。
- 专业库只负责 3D、图表、Waterfall、虚拟化等专业渲染，不提供第二套 Button/Form/Dialog/Theme。
- `frontend/src/styles/tokens.css` 是 Primitive raw value 的唯一生产来源。页面只消费 Semantic/Component Token。
- 不为抽象而包裹每一个 Nuxt UI primitive；只提取稳定、跨页面复用的 CloudOps 组合。

### 2.3 视觉与交互合同

- 共享 Shell、Sidebar/Rail、Header、Toolbar、表单、列表、弹层、状态反馈和 Motion 以当前样例及固定 CPA 为准。
- 常规页面保持连续工作区、克制表面层级、小圆角、低噪声边界和高信息密度，不做卡片墙或营销式 Hero。
- CloudOps 专业区域可以使用受控深度、透明层次、数据驱动动画和更强构图，但效果必须表达真实状态、关系或操作反馈。
- 每个可点击控件具备 Default、Hover、Pressed、Focus-visible、Loading、Disabled、Success、Error 中适用的状态。
- 图标按钮使用稳定尺寸和 Tooltip/accessible name；动态文本不得引发布局跳动。
- 技术 ID、UUID、Hash、UTC 和原始 JSON 默认渐进披露，但完整值必须可查看和复制。
- 中文优先；Agent、Incident、Trace、Evidence、Delivery、Verification 等稳定术语可保留英文。

### 2.4 主题、桌面尺寸与动效

每个页面首次实现时就必须写入：

- 暖灰 Light 默认主题与 Dark 等价映射。
- `1024x768`、`1280x800`、`1440x900`、`1920x1080` 的桌面布局约束。
- 125%、150%、200% 缩放下的折叠、长文本、Inspector/Dock 与 overflow 行为。
- 键盘 Focus、Skip Link、ARIA name 与 `prefers-reduced-motion`。

这些合同不能留到最终联调才补。实施期只延后完整验证矩阵，不延后代码实现。手机 Bottom Navigation、手机专用工作流和触摸手势不在范围内。

### 2.5 Settings 的 CPA 一比一规则

`/settings` 是重点页面。视觉、分区、密度、搜索、编辑反馈、Diff 与固定操作区直接对照固定 CPA SHA 的以下源码及实际页面：

```text
src/pages/ConfigPage.tsx
src/pages/ConfigPage.module.scss
src/components/config/VisualConfigEditor.tsx
src/components/config/VisualConfigEditor.module.scss
src/components/config/ConfigSection.tsx
src/components/config/DiffModal.tsx
src/features/providers/ProvidersWorkbenchPage.tsx
src/features/providers/sheets/ProviderSheet.tsx
```

CloudOps 映射规则：

- `/settings` 保持稳定入口；`#providers` 等旧 anchor 继续兼容，并用稳定 hash 支持直达、刷新和前进后退。
- 一次只呈现当前配置分区，不把全部配置铺成一个长页面。
- 五个现有 Draft 分区继续是 `system`、`scopes`、`policies`、`providers`、`secret-references`，Revision history 是独立可发现视图。
- 复刻 CPA 的分区导航、simple/full 渐进披露、搜索跳转、错误计数、Collapsible、悬浮状态/操作区、未保存保护与 Diff 确认。
- Provider 先呈现连接摘要列表，再进入单 Provider 详情；不同时展开所有 Provider 表单。
- 保留 CloudOps 的 validation identity、base revision/hash、conflict/rebase、逐项 Provider/Worker 结果与 fail-closed apply。
- 不照搬 CPA 的 React 或 YAML 数据模型。当前 API 没有无损 raw-source 合同时，不伪造可编辑源码模式；视觉复刻不能改变后端语义。
- 已知 `POST /api/v1/configuration-revisions` 缺少 atomic expected-active-revision compare-and-set，继续记录为 `BACKEND_GAP`，前端 preflight 不能冒充原子保证。

### 2.6 专业渲染器

- Atlas：Three.js 全宽/全高专业场景，Structured View 等价，WebGL failure/context loss/visibility pause/dispose 完整；不把 3D 场景放进装饰卡片。
- Monitoring：uPlot 负责真实时序图，提供同步数值/表格与键盘路径。
- Logs：TanStack Vue Virtual 负责大数据行虚拟化，保留原始行结构、wrap toggle、完整复制和 bounded horizontal scroll。
- Traces：专业 Waterfall 与 Span tree/Inspector，保留稳定坐标、选择和完整属性。

不得用手写 SVG/Canvas 替代成熟专业能力。专业渲染器仍只消费统一 Token。

## 3. Git、工作树与代码所有权

### 3.1 实施前基线

开始代码实施时必须先记录：

```bash
git status --short --branch
git rev-parse HEAD
git diff --stat
git diff --name-status
```

不得假定本文记录的 `dae990b...` 仍是当前 HEAD。Live worktree 优先于历史报告。

当前 13 个样例文件完成第 4.1 节最低校验后，创建独立本地提交：

```text
feat(frontend): freeze accepted CPA exemplar
```

该提交是五条领域线的不可变起点。不得 push。不得把无关 dirty file 混入。

### 3.2 串行共享基础与并行领域线

顺序固定为：

```text
已认可样例基线
-> 等效提取共享 Design System
-> 从同一共享基线创建五条领域线
-> 单一集成线合并与收口
-> Owner 视觉 Gate
-> 最终真实功能联调
```

建议分支与 worktree：

| 责任 | Branch | Worktree | 路由 |
| --- | --- | --- | --- |
| 集成/共享 | `main` 或执行时明确的 integration branch | `/home/monody/k8s/CloudOps-Copilot` | Shell、Overview、Agent、共享系统、最终集成 |
| 资源 | `frontend/final-resources` | `/home/monody/k8s/CloudOps-Copilot-final-resources` | Infrastructure、Atlas、404 |
| 可观测 | `frontend/final-observability` | `/home/monody/k8s/CloudOps-Copilot-final-observability` | Monitoring、Logs、Traces |
| 告警事件 | `frontend/final-alert-incidents` | `/home/monody/k8s/CloudOps-Copilot-final-alert-incidents` | Alerts、Alert detail、Incidents、Incident detail |
| 交付 | `frontend/final-delivery` | `/home/monody/k8s/CloudOps-Copilot-final-delivery` | DevOps、Authority、Delivery、Verification、Baseline |
| 控制 | `frontend/final-settings` | `/home/monody/k8s/CloudOps-Copilot-final-settings` | Settings、Provider、Draft、Diff、Revision、Apply |

五条领域线可以并行，但不能共享同一工作目录，也不能互相 merge/rebase。每条线必须从同一个共享基础提交创建。

### 3.3 集成线独占文件

以下文件或责任只由集成线修改：

```text
frontend/package.json
frontend/package-lock.json
frontend/components.d.ts
frontend/src/main.ts
frontend/src/App.vue
frontend/src/style.css
frontend/src/styles/
frontend/src/components/layout/
frontend/src/components/workspace/ 中的跨领域基础
frontend/src/composables/useTheme.ts
frontend/src/composables/useWorkspaceInspector.ts
frontend/src/composables/useWorkspaceQuery.ts
frontend/src/api/client.ts
frontend/src/router/
frontend/src/navigation.ts
frontend/src/views/overview/
frontend/src/views/agent/
frontend/src/components/agent/
```

领域线需要共享改动时，在 handoff 中给出：问题、消费者、最小接口、拟议测试和是否阻塞。不得直接抢改。集成线可以采纳、重做或拒绝。

### 3.4 提交与 handoff

- 每个可独立回滚的页面或领域能力创建小型本地提交。
- 每次提交前只显式 stage 当前责任文件，检查 `git diff --cached --stat` 与 `git diff --cached --name-status`。
- 不提交 `node_modules`、`dist`、Playwright report、trace、video、临时截图、生成缓存或 scratch 文件。
- 每条线只维护一个简洁 handoff：base SHA、final SHA、路由、文件、实际命令、结果、共享请求、`NOT RUN` 与已知限制。
- 合并使用 `git merge --no-ff`，冲突只由集成线解决。
- 不 push、不创建 PR、不发布、不部署。

## 4. 代码实施阶段

### 4.1 阶段 1：冻结已认可样例

**代码动作**

1. 核对 13 个文件只包含已认可的 Shell、Overview、Agent、Theme 和 Token 切片。
2. 修复阻塞 typecheck、明显 Console error、死路由或样例核心交互缺陷；不得借机重新设计。
3. 确认 Overview、Global Agent Dock 与 `/agent` 共享会话/上下文状态，关闭和卸载会 teardown。
4. 确认样例使用真实 typed client；fixture 只允许出现在测试环境。
5. 建立样例本地基线提交。

**最低校验**

- `git diff --check`。
- 与 Theme、Overview model、Agent API/Store/context 直接相关的 focused Vitest。
- 一次 `npm run typecheck`。
- `/overview` 与 `/agent` 各一次 Chromium `1440x900 Light` smoke：可进入、主内容渲染、一个核心交互可用、无阻塞 Console error。

不运行全量 lint/unit/E2E、Dark/multi-viewport、性能、真实前后端联调或写 E2E。

**退出状态**

```text
ACCEPTED_EXEMPLAR_BASELINE=PASS
OVERVIEW_IMPLEMENTATION=COMPLETE
AGENT_IMPLEMENTATION=COMPLETE
FOCUSED_VALIDATION=PASS
FULL_VALIDATION=NOT RUN
```

### 4.2 阶段 2：等效提取共享 Design System

**代码动作**

1. 从样例提取 Primitive、Semantic、Component Token，清除页面重复 raw value。
2. 固化 Sidebar/Rail、Header actions、Page Frame、Toolbar、status row、dense list、Inspector、Dock、Empty/Error/Skeleton、Technical Details、copy feedback 和 Motion 模式。
3. 保留 Nuxt UI 的键盘、Focus、ARIA 和 Overlay 行为，不创建第二套基础组件。
4. 对样例执行等效重构，确保视觉和交互不漂移。
5. 输出共享组件使用合同与领域线只读基线提交。

**最低校验**

- 只 lint 本阶段实际修改的 TS/Vue 文件。
- 共享 Token、Theme、Workspace、Inspector、Router 与 Agent 生命周期 focused tests。
- 一次 `npm run typecheck`。
- `/overview` 与 `/agent` Chromium `1440x900 Light` 对照 smoke。
- 若依赖、lockfile、Vite 或构建入口变化，才追加 `npm run build`、依赖树与 audit；否则 `NOT RUN`。

**退出状态**

```text
SHARED_DESIGN_SYSTEM=PASS
EXEMPLAR_EQUIVALENCE=PASS
PARALLEL_BASELINE_READY=YES
```

### 4.3 阶段 3：五条领域线实现

所有领域线必须完成生产代码，不得用 fixture、静态 JSON、假按钮或 TODO 交付。视觉检查使用当前样例和固定 CPA，不需要中途等待 Owner 审批。

#### 4.3.1 资源线

**主要文件**

```text
frontend/src/views/infrastructure/
frontend/src/views/atlas/
frontend/src/components/infrastructure/
frontend/src/api/infrastructure.ts
frontend/src/theme/atlasTheme.ts
frontend/src/pages/NotFoundPage.vue
```

**实现**

- Infrastructure 从资源检索器重建为资源态势工作台：Scope/同步/数量、健康摘要、异常资源优先、真实 Kind 导航、搜索/高级筛选、双行 dense list 与 Inspector。
- 保留 topology/resources/detail/events typed API、cluster/namespace/kind/search/time/resource URL 与 Monitoring/Logs/Traces/Agent context。
- Atlas 使用可用工作区的 full-bleed Three.js 场景，保留 Structured 等价视图、选择、deep link、resize、context loss、WebGL fallback、visibility pause 与 dispose。
- 旧 `/overview?view=atlas|canvas|structured&resource=...` 保持 replace 归一化；Router 变化交给集成线。
- 404 保留未知原路径、恢复入口和样例视觉，不做营销页。

**最低校验**

- infrastructure model、Atlas lifecycle/theme、legacy query focused tests。
- 一次 typecheck。
- `/infrastructure`、`/atlas`、404 各一次 Chromium `1440x900 Light` smoke。
- Atlas smoke 必须包含非空 Canvas 像素检查和 Structured fallback；完整性能/内存矩阵延后。

#### 4.3.2 可观测线

**主要文件**

```text
frontend/src/views/monitoring/
frontend/src/views/logs/
frontend/src/views/traces/
frontend/src/components/monitoring/
frontend/src/components/logs/
frontend/src/components/traces/
frontend/src/api/monitoring.ts
frontend/src/api/telemetry.ts
frontend/src/models/telemetry.ts
```

**实现**

- Monitoring 首屏以指标值、趋势和异常为主；高频资源/指标/时间在 Toolbar，高级参数渐进披露；可视化查询/PromQL 使用明确 segmented mode。
- uPlot 保持稳定坐标，展示当前值、峰值、变化率与关联资源；历史、定义、授权和 Agent 操作进入上下文区域。
- Logs 明确区分搜索与实时模式，提供主搜索、字段筛选、时间分布、虚拟日志流、关键词高亮、JSON 展开、wrap、完整复制、Evidence 与 Trace/Agent context。
- Traces 提供高频搜索、Trace 队列、全宽 `trace_id` 详情、Waterfall、Span tree/Inspector、Tags/events/resources 与 Logs/Monitoring/Agent context。
- 保留 canonical `resource` 与 legacy `workload` 输入、history/execution identity、stale cancellation 和 Provider links。

**最低校验**

- monitoring/telemetry model、route、presentation、virtualization 和 stale cancellation focused tests。
- 一次 typecheck。
- `/monitoring`、`/logs`、`/traces` 各一次 Chromium `1440x900 Light` smoke。
- 专业图表只检查可见、非空、选择稳定和无阻塞 Console；大数据与性能矩阵延后。

#### 4.3.3 告警事件线

**主要文件**

```text
frontend/src/views/alerts/
frontend/src/views/incidents/
frontend/src/components/alerts/
frontend/src/components/incidents/
frontend/src/composables/incidents/
frontend/src/api/alerts.ts
frontend/src/api/incidents.ts
frontend/src/models/incidents.ts
frontend/src/models/workbench.ts
frontend/src/models/recovery.ts
frontend/src/models/commands.ts
```

**实现**

- Alerts 使用可扫描双行队列和 Inspector，不再以多列宽表为唯一形态；高频状态筛选与搜索在前，高级条件折叠。
- Alert 详情保留信号、关联 Incident、Agent、活动时间线、acknowledge、silence、create/attach Incident 和 investigation 入口。
- Incidents 使用工作队列、状态结论和生命周期 Inspector；完整详情围绕触发、调查、Evidence、方案、审批、交付、验证和 Resolution 组织。
- Incident 是 incident-owned Approval/Delivery/Verification 唯一操作面。Overview canonical `/incidents?selected=<id>` 与 Back/Focus 恢复保持完整。
- SSE 保留 bounded reconnect、cursor/`Last-Event-ID`、refresh coalescing、dedupe、stale/disconnected 与 teardown，不能回到每事件一次请求风暴。
- exact authority/hash、Idempotency-Key、accepted/dispatched/observed/verified 区分和 fail-closed confirmation 不得弱化。

**最低校验**

- Alerts API/route/context、Incident list/detail/realtime/models/commands focused tests。
- 一次 typecheck。
- `/alerts`、Alert 详情、`/incidents`、Incident 详情各一次 Chromium `1440x900 Light` smoke。
- 写命令只验证 UI 状态与 deterministic fixture 合同；真实副作用 `NOT RUN`。

#### 4.3.4 交付线

**主要文件**

```text
frontend/src/views/devops/
frontend/src/api/devops.ts
frontend/src/stores/devOpsWorkspace.ts
```

**实现**

- 页面围绕 `Provider -> Change -> Candidate -> Operation -> Authority -> Execution -> Verification -> Deployment Baseline` 因果链组织。
- 首屏优先待审批、执行中、失败、冻结与当前 Active Baseline，不把 Provider ID、Hash 和内部枚举作为主视觉。
- Provider 展示连接摘要，诊断渐进披露；Authority Queue 使用人类可读操作、风险、Owner、阶段与下一步。
- Delivery identity 展示 Source、Image、GitOps、observed deployment 和 Verification 的关系，历史 baseline 折叠并支持 Diff/来源跳转。
- Incident-owned 或 ownership unknown 的 subject 必须 fail closed 并链接 Incident；DevOps 只拥有 global/non-incident 操作面。
- execute/authorize 保留 expected hash、身份、风险后果和失败恢复。
- 交付线只能消费告警事件线公开的 Incident 只读组件或类型；需要修改时提交 shared change request，不直接编辑 `frontend/src/components/incidents/`。

**最低校验**

- DevOps ownership/store、exact-hash 和 context-link focused tests。
- 一次 typecheck。
- `/devops` 主工作区、detail Query 与 Inspector 一次 Chromium `1440x900 Light` smoke。
- 授权与执行真实写入 `NOT RUN`。

#### 4.3.5 控制线

**主要文件**

```text
frontend/src/views/settings/
frontend/src/api/platform.ts
frontend/src/components/settings/（按需要新增）
```

**实现**

- 按第 2.5 节逐项对照固定 CPA Settings，不继承当前长页面布局。
- 当前分区一次只显示一组配置；提供 section navigation、simple/full、搜索跳转、错误数量、Collapsible 与固定 status/action island。
- System/Scopes/Policies/Providers/Secret References/Revision History 使用统一 Setting Row 与人类单位。
- Provider 使用集成列表和单项详情；测试、secret reference 和连接结果不与普通配置混在一张长表单。
- 五个独立 Draft 保留各自 baseline/fingerprint/validation；切换分区不丢失草稿。
- Diff、validation、apply confirmation、revision drift、rebase/discard/retry、itemized result 和 leave guard 完整。
- Secret 原始值不回显、不进入日志、URL、截图或状态；Provider link 和 endpoint 继续安全校验。

**最低校验**

- settingsDraft、platform typed client、section/hash、leave guard、conflict/rebase focused tests。
- 一次 typecheck。
- `/settings` 默认分区、`#providers`、Draft -> Diff 视觉流程各一次 Chromium `1440x900 Light` fixture smoke；不发真实 apply/test/secret 请求。

### 4.4 页面线统一完成条件

一条领域线只有同时满足以下条件才能报告：

```text
IMPLEMENTATION=COMPLETE
FOCUSED_VALIDATION=PASS
FULL_VALIDATION=NOT RUN
REAL_FUNCTION_INTEGRATION=NOT RUN
```

完成条件：

- 生产 typed API、Router、SSE 和领域状态已接入。
- 所有可见控件有真实行为，无死按钮、假成功、静态占位或 TODO。
- 适用的 Loading/Empty/Error/Partial/Stale/Disconnected/Permission Denied 已实现。
- URL、刷新、Back/Forward、Inspector、Agent context 与跨页链接完整。
- Light/Dark、桌面尺寸、长文本、Focus 和 reduced motion 已写入代码。
- 无 Element Plus、旧视觉骨架、未映射能力、生产 fixture 或第二套 UI 系统。
- focused tests、一次 typecheck 和逐路由 `1440x900 Light` smoke 通过。
- 工作树干净，handoff 完整，未修改其他领域或共享 owner 文件。

## 5. 集成与 Owner 视觉 Gate

### 5.1 阶段 4：单一集成线收口

按资源、可观测、告警事件、交付、控制的顺序 `merge --no-ff`。每合并一条线：

1. 解决冲突和 shared change request。
2. 运行一次 typecheck。
3. 只 smoke 新合入路由和直接跨页消费者。
4. 创建集成本地提交。

全部合并后：

- 统一 Router、navigation、Token、shared components、context links、术语和状态文案。
- 清理失效页面、重复样式、孤儿组件、旧视觉选择器和生成声明。
- 运行 `npm run typecheck`、`npm run build`、零残留扫描和所有公开路由的 Chromium `1440x900 Light` 最低 smoke。
- 不运行完整 lint/unit/E2E、多浏览器、性能、可访问性或真实前后端联调。

退出状态：

```text
ALL_PAGE_IMPLEMENTATION=COMPLETE
ALL_ROUTE_FOCUSED_SMOKE=PASS
OWNER_VISUAL_ACCEPTED=NOT RUN
REAL_FUNCTION_INTEGRATION=NOT RUN
```

### 5.2 阶段 5：Owner 唯一视觉验收

进入条件是第 5.1 节全部通过。集成线必须启动一个 Owner 可访问的完整站点并提供 URL，不只提交截图。

视觉数据允许使用隔离、确定性、真实结构的测试数据，以稳定展示：

- 正常、Loading、Empty、Error、Partial、Stale、Disconnected、Permission Denied。
- 长文本、长名称、Hash/UUID、密集列表、图表、Inspector、Dock、Modal 和 Diff。
- 当前样例和五条领域线的完整路由。

fixture 只能存在于 Playwright/本地视觉预览环境，不进入生产包，不得作为真实 Provider 证据。

自动化只负责发现空白、重叠、裁切、死交互、Console error 和明显漂移。最终视觉结论只由 Owner 给出：

```text
OWNER_VISUAL_ACCEPTED=PASS
```

若 Owner 不接受：

1. 保持在本阶段。
2. 按 Owner 反馈修改共享或领域实现。
3. 重跑受影响 focused tests/typecheck/smoke。
4. 重新启动完整站点供 Owner 判断。

在 Owner 明确给出 `PASS` 前，禁止开始第 6 节真实功能联调。

## 6. 最终全面前后端功能联调

### 6.1 定位

这是所有页面实现与 Owner 视觉通过后的最后一步，只证明前后端功能正常并用真实数据修复前端缺陷。它不是发布就绪、供应链、全浏览器、长时性能或生产写入认证。

### 6.2 真实运行

优先使用仓库受控生命周期：

```bash
make local-doctor
make local-up
make local-status
```

默认后端入口是 `http://127.0.0.1:18080`。端口变化时使用当前 `CLOUDOPS_LOCAL_PORT`，不得假定历史端口仍可用。前端开发服务器通过 `VITE_API_PROXY_TARGET` 指向当前 loopback 后端。

开始前记录：

- 当前 Git SHA 与 dirty status。
- kind context、Namespace、Helm release、API ready、schema 与 active configuration revision。
- Provider available/partial/unavailable/disabled identity。
- 数据分类、只读 guard 与写入授权状态。

### 6.3 全路由读链路

对第 1.3 节全部公开路由执行真实：

```text
Browser UI -> current /api/v1 -> current persistence/Provider projection
```

至少覆盖：

- 直接进入、刷新、Back/Forward、legacy query 与 404 恢复。
- Scope、Provider health、Notification SSE、Theme 与 Global Agent 生命周期。
- Overview -> Incident Inspector -> detail -> Back -> Focus restore。
- Overview/Alert/Incident/telemetry/infrastructure -> Agent context。
- Infrastructure/Atlas -> Kubernetes topology/resource/detail/event。
- Monitoring/Logs/Traces 的历史、结果、选择、专业渲染与 context links。
- Alerts list/detail、Incident list/detail、SSE、Evidence/Delivery/Verification 只读投影。
- DevOps operation/detail/baseline 与 Incident ownership 跳转。
- Settings active revision、各分区、Provider、storage、history 与 `#providers`。
- 实际 Console、Network、HTTP、API error identity 与 layout blocking defects。

### 6.4 收敛循环

“进行一次全面联调”指一个集中收敛阶段，不是只跑一遍：

1. 首轮全路由与关键跨页流程审计。
2. 立即修复所有前端代码、字段、状态、交互、Console/Network 和真实数据适配缺陷。
3. 必要后端合同缺口记录 `BACKEND_GAP`，不在本轮越权修改后端。
4. 重跑受影响路由与消费者。
5. 最后再做一次整站功能回归。

只有前端阻塞缺陷清零，剩余项都被如实归类，才能结束。

### 6.5 写链路

真实写链路只有同时具备以下条件才可运行：

- 明确隔离目标。
- 受限凭据和单独 Owner 授权。
- 初始 identity/hash/revision 证据。
- 可验证 cleanup 和最终状态恢复。
- 不会作用于 staging、production 或未经批准的 Provider。

条件不全时：

```text
WRITE_PATH_E2E=NOT RUN
```

UI 与 fixture contract 测试不能替代真实写入证据，也不阻塞本轮前端功能完成。

### 6.6 本阶段明确不运行

除非 Owner 后续单独授权，以下仍为 `NOT RUN`：

- 全量 lint、全量 unit、完整 Playwright E2E 与 release suite。
- Firefox、WebKit、完整 Light/Dark/multi-viewport/zoom 自动矩阵。
- Lighthouse、bundle 重测、10k logs、2.5k spans、20k table、SSE soak、内存和长时性能。
- 依赖安全、供应链、镜像、签名、PR、hosted CI、staging、production 与发布验证。

## 7. 验证选择规则

### 7.1 每阶段唯一最低校验

| 变更 | 必须运行 | 不默认运行 |
| --- | --- | --- |
| 页面模板/样式 | 改动路由 1440x900 Light smoke、相关文件 lint、typecheck | 全路由、多 viewport、Dark、全 E2E |
| Router/URL/Inspector | codec/scroll/focus focused tests、直接消费者 smoke | 无关页面套件 |
| API/SSE/Store | 相关 contract/lifecycle tests、typecheck、一个核心交互 smoke | 全 unit、真实写入 |
| Token/Shell/共享组件 | 样例对照 smoke、直接消费者 focused tests | Owner 中途视觉 Gate |
| 依赖/lockfile/Vite | build、依赖树、必要 audit/budget | 无关后端测试 |
| Three.js/uPlot/virtualization | lifecycle/model test、非空渲染与选择 smoke | 完整性能/大数据矩阵 |

阶段报告必须写实际命令、选择理由、结果和未运行项。没有运行的项保持 `NOT RUN`。

### 7.2 禁止的证据提升

- fixture 不能证明真实 MySQL、API、SSE 或 Provider。
- 旧 Gate 12A 的 `PASS` 不能证明新页面实现。
- 当前 CPA 截图不能证明 CloudOps 功能。
- typecheck/build 不能证明路由行为或视觉。
- Owner 视觉 `PASS` 不能证明真实前后端功能。
- 最终读链路 `PASS` 不能证明写链路、发布或生产就绪。

## 8. 交付物与最终状态

### 8.1 代码交付物

- 已认可样例基线提交。
- 等效共享 Design System 与 Shell 提取提交。
- 五条领域线的本地小提交与 handoff。
- 单一集成提交序列。
- Owner 视觉反馈修正提交。
- 最终真实联调前端修复提交。

### 8.2 最小文档交付物

```text
docs/evidence/frontend-final-rebuild/baseline.md
docs/evidence/frontend-final-rebuild/shared-system.md
docs/evidence/frontend-final-rebuild/lanes/<lane>.md
docs/evidence/frontend-final-rebuild/owner-visual-review.md
docs/evidence/frontend-final-rebuild/final-function-integration.md
```

文档只记录 identity、代码范围、命令、结果、缺口和回滚点，不建立实施期庞大截图/trace/performance 证据包。

### 8.3 本轮完成状态

成功结束时必须如实输出：

```text
ACCEPTED_EXEMPLAR_BASELINE=PASS
SHARED_DESIGN_SYSTEM=PASS
ALL_PAGE_IMPLEMENTATION=COMPLETE
ALL_ROUTE_FOCUSED_SMOKE=PASS
OWNER_VISUAL_ACCEPTED=PASS
REAL_FUNCTION_INTEGRATION=PASS
FRONTEND_BLOCKING_DEFECTS=0
WRITE_PATH_E2E=PASS|NOT RUN
BACKEND_GAP=<NONE|explicit identifiers>
FULL_RELEASE_VALIDATION=NOT RUN
FRONTEND_RELEASE_READY=NOT_ASSESSED
EXTERNAL_DELIVERY=NOT RUN
```

若真实 Provider 或必要后端事实不可用，受影响项必须为 `NOT RUN` 或 `BACKEND_GAP`。不得为了得到整齐的 `PASS` 使用 fixture、假数据、静态交互或未经授权的写入。

## 9. 停止与回滚规则

- focused check `FAIL` 时，只停止依赖当前失败的工作，不扩大执行范围。
- 领域线不能通过修改共享文件绕过问题；提交 shared change request。
- Owner 视觉未通过时停在第 5.2 节持续修正。
- 最终联调发现前端缺陷时修复并重跑，不回到重新设计阶段，除非 Owner 明确要求。
- 后端缺口不在前端伪造；记录 `BACKEND_GAP` 后继续其他不依赖路径。
- 每个阶段以本地提交作为回滚点。禁止使用 `git reset --hard`、`git clean` 或覆盖当前 dirty worktree。
- 未获得新授权，不 push、不创建 PR、不发布、不部署、不触发外部 Provider 写入。

## 10. 执行顺序摘要

```text
1. 重新读取本方案、vue.md、CPA redesign spec、live worktree
2. 记录 HEAD/status，验证并提交当前 13 文件样例基线
3. 等效提取共享 Design System，提交并冻结并行基线
4. 创建五个独立 branch + worktree
5. 并行实现资源、可观测、告警事件、交付、控制
6. 每线 focused validation、clean handoff、本地提交
7. 集成线按顺序 merge --no-ff，闭合共享请求与跨页合同
8. typecheck/build/零残留/全路由 1440 Light 最低 smoke
9. 启动完整 fixture 视觉预览，由 Owner 唯一判定
10. Owner 未通过则持续修正；通过后才进入真实联调
11. 全面真实 UI -> API -> Provider 功能联调并循环修复
12. 输出 PASS/FAIL/NOT RUN/BACKEND_GAP 与本地最终提交
13. 停止，不 push、不发布
```

本方案的核心不是再次规划视觉，而是以已认可样例为固定方向，尽快完成全部生产页面代码，在 Owner 视觉通过后用真实前后端事实完成最后收敛。
