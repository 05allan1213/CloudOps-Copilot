# Phase 3 代码审查结果

审查范围：`1ed7d32..17a5361`（29 个文件，+3691/-23 行）
审查日期：2026-05-08

---

## 优点

1. **架构分层清晰** — handler/service/repository/resolver/evidence/rule/summarizer 各司其职，`Config` 结构体显式依赖注入
2. **LLM 降级策略完整** — 三种失败模式（nil/disabled、timeout/error、parse error）均正确降级到 rule-only 报告
3. **EvidenceCollector 通过 Tool Registry 抽象调用工具**，保持 Phase 2 的 tool 抽象层
4. **权限控制正确** — repository 层 `canAccessReport` 检查，非 admin 只能查看自己的报告
5. **安全意识好** — resolver 过滤 password/token/secret 等敏感 label，prompt 限制 16KB
6. **前端覆盖面广** — 告警面板、告警历史、Copilot 页面均接入诊断入口
7. **冲突处理设计合理** — `ConflictError` 支持 `errors.Is()`，handler 映射 409，Copilot 返回候选列表

---

## 问题

### Critical — 无

### Important（应修复）

#### 1. `contextFromHistory` 未填充 Annotations

- **位置**: `resolver.go:207-243`
- **问题**: `contextFromHistory` 从 `model.AlertHistory` 构建 `AlertContext` 时，解析了 `Labels` 但从未填充 `Annotations` 字段。
- **影响**: 历史告警来源的诊断缺少 summary、description、runbook_url 等注解数据。文档 Section 4.2 Step 3 明确要求 resolver 输出包含 `annotations`。
- **修复**: 检查 `model.AlertHistory` 是否有 annotations JSON 字段，有则解析填充；无则初始化空 map 并从 model 字段回填。

#### 2. 集成层零测试覆盖

- **位置**: 无测试文件
- **问题**: 文档 Section 8.1 要求以下测试：
  - Repository: SQLite/mock 测试创建、更新、分页、权限过滤
  - Handler: httptest 测试 401、400、404、409、200
  - Service: Mock 依赖测试 pending/running/completed/failed 状态流转
  - Resolver: Mock reader 测试唯一匹配、多条匹配、无匹配
- **现有测试**: 仅 `request_test.go`、`rule_test.go`、`evidence_test.go`、`summarizer_test.go` 覆盖纯逻辑模块
- **影响**: handler 的错误映射（`handler.go:120-142` `writeError`）、service 的状态流转（`service.go:55-132`）、repository 的权限过滤（`repository.go:44-63`）、resolver 的冲突检测（`resolver.go:109-155`）均无测试，这些是最容易出 bug 的部分。

#### 3. EvidenceCollector 缺少单工具超时

- **位置**: `evidence.go:70-71`
- **问题**: 只设置了整体 45s context timeout，各 goroutine 无独立的 30s 单工具超时。文档 Section 4.3 Step 4 要求"单工具 timeout 默认 30 秒"。
- **影响**: 若 `prom.query_range` 卡 40s，会吃掉几乎整个预算，其余工具可能只剩 5s。
- **修复**: 每个 goroutine 内加 `context.WithTimeout`:
  ```go
  go func() {
      defer wg.Done()
      toolCtx, toolCancel := context.WithTimeout(ctx, 30*time.Second)
      defer toolCancel()
      active, err := c.collectActiveAlerts(toolCtx, alert)
      // ...
  }()
  ```

#### 4. `UpdateStatus` 签名偏离文档

- **位置**: `repository.go:31-42`
- **问题**: 文档 Module 1 要求 `UpdateStatus(ctx, id, status, fields)`，实现为通用 `Update(ctx, id, updates)`。
- **影响**: 通用方法允许不更新 status 就修改其他字段，绕过状态机约束。编译期无法强制状态转换。
- **修复**: 改为显式 `UpdateStatus(ctx, id, status, fields)` 签名，或在 service 层确保每次调用都包含 status。

---

### Minor（建议改进）

#### 1. 置信度上限 0.88

- **位置**: `rule.go:129-151`
- **说明**: 最大置信度 0.3 + 0.3 + 0.2 + 0.08 = 0.88（runbook 权重 0.2 × 固定值 0.4 = 0.08）。High 区间很窄（0.8~0.88），Phase 4 接入 Runbook 后会改善。

#### 2. `passedRuleRefs` 命名有歧义

- **位置**: `summarizer.go:174-185`
- **说明**: 函数返回规则名和证据引用的混合列表（如 `"cpu_sustained_high"` + `"metrics.cpu_usage"`），但函数名暗示只返回规则引用。建议改名为 `passedRuleEvidence` 或分离返回。

#### 3. `Severity` 字段加了未规划的索引

- **位置**: `diagnosis_report.go:13`
- **说明**: `Severity` 有 `gorm:"index"` 但文档 Section 4.1 Step 6 的索引列表未包含 severity。无害但偏离规格。

#### 4. 诊断路由耦合 `CopilotEnabled`

- **位置**: `router.go:120-188`
- **说明**: 所有诊断路由注册在 `if cfg.CopilotEnabled` 块内。无法在不启用 Copilot Chat 的情况下独立使用诊断 API。建议增加 `DiagnosisEnabled` 配置项或文档化此耦合。

#### 5. `extractJSONBody` 对畸形 markdown 容错不足

- **位置**: `summarizer.go:120-129`
- **说明**: 按 `\n` 分割取 `lines[1:len(lines)-1]`，对嵌套代码块或畸形输出可能提取错误内容。当前测试只覆盖了正常路径。

---

## 建议

1. 补 handler httptest（覆盖 400/401/404/409/200 错误映射）
2. 补 service 测试（mock 各依赖，覆盖状态流转各失败点）
3. 给 EvidenceCollector 每个 goroutine 加 `context.WithTimeout(ctx, 30*time.Second)`
4. 检查 `model.AlertHistory` 是否有 annotations 字段，有则在 `contextFromHistory` 中填充
5. 考虑将诊断路由独立于 `CopilotEnabled` 配置

---

## 结论

**可以合并，但建议先处理 Important #1 和 #3。**

核心诊断流水线（resolve → collect → analyze → summarize → persist → display）完整且降级正确。主要缺口在集成层测试覆盖和 resolver 的 annotations 遗漏。per-tool timeout 是性能风险但不影响正确性。

---

## Codex 复核与处理（2026-05-08）

### 复核结论

1. **Important #1 属实，且根因更靠前。**
   - `contextFromHistory` 原本没有填充 `AlertContext.Annotations`。
   - 同时 `model.AlertHistory` 原本也没有保存完整 annotations，只保存了 `summary` 和 `labels_json`，因此历史告警无法还原 `description`、`runbook_url` 等注解。

2. **Important #2 部分属实。**
   - 诊断模块并非完全没有测试文件，当前工作区存在 `request_test.go`、`rule_test.go`、`evidence_test.go`、`summarizer_test.go`。
   - 但根目录 `.gitignore` 忽略 `*_test.go`，这些测试不会出现在普通 `git status` / `git ls-files` 中。
   - handler / service / repository 的集成覆盖仍然不足，本次先补与已修问题直接相关的 resolver / evidence 回归测试，未扩大到完整集成测试矩阵。

3. **Important #3 属实。**
   - `EvidenceCollector` 原本只有整体 45s timeout，四个并发采集分支共用同一个 context。
   - 已改为整体 45s + 单工具默认 30s timeout。

4. **Important #4 属实。**
   - repository 原本暴露通用 `Update(ctx, id, updates)`。
   - 已收窄为 `UpdateStatus(ctx, id, status, fields)`，由方法强制写入合法状态，service 调用同步调整。

### 实际修改

- `model.AlertHistory` 增加可空 `annotations_json` 字段，由 AutoMigrate 补列；旧历史记录为空时回退使用 `summary`。
- 告警归档时保存完整 annotations，更新历史记录时同步更新 `annotations_json`。
- resolver 从历史告警还原 annotations，并继续过滤敏感 key。
- EvidenceCollector 为每个采集分支设置单工具 timeout。
- Diagnosis repository 改为显式状态更新接口。
- 新增/更新回归测试：
  - `copilot/diagnosis/resolver_test.go`
  - `copilot/diagnosis/evidence_test.go`

### 已验证

- `go test ./copilot/diagnosis`：通过。

### 未处理

- handler / service / repository 的完整集成测试矩阵仍未补齐，应作为后续独立测试增强任务处理。
- Minor 项未改动，均不影响当前 Phase 3 核心闭环。
