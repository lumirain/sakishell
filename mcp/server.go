// Package mcp 实现工具内置的 MCP Server：
//   - 提供 SSH 领域工具：ssh_ai_session_open / ssh_exec / ssh_session_close
//     （M4 追加 ssh_sftp_list / upload / download）
//   - 传输：stdio（headless 子命令，供各 AI 客户端 spawn）、
//     HTTP/SSE/Streamable HTTP（M3，供"网址"方式接入）
//   - 双形态：GUI 嵌入模式（事件广播给前端 AI 会话视图）与
//     headless 独立模式（不开 GUI，审计写入共享数据目录）
package mcp

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"sakishell/audit"
	"sakishell/sshx"
	"sakishell/store"
)

// Server 内置 MCP Server 服务端：持有连接存储、会话池、TOFU 主机密钥、审计
type Server struct {
	store    *store.Store
	pool     *sshx.Pool
	hostKeys *sshx.HostKeyStore
	audit    *audit.Logger

	// 客户端名（审计溯源）：stdio 会话固定，HTTP 会话在 M3 按会话记录
	clientName string

	// GUI 嵌入模式回调（headless 为 nil）
	// onSessionOpened: AI 共享会话打开时通知前端开 Tab
	OnSessionOpened func(info AIOpenInfo)
	// OnData: AI 会话输出转发给前端（复用 terminal:output 事件）
	OnData func(sessionID string, data []byte)

	// httpState HTTP MCP 服务状态（GUI 嵌入或 headless --http）
	httpState *httpServerState
}

// AIOpenInfo AI 会话打开时的信息
type AIOpenInfo struct {
	SessionID string `json:"id"`
	HostPort  string `json:"hostPort"`
	Title     string `json:"title"`
}

// NewServer 创建内置 MCP Server（dataDir 为空时使用系统用户配置目录）
func NewServer(dataDir string) (*Server, error) {
	st, err := store.New(dataDir)
	if err != nil {
		return nil, fmt.Errorf("初始化配置存储失败: %w", err)
	}
	hk, err := sshx.NewHostKeyStore(st.DataDir())
	if err != nil {
		return nil, fmt.Errorf("初始化主机密钥存储失败: %w", err)
	}
	al, err := audit.New(st.DataDir())
	if err != nil {
		return nil, fmt.Errorf("初始化审计日志失败: %w", err)
	}
	return &Server{
		store:      st,
		pool:       sshx.NewPool(),
		hostKeys:   hk,
		audit:      al,
		clientName: "unknown",
	}, nil
}

// Pool 返回会话池（GUI 模式复用）
func (s *Server) Pool() *sshx.Pool { return s.pool }

// Store 返回连接存储
func (s *Server) Store() *store.Store { return s.store }

// Audit 返回审计日志
func (s *Server) Audit() *audit.Logger { return s.audit }

// Close 关闭资源
func (s *Server) Close() {
	s.pool.CloseAll()
	_ = s.audit.Close()
}

// registerTools 注册全部 MCP 工具
func (s *Server) registerTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(
		mcp.NewTool("ssh_ai_session_open",
			mcp.WithDescription("打开一个 AI 共享 SSH 会话（可交互 PTY 终端，用户可同时在该会话手动输入命令）。"+
				"返回 session_id，后续 ssh_exec 使用。若 host/username 匹配已保存的连接配置则复用其凭据。"),
			mcp.WithString("host", mcp.Required(), mcp.Description("SSH 服务器主机地址")),
			mcp.WithNumber("port", mcp.Description("SSH 端口，默认 22")),
			mcp.WithString("username", mcp.Required(), mcp.Description("登录用户名")),
			mcp.WithString("auth_type", mcp.Description("认证方式：password（默认）或 key")),
			mcp.WithString("password", mcp.Description("密码认证时的密码；若匹配已保存连接可省略")),
			mcp.WithString("key_path", mcp.Description("密钥认证时的私钥路径；若匹配已保存连接可省略")),
			mcp.WithString("tab_title", mcp.Description("AI 会话标签标题（可选，默认 host:port）")),
		),
		s.handleSessionOpen,
	)
	mcpServer.AddTool(
		mcp.NewTool("ssh_exec",
			mcp.WithDescription("在指定 AI 共享会话的 shell 中执行命令。命令与输出会显示在用户可见的 AI 会话终端中。"+
				"返回 stdout 输出与退出码。适合执行任意 shell 命令（如查看状态、安装软件、修改配置）。"),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("ssh_ai_session_open 返回的会话 ID")),
			mcp.WithString("command", mcp.Required(), mcp.Description("要执行的 shell 命令")),
			mcp.WithNumber("timeout", mcp.Description("超时秒数，默认 60，最大 600")),
		),
		s.handleExec,
	)
	mcpServer.AddTool(
		mcp.NewTool("ssh_session_close",
			mcp.WithDescription("关闭 AI 共享 SSH 会话"),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("要关闭的会话 ID")),
		),
		s.handleSessionClose,
	)
	mcpServer.AddTool(
		mcp.NewTool("ssh_sftp_list",
			mcp.WithDescription("列出指定会话上远程目录的内容（SFTP）。"),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("ssh_ai_session_open 返回的会话 ID")),
			mcp.WithString("path", mcp.Required(), mcp.Description("远程目录路径，如 /home/user")),
		),
		s.handleSFTPList,
	)
	mcpServer.AddTool(
		mcp.NewTool("ssh_sftp_upload",
			mcp.WithDescription("把本机文件上传到远程服务器（SFTP）。local_path 为运行本工具的电脑上的文件路径。"),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("ssh_ai_session_open 返回的会话 ID")),
			mcp.WithString("local_path", mcp.Required(), mcp.Description("本机文件路径")),
			mcp.WithString("remote_path", mcp.Required(), mcp.Description("远程目标路径")),
		),
		s.handleSFTPUpload,
	)
	mcpServer.AddTool(
		mcp.NewTool("ssh_sftp_download",
			mcp.WithDescription("把远程服务器文件下载到本机（SFTP）。local_path 为运行本工具的电脑上的目标路径。"),
			mcp.WithString("session_id", mcp.Required(), mcp.Description("ssh_ai_session_open 返回的会话 ID")),
			mcp.WithString("remote_path", mcp.Required(), mcp.Description("远程文件路径")),
			mcp.WithString("local_path", mcp.Required(), mcp.Description("本机目标路径")),
		),
		s.handleSFTPDownload,
	)
}

// BuildMCPServer 装配 MCP server（stdio/HTTP 共用）
// 工具 handler 内部将错误转换为 IsError=true 的结果返回，不向框架抛 JSON-RPC 错误
func (s *Server) BuildMCPServer() *server.MCPServer {
	ms := server.NewMCPServer("sakishell", "1.0.0")
	s.registerTools(ms)
	return ms
}

// RunCLI 以 headless 子命令方式运行（main 入口的 "mcp" 分支）。
// 用法：sakishell mcp --stdio   （供 AI 客户端 spawn）
func RunCLI(args []string) error {
	mode := "stdio"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--stdio":
			mode = "stdio"
		case "--http":
			mode = "http"
		}
	}

	srv, err := NewServer("")
	if err != nil {
		return err
	}
	defer srv.Close()
	srv.clientName = mode

	ms := srv.BuildMCPServer()
	ctx := context.Background()

	if mode == "http" {
		return srv.RunHTTPCLI(args)
	}

	log.SetOutput(os.Stderr)
	log.Printf("[sakishell] MCP stdio 服务已启动（ctrl-c 退出）")
	stdioServer := server.NewStdioServer(ms)
	return stdioServer.Listen(ctx, os.Stdin, os.Stdout)
}

// helper：从参数取字符串 / 数字
func strArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func numArg(args map[string]any, key string, def int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return def
}

// waitCtx 用于工具内等待（带客户端请求取消）
func waitCtx(ctx context.Context, timeout time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return nil
	}
}
