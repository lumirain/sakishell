package app

import (
	"fmt"
	"os"
	"strconv"

	"sakishell/audit"
)

// MCP 相关事件名
const (
	// EvAISessionOpened AI 共享会话打开（GUI 嵌入模式）：{id, hostPort, title}
	EvAISessionOpened = "ai:session_opened"
	// EvAIAudit AI 操作审计条目（实时推送操作记录侧栏）：audit.Entry
	EvAIAudit = "ai:audit"
)

// MCPStatus MCP 服务状态
type MCPStatus struct {
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	URL     string `json:"url"`
	Token   string `json:"token"`
}

// StartMCPServer 启动 GUI 内嵌的 MCP HTTP 服务（"网址"方式接入）。
// port=0 自动分配；token 非空启用 Bearer 鉴权。
func (a *App) StartMCPServer(port int, token string) (MCPStatus, error) {
	if a.mcpServer == nil {
		return MCPStatus{}, fmt.Errorf("MCP Server 未初始化")
	}
	if a.mcpServer.HTTPRunning() {
		return a.GetMCPStatus(), nil
	}
	actual, err := a.mcpServer.StartHTTP(port, token)
	if err != nil {
		return MCPStatus{}, err
	}
	return MCPStatus{
		Running: true,
		Port:    actual,
		URL:     "http://127.0.0.1:" + strconv.Itoa(actual) + "/mcp",
		Token:   token,
	}, nil
}

// StopMCPServer 停止 GUI 内嵌的 MCP HTTP 服务
func (a *App) StopMCPServer() {
	if a.mcpServer != nil {
		a.mcpServer.StopHTTP()
	}
}

// GetMCPStatus 查询 MCP 服务状态
func (a *App) GetMCPStatus() MCPStatus {
	if a.mcpServer == nil || !a.mcpServer.HTTPRunning() {
		return MCPStatus{Running: false}
	}
	port := a.mcpServer.HTTPPort()
	return MCPStatus{
		Running: true,
		Port:    port,
		URL:     "http://127.0.0.1:" + strconv.Itoa(port) + "/mcp",
	}
}

// GetExecutablePath 返回当前可执行文件路径（向导页生成 stdio 接入命令用）
func (a *App) GetExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "sakishell"
	}
	return exe
}

// AuditHistory 返回最近 N 条审计记录（操作记录侧栏初始化用）
func (a *App) AuditHistory(limit int) []audit.Entry {
	if a.mcpServer == nil {
		return nil
	}
	return audit.ReadTail(a.mcpServer.Audit().Path(), limit)
}
