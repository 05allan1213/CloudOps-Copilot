package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

var (
	llmAPIKey string
	llmAPIURL string
)

func InitLLM() {
	llmAPIKey = os.Getenv("LLM_API_KEY")
	llmAPIURL = os.Getenv("LLM_API_URL")
	if llmAPIURL == "" {
		// DeepSeek 国内 API 地址
		llmAPIURL = "https://api.deepseek.com/v1/chat/completions"
	}
}

// LLMMessage 消息结构
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// llmRequest 请求体
type llmRequest struct {
	Model    string       `json:"model"`
	Messages []LLMMessage `json:"messages"`
}

// llmResponse 响应体
type llmResponse struct {
	Choices []struct {
		Message LLMMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// SystemPrompt 定义 AI 的角色和能力
const SystemPrompt = `你是一个智能运维助手（ChatOps），专门帮助用户查询和管理 Kubernetes 集群。

你可以执行以下操作，当用户的问题匹配时，返回对应的 JSON 指令：

1. 查询 Pod 列表/状态 → {"action": "get_pods", "namespace": "default"}
2. 查询 Deployment 列表 → {"action": "get_deployments", "namespace": "default"}
3. 查询 Service 列表 → {"action": "get_services", "namespace": "default"}
4. 查询 Node 状态 → {"action": "get_nodes"}
5. 查询 CPU 使用率 → {"action": "query_prometheus", "query": "cpu"}
6. 查询内存使用率 → {"action": "query_prometheus", "query": "memory"}

规则：
- 如果用户的问题匹配上述操作，只返回 JSON 指令，不要返回其他内容
- JSON 必须是单行，不要用 markdown 代码块包裹
- 如果用户的问题不匹配任何操作，用自然语言正常回答运维相关问题
- 如果用户问的不是运维相关问题，礼貌地引导回运维话题`

// Chat 调用 LLM API 获取回复
func Chat(history []LLMMessage, userMsg string) (string, error) {
	if llmAPIKey == "" {
		return "", fmt.Errorf("LLM_API_KEY 未配置")
	}

	// 构建消息列表：system prompt + 历史 + 当前消息
	messages := []LLMMessage{{Role: "system", Content: SystemPrompt}}
	messages = append(messages, history...)
	messages = append(messages, LLMMessage{Role: "user", Content: userMsg})

	reqBody, _ := json.Marshal(llmRequest{
		Model:    "deepseek-chat",
		Messages: messages,
	})

	req, _ := http.NewRequest("POST", llmAPIURL, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+llmAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用 LLM API 失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result llmResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 LLM 响应失败: %v", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("LLM API 错误: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("LLM 返回空结果")
	}

	return result.Choices[0].Message.Content, nil
}
