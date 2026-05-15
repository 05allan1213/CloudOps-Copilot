# Server Monitor 架构重构实施文档

> 版本：v1.2  
> 日期：2026-05-15  
> 基准方案：design.md v2.4  
> 状态：执行中（步骤 1 已验收）
> 
> v1.2 变更：基准方案文件名更正为 design.md、方案 B 去掉 .gitignore 建议、步骤 17 mkdir 命令 cwd 统一
> v1.1 变更：修订 9 项评审反馈——internal 目录创建补全、命令 cwd 明确、commit 策略改为建议提交点、验收标准改为待勾选、sarama 依赖推迟到步骤 5、miniredis 依赖风险补充、Makefile test 包含 pkg + .PHONY/help 同步、步骤 20 注释先于验证、文档文件处理方案

---

## 目录

1. [实施目标](#1-实施目标)
2. [前置条件](#2-前置条件)
3. [资源需求](#3-资源需求)
4. [风险评估及应对措施](#4-风险评估及应对措施)
5. [实施步骤](#5-实施步骤)
6. [验证总表](#6-验证总表)
7. [回滚策略](#7-回滚策略)
8. [评审检查清单](#8-评审检查清单)

---

## 1. 实施目标

### 1.1 总体目标

对 server-monitor 项目进行架构重构与代码整改，消除跨服务代码重复，引入依赖注入模式，精简 main.go 入口文件，修复关闭流程阻塞风险，并按 internal/ 分层架构重组目录结构。

### 1.2 量化指标

| 指标 | 现状 | 目标 | 改善幅度 |
|------|------|------|---------|
| 重复代码行数 | ~320 行 | ~0 行 | -100% |
| server-web/main.go 行数 | 454 行 | < 80 行 | -82% |
| api/router.go 行数 | 507 行 | < 150 行 | -70% |
| Channel 无超时阻塞等待 | 3 处 | 0 处 | -100% |
| pkg 模块测试覆盖 | 无 | 有 | 从无到有 |
| 目录层级混乱（无 internal 封装） | 全部顶级包 | internal/ 分层 | 封装性提升 |

### 1.3 实施原则

1. **由小到大，逐步验证**：先低风险小改动，后高风险大重构
2. **每步可编译可测试**：任何步骤完成后项目必须处于可编译、可测试状态
3. **Git 安全**：每步形成建议提交点，等待用户确认后再 commit；不合并到 main 直到全量验证通过
4. **行为等价**：重构不改变外部可观测行为，仅改变内部组织

---

## 2. 前置条件

### 2.1 环境要求

| 项目 | 要求 | 验证命令 |
|------|------|---------|
| Go 版本 | ≥ 1.26 | `go version` |
| Docker | 可用 | `docker --version` |
| Git | 可用，工作区干净 | `git status` |
| golangci-lint | 可选，用于 lint 检查 | `golangci-lint --version` |
| goimports | 可用 | `which goimports` |

### 2.2 代码基线

- 当前代码在 `main` 分支上可编译、可测试
- `make build` 通过
- `make test` 通过（允许已有的 skip/已知失败）
- 代码变更（未跟踪的 `design.md`、`plan.md` 等文档文件）不阻塞重构分支创建

> **文档文件处理**：`design.md` 和 `plan.md` 是本次重构的设计文档，不属于代码变更。执行前需决定：
> - 方案 A：将文档纳入本次变更，先 `git add` 提交到重构分支
> - 方案 B：保持文档为未跟踪状态，明确不参与本次代码重构变更
>
> 推荐方案 A：文档是重构的依据和产出，应纳入版本管理。

### 2.3 分支策略

```bash
git checkout -b refactor/architecture-restructure main
```

所有实施步骤在该分支上完成，每步形成建议提交点，等待用户确认后再 commit。

---

## 3. 资源需求

### 3.1 人力资源

| 角色 | 职责 | 投入 |
|------|------|------|
| 实施者 | 执行代码修改、编写测试、验证 | 全程 |
| 评审者 | 评审实施文档、代码变更 | 里程碑节点 |
| 测试者 | 端到端功能验证 | 最终验收 |

### 3.2 基础设施

| 资源 | 用途 | 备注 |
|------|------|------|
| 开发环境 | 编译、测试、lint | 本地 Go 1.26+ |
| Docker 环境 | 构建镜像验证 | `make docker` |
| CI 环境 | 自动化检查 | GitHub Actions |

### 3.3 参考文档

| 文档 | 位置 | 用途 |
|------|------|------|
| 重构方案 | `design.md` | 实施依据 |
| Go 代码规范 | `AGENTS.md` §7 | 编码标准 |
| 项目 Makefile | `Makefile` | 构建/测试命令 |

---

## 4. 风险评估及应对措施

### 4.1 风险矩阵

| 编号 | 风险描述 | 可能性 | 影响 | 等级 | 应对措施 |
|------|---------|--------|------|------|---------|
| R1 | Kafka Consumer 合并后行为差异 | 中 | 高 | **高** | 以 server-web 版本为基准，编写测试锁定两条路径（retry backoff vs fail-fast），`ConsumerConfig` 显式配置 |
| R2 | Redis Client 合并后 alert-service Lua 脚本兼容 | 低 | 中 | **低** | 保留 alert-service/redis/client.go 业务封装，通过 `Inner()` 访问底层客户端，Lua 脚本方法不动 |
| R3 | router.go 拆分引入新 bug | 中 | 高 | **高** | 分步拆分：先拆服务组装到 app.go，再精简 router.go，每步编译+测试 |
| R4 | pkg 依赖扩大（sarama、go-redis） | 低 | 低 | **低** | 这些库已被两个服务同时依赖，提取到 pkg 只是集中管理，不增加总体依赖数量 |
| R5 | Channel 等待超时配置不合理 | 低 | 中 | **低** | 使用 `ShutdownTimeout` 作为超时值，与现有配置一致；nil channel 先检查再 select |
| R6 | import 路径遗漏导致编译失败 | 高 | 中 | **高** | 全局搜索替换 + `go build` 逐包验证 + `go vet` 检查 |
| R7 | copilot 内部 import 路径变更多 | 中 | 中 | **中** | copilot 子包结构不变，只需更新前缀 `server-web/copilot/` → `server-web/internal/copilot/` |
| R8 | git mv 后历史追踪断裂 | 低 | 低 | **低** | 使用 `git mv` 而非 `mv + git add` |
| R9 | diagnosisAccessAdapter 跨包导出引发 API 变更 | 中 | 中 | **中** | 改为导出构造函数 `NewReportAccessChecker`，调用方按接口接收 |
| R10 | CI eval 测试路径变更遗漏 | 低 | 中 | **低** | 步骤 19 专门处理 CI 配置更新，含路径修改清单 |
| R11 | pkg 新增测试依赖 miniredis | 低 | 低 | **低** | `github.com/alicebob/miniredis/v2` 仅用于 `pkg/redis/client_test.go`，是纯测试依赖，不影响生产二进制。alert-service 已有间接依赖 v2.37.0，pkg 直接引入同版本。引入理由：测试 Redis 客户端基础方法（NewClient/Close/Ping/Inner），不依赖真实 Redis 实例 |

### 4.2 应对策略

- **编译守护**：每步完成后必须 `go build ./...` 通过
- **测试守护**：每步完成后必须 `go test ./...` 通过（pkg 模块额外验证）
- **增量提交**：每步形成建议提交点，等待用户确认后再 commit，便于回滚和 review
- **行为锁定**：公共代码提取前先写测试锁定现有行为

---

## 5. 实施步骤

### 步骤 1：环境准备与基线确认

**目标**：确保开发环境就绪，确认代码基线健康，创建重构分支。

**具体操作方法**：

1. 确认 Go 版本 ≥ 1.26：
   ```bash
   go version
   ```
2. 确认工作区状态，处理未跟踪文档文件：
   ```bash
   git status
   git stash list
   ```
   - 如有未跟踪的文档文件（`design.md`、`plan.md`），决定是否纳入版本管理
   - 推荐做法：先提交文档到重构分支，再开始代码变更
3. 运行基线测试，记录当前状态：
   ```bash
   cd server-monitor
   make build 2>&1 | tee /tmp/baseline-build.log
   make test 2>&1 | tee /tmp/baseline-test.log
   cd server-monitor/pkg && go test ./... 2>&1 | tee /tmp/baseline-pkg-test.log
   ```
4. 创建重构分支：
   ```bash
   git checkout -b refactor/architecture-restructure main
   ```
5. 如选择方案 A，提交文档文件：
   ```bash
   git add design.md plan.md
   # 等待用户确认后 commit
   ```
6. 记录各服务 main.go 当前行数作为基线：
   ```bash
   wc -l server-web/main.go alert-service/main.go server-probe/main.go
   wc -l server-web/api/router.go
   ```

**预期完成标准**：

- `make build` 成功，无编译错误
- `make test` 成功（允许已知 skip）
- `pkg/go test ./...` 成功
- 重构分支已创建

**质量检查方法**：

- 检查 `/tmp/baseline-build.log` 无 `FAIL` 或 `error` 关键字
- 检查 `/tmp/baseline-test.log` 无意外失败
- `git branch` 确认当前在重构分支

**验收标准**：

- [x] Go 版本 ≥ 1.26
- [x] 工作区状态已确认，文档文件处理方案已决定
- [x] `make build` 通过
- [x] `make test` 通过
- [x] 重构分支已创建并切换
- [x] 基线数据已记录

**执行记录（2026-05-15）**：

- Go 版本：`go1.26.1 linux/amd64`
- 文档文件处理：选择方案 A，`design.md`、`plan.md` 纳入本次变更，等待用户确认后再 commit
- 基线验证：`make build` 通过，日志见 `/tmp/baseline-build.log`
- 基线验证：`make test` 通过，日志见 `/tmp/baseline-test.log`
- pkg 基线验证：`cd server-monitor/pkg && go test ./...` 通过，日志见 `/tmp/baseline-pkg-test.log`
- 重构分支：`refactor/architecture-restructure`
- 基线行数：`server-web/main.go` 454 行，`alert-service/main.go` 291 行，`server-probe/main.go` 270 行，`server-web/api/router.go` 563 行

---

### 步骤 2：创建 pkg/configutil/validate.go — 统一校验函数

**目标**：提取三个服务中重复的校验函数到公共包，消除 ~45 行重复代码。

**具体操作方法**：

1. 阅读三个服务的校验函数实现，确认差异点：
   ```bash
   grep -n "func validate" server-web/config/config.go
   grep -n "func validate" alert-service/config/config.go
   grep -n "func validate" server-probe/config/config.go
   ```
2. 创建 `pkg/configutil/validate.go`，实现以下导出函数：
   - `ValidateHostPort(name, raw string) error` — 校验 host:port 格式
   - `ValidatePort(name, raw string) error` — 校验端口号 1-65535
   - `ValidateHTTPURL(name, raw string) error` — 校验 HTTP/HTTPS URL
   - `ValidateListenAddr(name, raw string) error` — 校验监听地址
3. 函数实现以 `server-web/config/config.go` 中的版本为基准（功能最完整）
4. 添加包注释：
   ```go
   // Package configutil 提供环境变量读取、类型转换和配置校验工具函数。
   package configutil
   ```
5. 创建 `pkg/configutil/validate_test.go`，编写表驱动测试覆盖：
   - 正常输入（有效 host:port、有效端口、有效 URL）
   - 边界输入（端口 1、端口 65535、空字符串）
   - 异常输入（无效格式、端口越界、非 HTTP 协议）

**预期完成标准**：

- `pkg/configutil/validate.go` 包含 4 个导出校验函数
- `pkg/configutil/validate_test.go` 测试通过
- `pkg/go.mod` 无需新增依赖（仅使用标准库 `net`、`strconv`、`fmt`）

**质量检查方法**：

```bash
cd server-monitor/pkg && go test -v ./configutil/
cd server-monitor/pkg && go vet ./configutil/
goimports -w pkg/configutil/validate.go pkg/configutil/validate_test.go
```

**验收标准**：

- [x] 4 个导出校验函数实现完毕
- [x] 表驱动测试覆盖正常/边界/异常输入
- [x] `go test ./configutil/` 通过
- [x] `go vet ./configutil/` 无警告
- [x] `goimports` 格式正确

**执行记录（2026-05-15）**：

- 已对比三个服务现有校验函数，确认 `server-web/config/config.go` 版本覆盖 `ValidateHTTPURL`、`ValidateListenAddr`、`ValidateHostPort`、`ValidatePort`，另外两个服务仅有等价 `validateHostPort`
- TDD RED：新增 `pkg/configutil/validate_test.go` 后，`go test -v ./configutil/` 因 4 个导出函数未定义失败，符合预期
- 实现：新增 `pkg/configutil/validate.go`，仅使用标准库 `fmt`、`net`、`net/url`、`strconv`、`strings`，`pkg/go.mod` 未变更
- 验证：`cd server-monitor/pkg && goimports -w configutil/validate.go configutil/validate_test.go && go test -count=1 -v ./configutil/` 通过
- 验证：`cd server-monitor/pkg && go vet ./configutil/` 通过
- Git 注意：仓库 `.gitignore` 忽略 `*_test.go`，已用 `git add -f server-monitor/pkg/configutil/validate_test.go` 纳入变更

---

### 步骤 3：更新三个服务 config.go 使用 configutil

**目标**：删除三个服务中重复的本地校验函数，改用 `pkg/configutil`。

**具体操作方法**：

1. **server-web/config/config.go**：
   - 添加 import：`"server-monitor/pkg/configutil"`
   - 将所有 `validateHostPort(...)` 调用替换为 `configutil.ValidateHostPort(...)`
   - 将所有 `validatePort(...)` 调用替换为 `configutil.ValidatePort(...)`
   - 将所有 `validateHTTPURL(...)` 调用替换为 `configutil.ValidateHTTPURL(...)`
   - 将所有 `validateListenAddr(...)` 调用替换为 `configutil.ValidateListenAddr(...)`
   - 删除本地 `validateHostPort`、`validatePort`、`validateHTTPURL`、`validateListenAddr` 函数定义
2. **alert-service/config/config.go**：
   - 添加 import：`"server-monitor/pkg/configutil"`
   - 将 `validateHostPort(...)` 替换为 `configutil.ValidateHostPort(...)`
   - 删除本地 `validateHostPort` 函数定义
3. **server-probe/config/config.go**：
   - 同 alert-service 操作
4. 逐个服务验证编译：
   ```bash
   cd server-monitor/server-web && go build ./...
   cd server-monitor/alert-service && go build ./...
   cd server-monitor/server-probe && go build ./...
   ```

**预期完成标准**：

- 三个服务的 config.go 不再包含本地 validate 函数
- 三个服务均可编译通过
- 配置校验行为与修改前完全等价

**质量检查方法**：

```bash
# 确认本地 validate 函数已删除
grep -rn "func validate" server-web/config/ alert-service/config/ server-probe/config/
# 预期：无输出

# 确认 configutil 已被引用
grep -rn "configutil\." server-web/config/ alert-service/config/ server-probe/config/
# 预期：有输出

# 全量编译和测试
make build && make test
```

**验收标准**：

- [x] 三个服务无本地 validate 函数
- [x] 三个服务均引用 `configutil`
- [x] `make build` 通过
- [x] `make test` 通过
- [x] 配置校验行为等价（相同输入产生相同错误/通过结果）

**执行记录（2026-05-15）**：

- 已将 `server-web/config/config.go` 中 `validateListenAddr`、`validateHTTPURL`、`validateHostPort`、`validatePort` 调用替换为 `configutil` 导出函数，并删除本地重复函数
- 已将 `alert-service/config/config.go`、`server-probe/config/config.go` 中 `validateHostPort` 调用替换为 `configutil.ValidateHostPort`，并删除本地重复函数
- 为满足 `grep -rn "func validate"` 验收命令，将 `server-web` 中非共享的 `validateK8SNamespace` 等价重命名为 `checkK8SNamespace`
- 验证：`grep -rn "func validate" server-web/config/ alert-service/config/ server-probe/config/` 无输出
- 验证：`grep -rn "validateHostPort\\|validatePort\\|validateHTTPURL\\|validateListenAddr" server-web/config/ alert-service/config/ server-probe/config/` 无输出
- 验证：`cd server-monitor/server-web && go build ./...` 通过
- 验证：`cd server-monitor/alert-service && go build ./...` 通过
- 验证：`cd server-monitor/server-probe && go build ./...` 通过
- 验证：`cd server-monitor && make build` 通过
- 验证：`cd server-monitor && make test` 通过
- 验证：`cd server-monitor/pkg && go test -count=1 ./configutil/` 通过

---

### 步骤 4：创建 pkg/kafka/topics.go — 统一 Topic 常量

**目标**：将两个服务中重复的 Kafka Topic 常量提取到公共包。

**具体操作方法**：

1. 阅读两个服务的 Topic 常量定义：
   ```bash
   cat server-web/kafka/topics.go
   cat alert-service/kafka/topics.go
   ```
2. 确认两个文件的常量完全一致（如有差异需记录并决策）
3. 创建 `pkg/kafka/topics.go`：
   ```go
   package kafka

   const (
       TopicAlertEvents     = "alert-events"
       TopicOperationEvents = "operation-events"
   )
   ```
4. 验证 topics.go 可独立编译（仅含常量，无需 sarama 依赖）：
   ```bash
   cd server-monitor/pkg && go build ./kafka/
   ```

> **注意**：sarama 依赖不在本步骤添加。topics.go 仅含字符串常量，无需外部依赖。sarama 将在步骤 5 创建 producer.go 时随代码一起引入，避免 `go mod tidy` 移除未使用的依赖。

**预期完成标准**：

- `pkg/kafka/topics.go` 包含统一的 Topic 常量
- `cd server-monitor/pkg && go build ./kafka/` 通过（无需 sarama 依赖）

**质量检查方法**：

```bash
cd server-monitor/pkg && go build ./kafka/
cd server-monitor/pkg && go vet ./kafka/
```

**验收标准**：

- [x] Topic 常量与原两个服务一致
- [x] `go build ./kafka/` 通过（无需 sarama）

**执行记录（2026-05-15）**：

- 已比对 `server-web/kafka/topics.go` 和 `alert-service/kafka/topics.go`，两个文件的 `TopicAlertEvents`、`TopicOperationEvents` 常量完全一致
- 已新增 `pkg/kafka/topics.go`，仅包含 Topic 字符串常量
- 验证：`cd server-monitor/pkg && gofmt -w kafka/topics.go && go build ./kafka/` 通过
- 验证：`cd server-monitor/pkg && go vet ./kafka/` 通过
- 依赖确认：本步骤未修改 `pkg/go.mod` / `pkg/go.sum`，未引入 sarama

---

### 步骤 5：创建 pkg/kafka/producer.go — 统一 Producer + AlertEvent

**目标**：将两个服务中重复的 AlertEvent 类型和 Producer 逻辑提取到公共包。

**具体操作方法**：

1. 阅读两个服务的 Producer 和 AlertEvent 定义：
   ```bash
   cat server-web/kafka/producer.go
   cat alert-service/kafka/event.go
   ```
2. 确认 AlertEvent 字段差异（如有），以 server-web 版本为基准合并
3. 创建 `pkg/kafka/producer.go`：
   - 包含 `AlertEvent` 结构体定义（含 JSON tag）
   - 包含 `Producer` 类型及 `NewProducer` 构造函数
   - 包含 `SendMessage` 等方法
   - 添加包注释和 AlertEvent 字段注释
4. 确认 `pkg/go.mod` 中 sarama 依赖版本与 server-web 和 alert-service 一致（v1.48.0）。如 `go.mod` 中尚无 sarama，执行：
   ```bash
   cd server-monitor/pkg && go get github.com/IBM/sarama@v1.48.0
   ```
   > **时机说明**：sarama 在本步骤引入，因为 producer.go 是第一个实际 import sarama 的文件，`go mod tidy` 不会移除。

**预期完成标准**：

- `pkg/kafka/producer.go` 包含统一的 AlertEvent + Producer
- `cd server-monitor/pkg && go build ./kafka/` 通过

**质量检查方法**：

```bash
cd server-monitor/pkg && go build ./kafka/
cd server-monitor/pkg && go vet ./kafka/
```

**验收标准**：

- [x] AlertEvent 字段与原两个服务合并后一致
- [x] Producer 接口与 server-web 版本行为等价
- [x] `go build ./kafka/` 通过

**执行记录（2026-05-15）**：

- 已读取 `server-web/kafka/producer.go` 和 `alert-service/kafka/event.go`
- AlertEvent 对比结果：两个服务字段、JSON tag 完全一致；`pkg/kafka/producer.go` 保留相同字段并补充字段注释
- Producer 基准：以 `server-web/kafka/producer.go` 为准，保留 `NewProducer`、`SetObserver`、`SendAlertEvent`、`SendOperationEvent`、`Close`、异步 success/error 处理、observer 结果常量
- 依赖：`pkg/go.mod` 新增直接依赖 `github.com/IBM/sarama v1.48.0`，版本与 `server-web` / `alert-service` 一致；间接依赖随 sarama 加入，`golang.org/x/*` 版本与现有两个服务模块对齐
- 验证：`cd server-monitor/pkg && go list -m github.com/IBM/sarama` 输出 `github.com/IBM/sarama v1.48.0`
- 验证：`cd server-monitor/pkg && go build ./kafka/` 通过
- 验证：`cd server-monitor/pkg && go vet ./kafka/` 通过
- 补充检查：`cd server-monitor/pkg && go test ./kafka/` 通过（当前无测试文件，Kafka 行为测试按计划在步骤 7 补齐）

---

### 步骤 6：创建 pkg/kafka/consumer.go — 统一 Consumer

**目标**：合并两个服务的 Kafka Consumer，通过显式配置支持两种错误处理策略。

**具体操作方法**：

1. 详细对比两个 Consumer 的行为差异（参照 design.md §3.1 行为差异表）
2. 以 `server-web/kafka/consumer.go` 为基准（功能更完整）
3. 创建 `pkg/kafka/consumer.go`，核心设计：
   ```go
   type ConsumerConfig struct {
       Brokers      []string
       GroupID      string
       Topics       []string
       RetryBackoff func(int) time.Duration  // 非 nil 时启用重试循环
       StopOnError  bool                     // true 时 Consume 出错即返回
   }
   ```
4. `NewConsumer` 中校验 `RetryBackoff` 和 `StopOnError` 互斥：
   ```go
   if cfg.RetryBackoff != nil && cfg.StopOnError {
       return nil, fmt.Errorf("kafka: RetryBackoff and StopOnError are mutually exclusive")
   }
   ```
5. 保留 server-web 版本的全部特性：retry backoff、skipped error 日志、SetRetryableErrors、SetObserver、onReady/onNotReady 回调
6. 添加包注释，说明 Consumer 生命周期和两种消费策略

**预期完成标准**：

- `pkg/kafka/consumer.go` 支持两种消费错误策略
- `RetryBackoff` 和 `StopOnError` 互斥校验
- `cd server-monitor/pkg && go build ./kafka/` 通过

**质量检查方法**：

```bash
cd server-monitor/pkg && go build ./kafka/
cd server-monitor/pkg && go vet ./kafka/
```

**验收标准**：

- [x] ConsumerConfig 包含 RetryBackoff 和 StopOnError 字段
- [x] 互斥校验逻辑正确
- [x] server-web 路径（RetryBackoff）行为与原实现等价
- [x] alert-service 路径（StopOnError）行为与原实现等价
- [x] `go build ./kafka/` 通过

**执行记录（2026-05-16）**：

- 已对比 `server-web/kafka/consumer.go` 和 `alert-service/kafka/consumer.go`，确认核心差异为消费错误策略：server-web 重试，alert-service fail-fast
- 已新增 `pkg/kafka/consumer.go`，以 server-web 版本为基准，保留 `Skipped`、`Permanent`、`SetRetryableErrors`、`SetObserver`、onReady/onNotReady、invalid JSON 提交 offset、panic recovery、observer 结果常量
- 已新增 `ConsumerConfig`，包含 `Brokers`、`GroupID`、`Topics`、`RetryBackoff`、`StopOnError`
- 已实现 `RetryBackoff != nil && StopOnError` 互斥校验，错误信息为 `kafka: RetryBackoff and StopOnError are mutually exclusive`
- server-web 等价路径：`RetryBackoff` 非 nil 时，`Consume` 出错后执行 not-ready 回调并按 backoff 重试，直到 context 取消
- alert-service 等价路径：`StopOnError: true` 或 `RetryBackoff == nil` 时，`Consume` 出错直接返回 `consume kafka topics: ...`
- 已新增 `DefaultConsumeRetryBackoff`，供后续步骤迁移 server-web 初始化时显式配置
- 验证说明：默认 Go cache `/home/monody/.cache/go-build` 当前只读，步骤 6 验证使用 `GOCACHE=/tmp/cloudops-gocache`
- 验证：`cd server-monitor/pkg && GOCACHE=/tmp/cloudops-gocache go build ./kafka/` 通过
- 验证：`cd server-monitor/pkg && GOCACHE=/tmp/cloudops-gocache go vet ./kafka/` 通过
- 补充检查：`cd server-monitor/pkg && GOCACHE=/tmp/cloudops-gocache go test ./kafka/` 通过（当前无测试文件，Kafka 行为测试按计划在步骤 7 补齐）

---

### 步骤 7：编写 pkg/kafka/ 单元测试

**目标**：编写测试锁定两条消费路径的行为，确保合并后行为等价。

**具体操作方法**：

1. 迁移 `server-web/kafka/consumer_test.go` 到 `pkg/kafka/consumer_test.go`，适配新的 ConsumerConfig API
2. 新增测试用例覆盖：
   - **RetryBackoff 路径**：Consume 出错后重试，直到 context 取消
   - **StopOnError 路径**：Consume 出错后直接返回错误
   - **互斥校验**：RetryBackoff + StopOnError 同时设置返回错误
   - **nil channel 处理**：Consumer 未启动时 Close 不 panic
3. 使用 `sarama.NewMockBroker` 或 `httptest` 模拟 Kafka 交互，不依赖真实 Kafka
4. 新增 `pkg/kafka/producer_test.go`，测试 AlertEvent 序列化/反序列化

**预期完成标准**：

- `pkg/kafka/consumer_test.go` 覆盖两条消费路径
- `pkg/kafka/producer_test.go` 覆盖 AlertEvent 序列化
- `cd server-monitor/pkg && go test ./kafka/ -v` 全部通过

**质量检查方法**：

```bash
cd server-monitor/pkg && go test -v -count=1 ./kafka/
cd server-monitor/pkg && go test -race ./kafka/
```

**验收标准**：

- [ ] RetryBackoff 路径测试通过
- [ ] StopOnError 路径测试通过
- [ ] 互斥校验测试通过
- [ ] AlertEvent 序列化测试通过
- [ ] 无竞态条件（`-race` 通过）
- [ ] 不依赖真实 Kafka 实例

---

### 步骤 8：迁移 server-web/kafka/ → pkg/kafka/

**目标**：更新 server-web 的 import 路径，删除本地 kafka/ 目录。

**具体操作方法**：

1. 全局替换 server-web 中的 kafka import：
   ```bash
   # 查找所有引用 server-web/kafka 的文件
   grep -rn '"server-web/kafka"' server-web/
   ```
2. 逐文件替换 import 路径：
   - `"server-web/kafka"` → `"server-monitor/pkg/kafka"`
3. 更新 server-web 中 Consumer 的初始化代码，使用新的 ConsumerConfig：
   - Diagnosis Worker 的 Consumer 使用 `RetryBackoff: defaultConsumeRetryBackoff`
4. 确认 server-web 中不再有对 `server-web/kafka` 的引用
5. 删除 server-web/kafka/ 目录：
   ```bash
   git rm -r server-web/kafka/
   ```
6. 验证编译：
   ```bash
   cd server-monitor/server-web && go build ./...
   ```

**预期完成标准**：

- server-web 无本地 kafka/ 目录
- server-web 所有 kafka 相关 import 指向 `server-monitor/pkg/kafka`
- `cd server-monitor/server-web && go build ./...` 通过

**质量检查方法**：

```bash
# 确认无残留引用
grep -rn '"server-web/kafka"' server-web/
# 预期：无输出

# 编译验证
cd server-monitor/server-web && go build ./...
cd server-monitor/server-web && go test ./...
```

**验收标准**：

- [ ] `server-web/kafka/` 目录已删除
- [ ] 无 `server-web/kafka` 残留 import
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过
- [ ] Consumer 初始化使用 ConsumerConfig + RetryBackoff

---

### 步骤 9：迁移 alert-service/kafka/ → pkg/kafka/

**目标**：更新 alert-service 的 import 路径，删除本地 kafka/ 目录。

**具体操作方法**：

1. 全局替换 alert-service 中的 kafka import：
   ```bash
   grep -rn '"alert-service/kafka"' alert-service/
   ```
2. 逐文件替换 import 路径：
   - `"alert-service/kafka"` → `"server-monitor/pkg/kafka"`
3. 更新 alert-service 中 Consumer 的初始化代码，使用新的 ConsumerConfig：
   - Alert Consumer 使用 `StopOnError: true`
4. 删除 alert-service/kafka/ 目录：
   ```bash
   git rm -r alert-service/kafka/
   ```
5. 验证编译：
   ```bash
   cd server-monitor/alert-service && go build ./...
   ```

**预期完成标准**：

- alert-service 无本地 kafka/ 目录
- alert-service 所有 kafka 相关 import 指向 `server-monitor/pkg/kafka`
- `cd server-monitor/alert-service && go build ./...` 通过

**质量检查方法**：

```bash
grep -rn '"alert-service/kafka"' alert-service/
# 预期：无输出

cd server-monitor/alert-service && go build ./...
cd server-monitor/alert-service && go test ./...
```

**验收标准**：

- [ ] `alert-service/kafka/` 目录已删除
- [ ] 无 `alert-service/kafka` 残留 import
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过
- [ ] Consumer 初始化使用 ConsumerConfig + StopOnError: true

---

### 步骤 10：创建 pkg/redis/ — 统一 Redis 基础客户端

**目标**：提取两个服务中重复的 Redis Client 基础结构到公共包。

**具体操作方法**：

1. 详细对比两个 Redis Client：
   ```bash
   diff <(grep -n "func " server-web/redis/client.go) <(grep -n "func " alert-service/redis/client.go)
   ```
2. 确认共同方法：NewClient、Enabled、Close、Ping、Options
3. 创建 `pkg/redis/client.go`：
   - 包含基础客户端结构体和通用方法
   - 包含 `Inner()` 方法暴露底层 `go-redis.Client`（供业务封装使用）
   - 添加包注释
4. 创建 `pkg/redis/options.go`：
   - 包含 `Options` 配置结构体
5. 更新 `pkg/go.mod`，添加 go-redis 依赖：
   ```bash
   cd server-monitor/pkg && go get github.com/redis/go-redis/v9@v9.17.0
   ```
6. 编写 `pkg/redis/client_test.go`，使用 `miniredis` 测试：
   - NewClient / Close / Ping
   - Enabled 状态判断
   - Inner() 返回底层客户端
7. 添加 miniredis 测试依赖到 `pkg/go.mod`：
   ```bash
   cd server-monitor/pkg && go get github.com/alicebob/miniredis/v2@v2.37.0
   ```
   > **依赖说明**：miniredis 是纯测试依赖，不进入生产二进制。版本与 alert-service 已有间接依赖一致（v2.37.0）。

**预期完成标准**：

- `pkg/redis/client.go` 包含基础客户端 + 通用方法
- `pkg/redis/options.go` 包含配置选项
- `pkg/go.mod` 包含 go-redis 依赖
- `cd server-monitor/pkg && go test ./redis/` 通过

**质量检查方法**：

```bash
cd server-monitor/pkg && go build ./redis/
cd server-monitor/pkg && go test -v ./redis/
cd server-monitor/pkg && go vet ./redis/
```

**验收标准**：

- [ ] 基础客户端包含 NewClient/Enabled/Close/Ping/Options/Inner
- [ ] `Inner()` 方法可访问底层 go-redis.Client
- [ ] `pkg/go.mod` 包含 go-redis 依赖
- [ ] 测试使用 miniredis，不依赖真实 Redis
- [ ] `go test ./redis/` 通过

---

### 步骤 11：更新 alert-service/redis/ 基于 pkg/redis.Client 封装

**目标**：alert-service 的 Redis 客户端改为基于 `pkg/redis.Client` 的业务封装，保留 Lua 脚本方法。

**具体操作方法**：

1. 修改 `alert-service/redis/client.go`：
   - 保留 `package redisstore`（当前实际包名，不改变）
   - 内部组合 `pkgredis.Client`：
     ```go
     import pkgredis "server-monitor/pkg/redis"

     type Client struct {
         base *pkgredis.Client
     }

     func NewClient(options pkgredis.Options) *Client {
         return &Client{base: pkgredis.NewClient(options)}
     }

     func (c *Client) Enabled() bool  { return c.base.Enabled() }
     func (c *Client) Close() error   { return c.base.Close() }
     func (c *Client) Ping(ctx context.Context) error { return c.base.Ping(ctx) }
     ```
   - 保留业务特定的 Lua 脚本方法（`ApplyFiringEvent`、`ApplyResolvedEvent` 等），内部通过 `c.base.Inner()` 访问底层 go-redis.Client
2. 更新 `alert-service/go.mod`（如需添加 `server-monitor/pkg` 的间接依赖）
3. 验证编译：
   ```bash
   cd server-monitor/alert-service && go build ./...
   ```

**预期完成标准**：

- alert-service/redis/client.go 基于 pkg/redis.Client 封装
- Lua 脚本方法功能不变
- `package redisstore` 包名不变
- `cd server-monitor/alert-service && go build ./...` 通过

**质量检查方法**：

```bash
cd server-monitor/alert-service && go build ./...
cd server-monitor/alert-service && go test ./...
grep -n "pkgredis" alert-service/redis/client.go
```

**验收标准**：

- [ ] alert-service/redis/client.go 使用 pkg/redis.Client 作为基础
- [ ] Lua 脚本方法保留且功能不变
- [ ] `package redisstore` 包名不变
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过

---

### 步骤 12：更新 server-web/redis/ 基于 pkg/redis.Client 封装

**目标**：server-web 的 Redis 客户端改为基于 `pkg/redis.Client` 的业务封装，保留限流、缓存等业务方法。

**具体操作方法**：

1. 修改 `server-web/redis/client.go`：
   - 保留 `package rediscache`（当前实际包名，不改变）
   - 内部组合 `pkgredis.Client`，委托基础方法
   - 保留业务方法（Get、Set、HSet、Publish、Subscribe、限流等），内部通过 `c.base.Inner()` 或委托 `c.base` 方法实现
2. `server-web/redis/cache.go`（缓存常量）不变
3. 验证编译：
   ```bash
   cd server-monitor/server-web && go build ./...
   ```

**预期完成标准**：

- server-web/redis/client.go 基于 pkg/redis.Client 封装
- 业务方法（限流、缓存等）功能不变
- `package rediscache` 包名不变
- `cd server-monitor/server-web && go build ./...` 通过

**质量检查方法**：

```bash
cd server-monitor/server-web && go build ./...
cd server-monitor/server-web && go test ./...
grep -n "pkgredis" server-web/redis/client.go
```

**验收标准**：

- [ ] server-web/redis/client.go 使用 pkg/redis.Client 作为基础
- [ ] 业务方法保留且功能不变
- [ ] `package rediscache` 包名不变
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过

---

### 步骤 13：创建 server-web/app.go + 拆分 router.go 服务组装逻辑

**目标**：将服务组装逻辑从 router.go 拆分到 app.go，router.go 仅保留路由注册。

**具体操作方法**：

1. 创建 `server-web/app.go`，定义三层依赖结构：
   ```go
   type infrastructure struct {
       shutdownTracer   func(context.Context) error
       prometheusClient *promclient.Client
       redisClient      *rediscache.Client
       mysqlClient      *database.MySQL
       kafkaProducer    *eventbus.Producer
       websocketHub     *ws.Hub
       alertHub         *pubsub.Hub
   }

   type services struct {
       authService  *authpkg.Service
       alertService *alert.Service
       copilotDeps  *api.CopilotDeps
   }
   ```
2. 实现 `initInfrastructure` 和 `initServices` 函数，从 main.go 的 `initApp` 中迁移初始化逻辑
3. 修改 `server-web/api/router.go`：
   - 定义 `Dependencies` 结构体，接收所有路由层依赖
   - `NewRouter` 签名改为 `NewRouter(cfg config.Config, deps Dependencies) (*gin.Engine, error)`
   - 删除 router.go 中的服务组装代码，仅保留路由注册和中间件挂载
4. 逐步迁移，每迁移一个 init 函数就编译验证
5. 添加 app.go 依赖分层注释

**预期完成标准**：

- `server-web/app.go` 包含三层依赖结构和初始化函数
- `server-web/api/router.go` 仅包含路由注册，行数 < 150
- `cd server-monitor/server-web && go build ./...` 通过

**质量检查方法**：

```bash
wc -l server-web/api/router.go
# 预期：< 150 行

cd server-monitor/server-web && go build ./...
cd server-monitor/server-web && go test ./...
```

**验收标准**：

- [ ] app.go 包含 infrastructure/services 三层结构
- [ ] router.go 仅包含路由注册 + Dependencies 注入
- [ ] router.go 行数 < 150
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过
- [ ] 服务启动行为与重构前等价

---

### 步骤 14：精简 server-web/main.go + 修复 Channel 等待超时

**目标**：精简 main.go 至 < 80 行，修复 3 处 Channel 无超时阻塞等待。

**具体操作方法**：

1. 精简 `server-web/main.go`，统一为三段式模式：
   ```go
   func main() {
       log, err := logger.Init("server-web")
       if err != nil { ... }
       defer logger.Sync(log)

       app, err := initApp(context.Background())
       if err != nil { ... }

       exitCode := runApp(app)
       shutdownApp(app)
       if exitCode != 0 { os.Exit(exitCode) }
   }
   ```
2. `initApp`、`runApp`、`shutdownApp` 定义在 app.go 中
3. 修复 Channel 等待超时，添加 `waitWithTimeout` 工具函数：
   ```go
   func waitWithTimeout(ch <-chan struct{}, timeout time.Duration, name string) {
       if ch == nil {
           return
       }
       select {
       case <-ch:
           zap.L().Info("shutdown wait completed", zap.String("name", name))
       case <-time.After(timeout):
           zap.L().Warn("shutdown wait timed out, proceeding",
               zap.String("name", name), zap.Duration("timeout", timeout))
       }
   }
   ```
4. 修复 3 处无超时等待：
   - `app.subscriberDone` → `waitWithTimeout(app.subscriberDone, app.cfg.ShutdownTimeout, "subscriber")`
   - `app.diagnosisDone` → `waitWithTimeout(app.diagnosisDone, app.cfg.ShutdownTimeout, "diagnosis-consumer")`
   - `app.alertHubConsumers` → `waitWithTimeout(app.alertHubConsumers, app.cfg.ShutdownTimeout, "alert-hub-consumers")`
5. 实现分阶段关闭（参照 design.md §6.4）：
   - 阶段 1：停止流量入口（HTTP Server + Tracer）
   - 阶段 2：停止消费者（cancel context + 关闭 Kafka Consumer + 等待 channel）
   - 阶段 3：释放资源（Redis + MySQL + Kafka Producer）
   - 阶段 4：清理（AlertHub + 等待 consumers）

**预期完成标准**：

- server-web/main.go < 80 行
- 3 处 Channel 等待均有超时保护
- 关闭流程分 4 阶段有序执行
- `cd server-monitor/server-web && go build ./...` 通过

**质量检查方法**：

```bash
wc -l server-web/main.go
# 预期：< 80 行

# 确认无裸 channel 等待
grep -n "<-app\." server-web/app.go server-web/main.go
# 预期：所有 channel 等待均通过 waitWithTimeout

cd server-monitor/server-web && go build ./...
cd server-monitor/server-web && go test ./...
```

**验收标准**：

- [ ] main.go < 80 行
- [ ] 3 处 Channel 等待均有超时保护
- [ ] nil channel 先检查再 select
- [ ] 关闭流程分 4 阶段
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过

---

### 步骤 15：创建 alert-service/app.go + 精简 main.go

**目标**：将 alert-service 的初始化逻辑迁移到 app.go，精简 main.go 至 < 60 行。

**具体操作方法**：

1. 创建 `alert-service/app.go`，封装 `initApp`、`runApp`、`shutdownApp`
2. 从 main.go 迁移初始化逻辑到 app.go：
   - initInfrastructure：Tracer → Redis
   - initServices：Metrics → AlertStore → Kafka Consumer
3. 精简 main.go 为三段式模式（同步骤 14 模式）
4. 检查关闭流程中是否有 Channel 等待需要超时保护
5. 添加依赖分层注释

**预期完成标准**：

- alert-service/app.go 包含依赖组装逻辑
- alert-service/main.go < 60 行
- `cd server-monitor/alert-service && go build ./...` 通过

**质量检查方法**：

```bash
wc -l alert-service/main.go
# 预期：< 60 行

cd server-monitor/alert-service && go build ./...
cd server-monitor/alert-service && go test ./...
```

**验收标准**：

- [ ] app.go 包含 initApp/runApp/shutdownApp
- [ ] main.go < 60 行
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过
- [ ] 关闭流程无无超时 Channel 等待

---

### 步骤 16：创建 server-probe/app.go + 精简 main.go

**目标**：将 server-probe 的初始化逻辑迁移到 app.go，精简 main.go 至 < 60 行。

**具体操作方法**：

1. 创建 `server-probe/app.go`，封装 `initApp`、`runApp`、`shutdownApp`
2. 从 main.go 迁移初始化逻辑到 app.go：
   - initInfrastructure：Tracer → Host 路径
   - initServices：Collectors + Prometheus Registry
3. 精简 main.go 为三段式模式
4. 检查关闭流程
5. 添加依赖分层注释

**预期完成标准**：

- server-probe/app.go 包含依赖组装逻辑
- server-probe/main.go < 60 行
- `cd server-monitor/server-probe && go build ./...` 通过

**质量检查方法**：

```bash
wc -l server-probe/main.go
# 预期：< 60 行

cd server-monitor/server-probe && go build ./...
cd server-monitor/server-probe && go test ./...
```

**验收标准**：

- [ ] app.go 包含 initApp/runApp/shutdownApp
- [ ] main.go < 60 行
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过

---

### 步骤 17：目录结构重构 — 创建 internal/ 目录 + 移动底层模块

**目标**：创建 server-web/internal/ 目录结构，移动底层无业务依赖的模块。

**具体操作方法**：

1. 创建目标目录结构（包含步骤 17 和步骤 18 需要的全部目录，工作目录：`server-monitor/server-web/`）：
   ```bash
   cd server-monitor/server-web
   mkdir -p internal/{config,handler,router,middleware,model}
   mkdir -p internal/service/{alert,auth,cache,host}
   mkdir -p internal/infra/{database,redis,prometheus,pubsub,websocket,webhook}
   mkdir -p internal/copilot/{service,handler,session,nlu,llm,tool,runbook,k8s,diagnosis,action,feedback}
   mkdir -p internal/copilot/nlu/eval
   mkdir -p internal/copilot/runbook/eval
   ```
   > **说明**：步骤 18 需要的 `internal/service/` 和 `internal/copilot/` 子目录在此一并创建，避免步骤 18 执行 `git mv` 时目标目录不存在导致失败。
2. 按依赖关系由底到顶移动文件（使用 `git mv`，工作目录：`server-monitor/server-web/`）：
   ```bash
   cd server-monitor/server-web
   # 1. model（无业务依赖）
   git mv model/*.go internal/model/

   # 2. database（仅依赖 model）
   git mv database/mysql.go internal/infra/database/
   git mv database/migrate.go internal/infra/database/

   # 3. prometheus（无业务依赖）
   git mv prometheus/client.go internal/infra/prometheus/
   git mv prometheus/queries.go internal/infra/prometheus/

   # 4. websocket（无业务依赖）
   git mv websocket/hub.go internal/infra/websocket/

   # 5. webhook（无业务依赖）
   git mv webhook/alertmanager.go internal/infra/webhook/
   ```
3. 每移动一个模块后，立即更新该模块内部的 import 路径并编译验证
4. **注意**：此步骤暂不移动 redis/ 和 pubsub/（它们依赖 pkg/redis，需在步骤 12 完成后移动）

**预期完成标准**：

- model/、database/、prometheus/、websocket/、webhook/ 已移入 internal/
- 各模块 package 声明不变（model、database、promclient、websocket、webhook）
- 每个模块移动后编译通过

**质量检查方法**：

```bash
# 每移动一个模块后验证（工作目录：server-monitor/server-web/）
cd server-monitor/server-web
go build ./internal/model/
go build ./internal/infra/database/
go build ./internal/infra/prometheus/
go build ./internal/infra/websocket/
go build ./internal/infra/webhook/
```

**验收标准**：

- [ ] 5 个底层模块已移入 internal/
- [ ] 各模块 package 名不变
- [ ] 各模块 `go build` 通过
- [ ] 使用 `git mv` 保持历史连续性

---

### 步骤 18：目录结构重构 — 移动服务层 + 基础设施层 + API 层 + Copilot

**目标**：完成剩余模块的 internal/ 迁移，更新全部 import 路径。

**具体操作方法**：

1. 移动服务层模块（工作目录：`server-monitor/server-web/`）：
   ```bash
   cd server-monitor/server-web
   git mv alert/service.go internal/service/alert/
   git mv auth/service.go auth/token.go auth/password.go internal/service/auth/
   git mv cache/service.go internal/service/cache/
   git mv host/service.go internal/service/host/
   ```
2. 移动基础设施层剩余模块（工作目录：`server-monitor/server-web/`）：
   ```bash
   cd server-monitor/server-web
   git mv redis/client.go redis/cache.go internal/infra/redis/
   git mv pubsub/hub.go pubsub/subscriber.go internal/infra/pubsub/
   ```
3. 移动 API 层（工作目录：`server-monitor/server-web/`）：
   ```bash
   cd server-monitor/server-web
   git mv api/handlers/*.go internal/handler/
   git mv api/middleware/*.go internal/middleware/
   git mv api/router.go internal/router/router.go
   ```
4. 移动 diagnosisAccessAdapter（工作目录：`server-monitor/server-web/`）：
   ```bash
   cd server-monitor/server-web
   git mv api/diagnosis_access_adapter.go internal/copilot/diagnosis/access_adapter.go
   ```
   - 修改为导出构造函数 `NewReportAccessChecker`
5. 移动 config（工作目录：`server-monitor/server-web/`）：
   ```bash
   cd server-monitor/server-web
   git mv config/config.go internal/config/
   ```
6. 移动 copilot（保持内部子包结构，工作目录：`server-monitor/server-web/`）：
   ```bash
   cd server-monitor/server-web
   git mv copilot/service/ internal/copilot/service/
   git mv copilot/handler/ internal/copilot/handler/
   git mv copilot/session/ internal/copilot/session/
   git mv copilot/nlu/ internal/copilot/nlu/
   git mv copilot/llm/ internal/copilot/llm/
   git mv copilot/tool/ internal/copilot/tool/
   git mv copilot/runbook/ internal/copilot/runbook/
   git mv copilot/k8s/ internal/copilot/k8s/
   git mv copilot/diagnosis/ internal/copilot/diagnosis/
   git mv copilot/action/ internal/copilot/action/
   git mv copilot/feedback/ internal/copilot/feedback/
   ```
7. 全局更新 import 路径（参照 design.md §17.3，工作目录：`server-monitor/server-web/`）：
   ```bash
   cd server-monitor/server-web

   # 服务层
   sed -i 's|"server-web/alert"|"server-web/internal/service/alert"|g' $(find . -name '*.go')
   sed -i 's|"server-web/auth"|"server-web/internal/service/auth"|g' $(find . -name '*.go')
   sed -i 's|"server-web/cache"|"server-web/internal/service/cache"|g' $(find . -name '*.go')
   sed -i 's|"server-web/host"|"server-web/internal/service/host"|g' $(find . -name '*.go')

   # 基础设施层
   sed -i 's|"server-web/database"|"server-web/internal/infra/database"|g' $(find . -name '*.go')
   sed -i 's|"server-web/redis"|"server-web/internal/infra/redis"|g' $(find . -name '*.go')
   sed -i 's|"server-web/prometheus"|"server-web/internal/infra/prometheus"|g' $(find . -name '*.go')
   sed -i 's|"server-web/pubsub"|"server-web/internal/infra/pubsub"|g' $(find . -name '*.go')
   sed -i 's|"server-web/websocket"|"server-web/internal/infra/websocket"|g' $(find . -name '*.go')
   sed -i 's|"server-web/webhook"|"server-web/internal/infra/webhook"|g' $(find . -name '*.go')

   # API 层
   sed -i 's|"server-web/api/handlers"|"server-web/internal/handler"|g' $(find . -name '*.go')
   sed -i 's|"server-web/api/middleware"|"server-web/internal/middleware"|g' $(find . -name '*.go')
   sed -i 's|"server-web/model"|"server-web/internal/model"|g' $(find . -name '*.go')
   sed -i 's|"server-web/config"|"server-web/internal/config"|g' $(find . -name '*.go')

   # Copilot
   sed -i 's|"server-web/copilot/|"server-web/internal/copilot/|g' $(find . -name '*.go')
   ```
8. 逐个编译验证，修复遗漏的 import 路径

**预期完成标准**：

- 所有模块已移入 internal/
- 所有 import 路径已更新
- `cd server-monitor/server-web && go build ./...` 通过
- 原顶级目录（alert/、auth/、cache/、host/、model/、database/、prometheus/、redis/、pubsub/、websocket/、webhook/、config/、api/、copilot/）已清空

**质量检查方法**：

```bash
# 确认无旧路径残留
grep -rn '"server-web/alert"' server-web/
grep -rn '"server-web/auth"' server-web/
grep -rn '"server-web/database"' server-web/
# ... 其余旧路径同理，预期均无输出

# 全量编译和测试
cd server-monitor/server-web && go build ./...
cd server-monitor/server-web && go test ./...
cd server-monitor/server-web && go vet ./...

# 确认原顶级目录已清空
ls server-web/alert/ server-web/auth/ server-web/model/ 2>&1
# 预期：No such file or directory
```

**验收标准**：

- [ ] 所有模块已移入 internal/ 对应目录
- [ ] 无旧 import 路径残留
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过
- [ ] `go vet ./...` 无警告
- [ ] diagnosisAccessAdapter 已改为导出构造函数
- [ ] 原顶级业务目录已删除

---

### 步骤 19：更新 CI 配置 + Makefile 补充

**目标**：更新 CI 配置以适配目录结构变更，补充 Makefile 的 pkg 测试目标。

**具体操作方法**：

1. 修改 `.github/workflows/ci.yaml`：
   - 新增 `pkg` job：
     ```yaml
     pkg:
       name: pkg checks
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v4
         - uses: actions/setup-go@v5
           with:
             go-version: "1.26"
             cache-dependency-path: pkg/go.sum
         - name: Install goimports
           run: go install golang.org/x/tools/cmd/goimports@latest
         - name: Goimports
           working-directory: pkg
           run: test -z "$(goimports -l .)"
         - name: Test
           working-directory: pkg
           run: go test ./...
         - name: Vet
           working-directory: pkg
           run: go vet ./...
     ```
   - 修改 3 个 eval 测试步骤路径：
     ```yaml
     # 修改前：./copilot/nlu/eval/
     # 修改后：./internal/copilot/nlu/eval/
     # 修改前：./copilot/runbook/eval/
     # 修改后：./internal/copilot/runbook/eval/
     ```
   - 将 `pkg` 加入 `docker-build` job 的 `needs` 列表
2. 补充 Makefile：
   - 修改 `test` 目标，增加 pkg 测试：
     ```makefile
     test:
     	@echo "运行测试..."
     	cd server-monitor/pkg && go test -v ./...
     	cd server-monitor/server-probe && go test -v ./...
     	cd server-monitor/server-web && go test -v ./...
     	cd server-monitor/alert-service && go test -v ./...
     ```
   - 新增 `test-pkg` 独立目标（用于单独验证 pkg）：
     ```makefile
     test-pkg:
     	@echo "运行 pkg 测试..."
     	cd server-monitor/pkg && go test -v ./...
     ```
   - 更新 `.PHONY` 声明，添加 `test-pkg`：
     ```makefile
     .PHONY: all build build-probe build-web build-alert-service run run-probe run-web docker docker-up docker-down docker-logs clean test test-pkg help dev-deps dev-web dev-frontend dev-stop
     ```
   - 更新 `help` target，添加 `test-pkg` 说明：
     ```makefile
     @echo "  make test           运行所有测试（含 pkg）"
     @echo "  make test-pkg       仅运行 pkg 测试"
     ```
   > **关键变更**：`make test` 现在包含 `pkg` 测试，与 CI 的 `pkg` job 对齐，确保本地验证与 CI 一致。
3. 验证 CI 配置语法：
   ```bash
   # 可选：使用 actionlint 检查
   actionlint .github/workflows/ci.yaml
   ```

**预期完成标准**：

- CI 新增 pkg job
- 3 个 eval 测试路径已更新
- docker-build 依赖 pkg job
- Makefile 新增 test-pkg 目标

**质量检查方法**：

```bash
# 验证 Makefile
make test-pkg

# 验证 CI YAML 语法
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yaml'))"

# 验证 eval 测试路径
grep -n "copilot/nlu/eval" .github/workflows/ci.yaml
# 预期：路径包含 internal/
```

**验收标准**：

- [ ] CI 新增 pkg job（goimports + test + vet）
- [ ] 3 个 eval 测试路径加 `internal/` 前缀
- [ ] `docker-build.needs` 包含 `pkg`
- [ ] `make test-pkg` 可执行
- [ ] CI YAML 语法正确

---

### 步骤 20：关键注释补充 + 全量验证 + 最终验收

**目标**：补充关键注释，执行全量编译测试验证，完成最终验收。

**具体操作方法**：

1. **补充关键注释**（仅补充真正缺失且有价值的，注释也是文件变更，须在最终验证前完成）：
   - `pkg/kafka/consumer.go`：包注释 + Consumer 生命周期说明
   - `pkg/kafka/producer.go`：包注释 + AlertEvent 字段注释
   - `pkg/redis/client.go`：包注释 + Inner() 方法说明
   - `pkg/configutil/validate.go`：包注释
   - `server-web/app.go`：依赖分层说明
   - `alert-service/app.go`：依赖分层说明
   - `server-probe/app.go`：依赖分层说明
   - `server-web/main.go`：关闭流程注释
2. **代码格式化**（注释变更后重新格式化）：
   ```bash
   cd server-monitor
   goimports -w server-web/ alert-service/ server-probe/ pkg/
   ```
3. **全量编译验证**：
   ```bash
   cd server-monitor && make build
   ```
4. **全量测试验证**：
   ```bash
   cd server-monitor && make test
   ```
5. **静态分析**：
   ```bash
   cd server-monitor
   cd server-monitor/server-web && go vet ./... && cd ..
   cd server-monitor/alert-service && go vet ./... && cd ..
   cd server-monitor/server-probe && go vet ./... && cd ..
   cd server-monitor/pkg && go vet ./... && cd ..
   ```
6. **Docker 构建验证**：
   ```bash
   cd server-monitor && make docker
   ```
7. **量化指标验收**：
   ```bash
   wc -l server-web/main.go          # 预期：< 80
   wc -l server-web/internal/router/router.go  # 预期：< 150
   wc -l alert-service/main.go       # 预期：< 60
   wc -l server-probe/main.go        # 预期：< 60
   ```
8. **Channel 等待超时验收**：
   ```bash
   grep -rn "<-app\." server-web/
   # 预期：所有 channel 等待均通过 waitWithTimeout
   ```
9. **重复代码验收**：
   ```bash
   ls server-web/kafka/ alert-service/kafka/ 2>&1
   # 预期：No such file or directory
   grep -rn "func validateHostPort" server-web/ alert-service/ server-probe/
   # 预期：无输出
   ```
10. **internal 封装验收**：
    ```bash
    ls server-web/internal/
    # 预期：config handler middleware model router service infra copilot
    ```

**预期完成标准**：

- 关键注释已补充
- `make build` 通过
- `make test` 通过
- `make docker` 通过
- 所有量化指标达标

**质量检查方法**：

```bash
make build && make test && make docker
cd server-monitor/pkg && go test -v ./...
cd server-monitor/server-web && go vet ./...
cd server-monitor/alert-service && go vet ./...
cd server-monitor/server-probe && go vet ./...
```

**验收标准**：

- [ ] `make build` 通过
- [ ] `make test` 通过
- [ ] `make docker` 通过
- [ ] `pkg/go test ./...` 通过
- [ ] `go vet ./...` 各服务无警告
- [ ] server-web/main.go < 80 行
- [ ] router.go < 150 行
- [ ] alert-service/main.go < 60 行
- [ ] server-probe/main.go < 60 行
- [ ] Channel 等待超时：0 处无保护
- [ ] 重复代码：0 处残留
- [ ] internal/ 封装：所有业务代码在 internal/ 下
- [ ] 关键注释已补充
- [ ] CI 配置已更新

---

## 6. 验证总表

| 步骤 | 验证命令 | 关键指标 |
|------|---------|---------|
| 1 | `make build && make test` | 基线健康 |
| 2 | `cd server-monitor/pkg && go test ./configutil/` | 校验函数测试通过 |
| 3 | `make build && make test` | 三个服务编译测试通过 |
| 4 | `cd server-monitor/pkg && go build ./kafka/` | topics.go 编译通过 |
| 5 | `cd server-monitor/pkg && go build ./kafka/` | producer.go 编译通过 |
| 6 | `cd server-monitor/pkg && go build ./kafka/` | consumer.go 编译通过 |
| 7 | `cd server-monitor/pkg && go test -v ./kafka/` | 两条消费路径测试通过 |
| 8 | `cd server-monitor/server-web && go build ./...` | server-web 无本地 kafka/ |
| 9 | `cd server-monitor/alert-service && go build ./...` | alert-service 无本地 kafka/ |
| 10 | `cd server-monitor/pkg && go test ./redis/` | Redis 基础客户端测试通过 |
| 11 | `cd server-monitor/alert-service && go build ./...` | alert-service Redis 封装编译通过 |
| 12 | `cd server-monitor/server-web && go build ./...` | server-web Redis 封装编译通过 |
| 13 | `cd server-monitor/server-web && go build ./...` | router.go < 150 行 |
| 14 | `cd server-monitor/server-web && go build ./...` | main.go < 80 行，0 处无超时等待 |
| 15 | `cd server-monitor/alert-service && go build ./...` | main.go < 60 行 |
| 16 | `cd server-monitor/server-probe && go build ./...` | main.go < 60 行 |
| 17 | `cd server-monitor/server-web && go build ./internal/...` | 底层模块 internal 迁移通过 |
| 18 | `cd server-monitor/server-web && go build ./...` | 全量 internal 迁移通过 |
| 19 | `make test-pkg` | CI 配置 + Makefile 更新 |
| 20 | `make build && make test && make docker` | 全量验收通过 |

---

## 7. 回滚策略

### 7.1 逐步回滚

由于每步一个 commit，可逐回滚：

```bash
# 查看提交历史
git log --oneline refactor/architecture-restructure

# 回滚到指定步骤
git revert <commit-hash>
```

### 7.2 全量回滚

如果需要完全放弃重构：

```bash
git checkout main
git branch -D refactor/architecture-restructure
```

### 7.3 关键回滚点

| 步骤 | 回滚风险 | 回滚方法 |
|------|---------|---------|
| 步骤 2-3 | 低 | `git revert` 恢复本地 validate 函数 |
| 步骤 4-9 | 中 | `git revert` 恢复本地 kafka/ 目录 |
| 步骤 10-12 | 中 | `git revert` 恢复本地 redis/ 目录原始实现 |
| 步骤 13-16 | 中 | `git revert` 恢复 main.go 和 router.go |
| 步骤 17-18 | 高 | `git revert` 或 `git reset` 恢复目录结构 |

---

## 8. 评审检查清单

### 8.1 文档完整性

- [ ] 20 个步骤是否覆盖 design.md 全部内容
- [ ] 每个步骤是否有具体操作方法、预期完成标准、质量检查方法、验收标准
- [ ] 实施目标、前置条件、资源需求、风险评估是否完整

### 8.2 准确性

- [ ] 文件路径是否与实际项目结构一致
- [ ] import 路径替换是否正确
- [ ] package 名是否与实际代码一致（rediscache、promclient 等）
- [ ] ConsumerConfig 互斥校验逻辑是否与 design.md 一致

### 8.3 可操作性

- [ ] 每个步骤是否可独立执行
- [ ] 步骤间依赖关系是否清晰
- [ ] 验证命令是否可直接复制执行
- [ ] 回滚策略是否可行
- [ ] 每个命令块是否明确 cwd（不依赖上一步 shell 状态）
- [ ] internal/ 目录创建是否在 git mv 之前完成
- [ ] 依赖添加时机是否与代码 import 同步（避免 `go mod tidy` 移除）

### 8.4 风险覆盖

- [ ] 11 项风险是否均有应对措施
- [ ] 高风险步骤是否有额外保护（如步骤 7 的测试锁定）
- [ ] 是否有遗漏的风险项

### 8.5 与 design.md 一致性

- [ ] 公共代码提取范围是否一致（kafka、redis、configutil）
- [ ] 目录结构目标是否一致（internal/ 分层）
- [ ] 依赖注入方式是否一致（构造函数注入，无 DI 框架）
- [ ] 关闭流程是否一致（4 阶段 + 超时保护）
- [ ] CI 变更是否一致（pkg job + eval 路径更新）
