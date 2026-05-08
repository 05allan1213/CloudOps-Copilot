package handler

import (
	"chatops-server/cache"
	"chatops-server/service"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type ChatRequest struct {
	Message   string `json:"message" binding:"required"`
	SessionID string `json:"session_id"`
}

type ChatResponse struct {
	Reply     string `json:"reply"`
	SessionID string `json:"session_id"`
}

// actionCommand LLM 返回的结构化指令
type actionCommand struct {
	Action    string `json:"action"`
	Namespace string `json:"namespace,omitempty"`
	Query     string `json:"query,omitempty"`
}

func Chat(c *gin.Context) {
	if cache.CheckRateLimit(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "请求过于频繁，请稍后再试"})
		return
	}

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "消息不能为空"})
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	cache.SaveMessage(sessionID, "user", req.Message)

	// 先检查缓存
	if cached, ok := cache.GetCachedQuery(req.Message); ok {
		cache.SaveMessage(sessionID, "assistant", cached)
		c.JSON(http.StatusOK, ChatResponse{Reply: cached, SessionID: sessionID})
		return
	}

	// 获取对话历史，转换为 LLM 消息格式
	history, _ := cache.GetHistory(sessionID)
	var llmHistory []service.LLMMessage
	for _, h := range history {
		llmHistory = append(llmHistory, service.LLMMessage{Role: h.Role, Content: h.Content})
	}

	// 调用 LLM 解析意图
	llmReply, err := service.Chat(llmHistory, req.Message)
	if err != nil {
		log.Printf("LLM 调用失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI 服务暂时不可用"})
		return
	}

	// 尝试解析为结构化指令
	reply := processLLMReply(llmReply)

	cache.SaveMessage(sessionID, "assistant", reply)
	cache.CacheQuery(req.Message, reply)

	c.JSON(http.StatusOK, ChatResponse{Reply: reply, SessionID: sessionID})
}

// processLLMReply 解析 LLM 回复，如果是 JSON 指令则执行对应查询
func processLLMReply(llmReply string) string {
	trimmed := strings.TrimSpace(llmReply)

	// 支持多行 JSON 指令（LLM 可能一次返回多条）
	lines := strings.Split(trimmed, "\n")
	var results []string
	parsed := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var cmd actionCommand
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			continue
		}
		if cmd.Action == "" {
			continue
		}
		parsed = true
		result := executeCommand(cmd)
		results = append(results, result)
	}

	if !parsed {
		return llmReply
	}
	return strings.Join(results, "\n")
}

// executeCommand 执行单条结构化指令
func executeCommand(cmd actionCommand) string {
	ns := cmd.Namespace
	if ns == "" {
		ns = "default"
	}

	var result string
	var err error

	switch cmd.Action {
	case "get_pods":
		result, err = service.GetPods(ns)
	case "get_deployments":
		result, err = service.GetDeployments(ns)
	case "get_services":
		result, err = service.GetServices(ns)
	case "get_nodes":
		result, err = service.GetNodes()
	case "query_prometheus":
		result, err = service.QueryPrometheus(cmd.Query)
	default:
		return "未知操作: " + cmd.Action
	}

	if err != nil {
		return "查询出错：" + err.Error()
	}
	return result
}

func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func generateSessionID() string {
	return "sess_" + randomString(16)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
