# AI 质量退化排查 Runbook

## 1 概述
本文档覆盖 CloudOps Copilot AI 模块的 5 类常见质量退化场景，
每类包含现象识别、排查步骤和恢复操作。

## 2 通用排查前提
- 确认 Prometheus 指标采集正常：curl localhost:9090/api/v1/query?query=up
- 确认 server-web 服务健康：curl localhost:8080/health
- 确认 LLM API Key 有效：检查环境变量或 Secret

## 3 场景一：LLM 降级率升高
### 3.1 现象
- 告警：CopilotDiagnosisHighFallbackRate firing
- Dashboard：诊断 LLM 成功率下降

### 3.2 排查步骤
1. 检查 LLM Provider 状态
2. 检查 LLM API Key 有效性
3. 检查网络连通性
4. 检查 LLM 请求延迟趋势
5. 检查 LLM 错误日志

### 3.3 恢复操作
- API Key 过期 → 更新 Secret 并重启
- Provider 限流 → 调整请求频率或切换 Provider
- 网络问题 → 修复网络后自动恢复

## 4 场景二：RAG 无结果率升高
### 4.1 现象
- 告警：CopilotRAGHighNoResultRate firing
- Dashboard：RAG 命中率下降

### 4.2 排查步骤
1. 检查 Runbook 文件是否完整
2. 检查 Embedding 服务可用性
3. 检查向量索引构建状态
4. 检查查询分布变化
5. 运行 RAG 评估集

### 4.3 恢复操作
- Runbook 缺失 → 补充 Runbook 文件并重启
- Embedding 不可用 → 系统自动降级到 BM25+Struct
- 索引构建失败 → 重启服务重新构建
- 查询模式变化 → 补充评估用例和 Runbook

## 5 场景三：NLU 准确率下降
### 5.1 现象
- 告警：CopilotNLULowConfidenceRate firing
- CI：NLU 评估准确率低于 80%

### 5.2 排查步骤
1. 运行 NLU 评估集获取详细报告
2. 定位 F1 最低的意图
3. 分析失败 case 的输入模式
4. 检查是否有新增意图类型未覆盖

### 5.3 恢复操作
- 规则覆盖不足 → 补充 NLU 规则
- 新意图类型 → 在 nlu.go 中增加意图定义和规则
- 评估后验证 → 重跑评估确认恢复

## 6 场景四：诊断置信度下降
### 6.1 现象
- Dashboard：诊断置信度 P50 下降
- 诊断报告质量下降（用户反馈）

### 6.2 排查步骤
1. 检查证据采集完整性
2. 检查 Rule Analyzer 命中率
3. 检查 LLM Prompt 是否被意外修改
4. 检查诊断降级率

### 6.3 恢复操作
- 证据采集失败 → 检查 Redis/MySQL/Prometheus 连接
- Prompt 变更 → 回滚 Prompt 到上一版本
- LLM 降级 → 参见场景一

## 7 场景五：Embedding 服务异常
### 7.1 现象
- 向量检索无结果（但 BM25+Struct 正常）
- Dashboard：RAG 命中率未下降（因为降级到两路融合）

### 7.2 排查步骤
1. 检查 EMBEDDING_API_URL 配置
2. 检查 EMBEDDING_API_KEY 有效性
3. 手动调用 Embedding API 验证
4. 检查向量索引构建日志
5. 检查 vectorStore.Len()

### 7.3 恢复操作
- API 不可用 → 系统自动降级，无需手动干预
- Key 过期 → 更新 Secret 并重启
- 索引构建失败 → 重启服务重新构建
