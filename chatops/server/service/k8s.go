package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var k8sClient *kubernetes.Clientset

// InitK8s 初始化 K8s 客户端（集群内优先，否则用 kubeconfig）
func InitK8s() error {
	config, err := rest.InClusterConfig()
	if err != nil {
		// 集群外：使用 kubeconfig
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			home, _ := os.UserHomeDir()
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return fmt.Errorf("无法加载 K8s 配置: %v", err)
		}
	}
	k8sClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("创建 K8s 客户端失败: %v", err)
	}
	return nil
}

// GetPods 查询指定命名空间的 Pod 列表
func GetPods(namespace string) (string, error) {
	if k8sClient == nil {
		return "K8s 客户端未初始化（可能不在集群环境中）", nil
	}
	pods, err := k8sClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("查询 Pod 失败: %v", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Sprintf("命名空间 %s 中没有 Pod", namespace), nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("命名空间 %s 中共有 %d 个 Pod：\n", namespace, len(pods.Items)))
	for _, pod := range pods.Items {
		sb.WriteString(fmt.Sprintf("  - %s | 状态: %s | IP: %s\n", pod.Name, pod.Status.Phase, pod.Status.PodIP))
	}
	return sb.String(), nil
}

// GetDeployments 查询 Deployment 列表
func GetDeployments(namespace string) (string, error) {
	if k8sClient == nil {
		return "K8s 客户端未初始化（可能不在集群环境中）", nil
	}
	deps, err := k8sClient.AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("查询 Deployment 失败: %v", err)
	}
	if len(deps.Items) == 0 {
		return fmt.Sprintf("命名空间 %s 中没有 Deployment", namespace), nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("命名空间 %s 中共有 %d 个 Deployment：\n", namespace, len(deps.Items)))
	for _, d := range deps.Items {
		sb.WriteString(fmt.Sprintf("  - %s | 期望副本: %d | 就绪: %d\n", d.Name, *d.Spec.Replicas, d.Status.ReadyReplicas))
	}
	return sb.String(), nil
}

// GetServices 查询 Service 列表
func GetServices(namespace string) (string, error) {
	if k8sClient == nil {
		return "K8s 客户端未初始化（可能不在集群环境中）", nil
	}
	svcs, err := k8sClient.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("查询 Service 失败: %v", err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("命名空间 %s 中共有 %d 个 Service：\n", namespace, len(svcs.Items)))
	for _, s := range svcs.Items {
		sb.WriteString(fmt.Sprintf("  - %s | 类型: %s | ClusterIP: %s\n", s.Name, s.Spec.Type, s.Spec.ClusterIP))
	}
	return sb.String(), nil
}

// GetNodes 查询 Node 状态
func GetNodes() (string, error) {
	if k8sClient == nil {
		return "K8s 客户端未初始化（可能不在集群环境中）", nil
	}
	nodes, err := k8sClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("查询 Node 失败: %v", err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("集群共有 %d 个 Node：\n", len(nodes.Items)))
	for _, n := range nodes.Items {
		status := "NotReady"
		for _, c := range n.Status.Conditions {
			if c.Type == "Ready" && c.Status == "True" {
				status = "Ready"
			}
		}
		sb.WriteString(fmt.Sprintf("  - %s | 状态: %s\n", n.Name, status))
	}
	return sb.String(), nil
}
