package main

import (
	"chatops-server/cache"
	"chatops-server/handler"
	"chatops-server/service"
	"log"

	"github.com/gin-gonic/gin"
)

func main() {
	// 初始化 Redis
	cache.Init()
	log.Println("Redis connected")

	// 初始化 LLM
	service.InitLLM()
	log.Println("LLM service initialized")

	// 初始化 K8s（非集群环境下会打印警告但不退出）
	if err := service.InitK8s(); err != nil {
		log.Printf("K8s 初始化警告: %v（非集群环境可忽略）", err)
	} else {
		log.Println("K8s client initialized")
	}

	// 初始化 Prometheus
	service.InitPrometheus()
	log.Println("Prometheus client initialized")

	r := gin.Default()

	// 静态文件
	r.Static("/static", "./web")
	r.StaticFile("/", "./web/index.html")

	// API 路由
	api := r.Group("/api")
	{
		api.POST("/chat", handler.Chat)
		api.GET("/health", handler.Health)
	}

	log.Println("ChatOps server starting on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
