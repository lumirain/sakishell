package mcp

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"sakishell/sshx"
)

// httpServerState 一个运行的 HTTP MCP 服务实例
type httpServerState struct {
	ln    net.Listener
	port  int
	token string
	srv   *http.Server
	done  chan struct{}
}

// UsePool 让外部注入共享会话池（GUI 嵌入模式与 App 共用，前端可统一关闭 AI 会话）
func (s *Server) UsePool(p *sshx.Pool) { s.pool = p }

// StartHTTP 以 Streamable HTTP + 传统 SSE 两种端点启动 MCP 服务。
// token 非空时要求请求头 Authorization: Bearer <token>。
// 返回监听端口（port=0 时自动分配）。
func (s *Server) StartHTTP(port int, token string) (int, error) {
	if port == 0 {
		port = 0 // 自动分配
	}
	if port < 0 || port > 65535 {
		return 0, fmt.Errorf("端口 %d 非法", port)
	}

	mux := http.NewServeMux()

	// Streamable HTTP（现代标准）：/mcp
	streamable := server.NewStreamableHTTPServer(s.BuildMCPServer())
	mux.Handle("/mcp", s.authMiddleware(token, streamable))

	// 传统 SSE：/sse + /message
	sse := server.NewSSEServer(s.BuildMCPServer())
	mux.Handle("/sse", s.authMiddleware(token, sse))
	mux.Handle("/message", s.authMiddleware(token, sse))

	// 健康检查 / 信息端点
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "sakishell MCP server\n- Streamable HTTP: /mcp\n- SSE: /sse (+ /message)\n")
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return 0, fmt.Errorf("监听端口失败: %w", err)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ln)
		close(done)
	}()

	s.httpState = &httpServerState{ln: ln, port: actualPort, token: token, srv: srv, done: done}
	log.Printf("[sakishell] MCP HTTP 服务已启动: http://127.0.0.1:%d/mcp (token=%v)", actualPort, token != "")
	return actualPort, nil
}

// StopHTTP 停止 HTTP MCP 服务
func (s *Server) StopHTTP() {
	if s.httpState == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.httpState.srv.Shutdown(ctx)
	s.httpState = nil
}

// HTTPPort 返回当前 HTTP 服务端口（未启动返回 0）
func (s *Server) HTTPPort() int {
	if s.httpState == nil {
		return 0
	}
	return s.httpState.port
}

// HTTPRunning 是否在运行
func (s *Server) HTTPRunning() bool { return s.httpState != nil }

// authMiddleware Bearer token 鉴权（token 为空则放行）
func (s *Server) authMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		got := strings.TrimPrefix(auth, "Bearer ")
		if auth == got && !strings.EqualFold(auth, "Bearer") {
			// 兼容 X-API-Key
			got = r.Header.Get("X-API-Key")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// parsePortToken 从 CLI 参数解析 --port / --token
func parsePortToken(args []string) (int, string, error) {
	port := 0
	token := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 < len(args) {
				p, err := strconv.Atoi(args[i+1])
				if err != nil {
					return 0, "", fmt.Errorf("--port 参数非法: %v", err)
				}
				port = p
				i++
			}
		case "--token":
			if i+1 < len(args) {
				token = args[i+1]
				i++
			}
		}
	}
	return port, token, nil
}

// RunHTTPCLI headless 的 --http 模式：阻塞直到 Ctrl-C
func (s *Server) RunHTTPCLI(args []string) error {
	port, token, err := parsePortToken(args)
	if err != nil {
		return err
	}
	actual, err := s.StartHTTP(port, token)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	log.Printf("[sakishell] MCP HTTP 服务运行中：")
	log.Printf("  本机:   http://127.0.0.1:%d/mcp", actual)
	log.Printf("  局域网: http://%s:%d/mcp（如需外部访问请使用 --token 设置鉴权）", host, actual)
	if token != "" {
		log.Printf("  鉴权:   Authorization: Bearer %s", token)
	}
	<-s.httpState.done
	return nil
}
