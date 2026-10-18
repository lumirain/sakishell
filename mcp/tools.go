package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"

	"sakishell/audit"
	"sakishell/sshx"
)

// openParams 统一会话打开参数（来自工具参数或已保存连接）
type openParams struct {
	host     string
	port     int
	username string
	authType string
	password string
	keyPath  string
	title    string
}

// resolveCredentials 优先复用已保存连接凭据，否则使用调用方传入参数
func (s *Server) resolveCredentials(args map[string]any) (openParams, error) {
	p := openParams{
		host:     strArg(args, "host"),
		port:     numArg(args, "port", 22),
		username: strArg(args, "username"),
		authType: strArg(args, "auth_type"),
		password: strArg(args, "password"),
		keyPath:  strArg(args, "key_path"),
		title:    strArg(args, "tab_title"),
	}
	if p.host == "" || p.username == "" {
		return p, errors.New("参数缺失：host 与 username 必填")
	}
	if p.authType != "key" {
		p.authType = "password"
	}
	if p.title == "" {
		p.title = fmt.Sprintf("%s:%d", p.host, p.port)
	}

	// 匹配已保存连接（host + port + username）
	conns, err := s.store.List()
	if err == nil {
		for _, c := range conns {
			if c.Host == p.host && c.Port == p.port && c.Username == p.username {
				p.authType = c.AuthType
				if c.AuthType == "key" && c.KeyPath != "" {
					p.keyPath = c.KeyPath
				} else if c.Password != "" {
					p.password = c.Password
				}
				break
			}
		}
	}

	// 校验认证信息完整
	if p.authType == "key" && p.keyPath == "" {
		return p, errors.New("密钥认证需要 key_path（或先在工具中保存该主机的连接配置）")
	}
	if p.authType == "password" && p.password == "" {
		return p, errors.New("密码认证需要 password（或先在工具中保存该主机的连接配置）")
	}
	return p, nil
}

// sanitizeArgs 审计脱敏：隐藏密码
func sanitizeArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if strings.Contains(strings.ToLower(k), "password") {
			out[k] = "***"
		} else {
			out[k] = v
		}
	}
	return out
}

// handleSessionOpen ssh_ai_session_open
func (s *Server) handleSessionOpen(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	args := req.GetArguments()
	p, err := s.resolveCredentials(args)
	if err != nil {
		return errResult(err), nil
	}

	// 创建 AI 共享会话
	sess := sshx.NewSession(uuid.NewString(), sshx.SessionAIShared)
	if s.OnData != nil {
		sess.OnData = s.OnData
	}
	sess.OnClose = func(sid string, e error) {
		s.pool.Remove(sid)
	}

	if err := sess.Connect(p.host, p.port, p.username, p.authType, p.password, p.keyPath, s.hostKeys.Callback()); err != nil {
		s.audit.Log(audit.Entry{Time: now(), Source: s.clientName, Tool: "ssh_ai_session_open", Args: sanitizeArgs(args), Result: "连接失败: " + err.Error(), DurationMS: ms(start)})
		return errResult(fmt.Errorf("连接失败: %w", err)), nil
	}
	if err := sess.OpenPtyShell(80, 24); err != nil {
		sess.Close()
		s.audit.Log(audit.Entry{Time: now(), Source: s.clientName, Tool: "ssh_ai_session_open", Args: sanitizeArgs(args), Result: "打开 shell 失败: " + err.Error(), DurationMS: ms(start)})
		return errResult(fmt.Errorf("打开 shell 失败: %w", err)), nil
	}
	s.pool.Add(sess)

	// GUI 嵌入模式：通知前端打开 AI 会话 Tab
	if s.OnSessionOpened != nil {
		s.OnSessionOpened(AIOpenInfo{SessionID: sess.ID, HostPort: sess.HostPort, Title: p.title})
	}

	s.audit.Log(audit.Entry{Time: now(), Source: s.clientName, SessionID: sess.ID, Tool: "ssh_ai_session_open", Args: sanitizeArgs(args), Result: "ok", DurationMS: ms(start)})

	return textResult(fmt.Sprintf(`{"session_id": %q, "host_port": %q, "title": %q}`+"\n提示：AI 共享会话已打开，用户在工具界面可以看到该会话并手动输入命令。后续用 ssh_exec(session_id=...) 执行命令。",
		sess.ID, sess.HostPort, p.title)), nil
}

// handleExec ssh_exec
func (s *Server) handleExec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	args := req.GetArguments()
	sessionID := strArg(args, "session_id")
	cmd := strArg(args, "command")
	timeout := numArg(args, "timeout", 60)
	if timeout > 600 {
		timeout = 600
	}

	sess, ok := s.pool.Get(sessionID)
	if !ok {
		return errResult(fmt.Errorf("会话 %s 不存在，请先调用 ssh_ai_session_open 打开会话", sessionID)), nil
	}

	res, err := sess.ExecInShell(ctx, cmd, time.Duration(timeout)*time.Second)

	status := "ok"
	if err != nil {
		status = "err: " + err.Error()
	}
	s.audit.Log(audit.Entry{Time: now(), Source: s.clientName, SessionID: sessionID, Tool: "ssh_exec", Args: sanitizeArgs(args), Result: status, DurationMS: ms(start)})

	if err != nil {
		var execErr *sshx.ExecInShellError
		if errors.As(err, &execErr) {
			// 命令执行了但退出码非零：返回输出 + 错误标记
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.TextContent{Type: "text", Text: fmt.Sprintf(
					"命令执行完毕但退出码非零：\n退出码: %d\n输出:\n%s", execErr.Result.ExitCode, execErr.Result.Output)}},
				IsError: true,
			}, nil
		}
		return errResult(err), nil
	}

	return textResult(fmt.Sprintf("退出码: %d\n输出:\n%s", res.ExitCode, res.Output)), nil
}

// handleSessionClose ssh_session_close
func (s *Server) handleSessionClose(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	args := req.GetArguments()
	sessionID := strArg(args, "session_id")

	sess, ok := s.pool.Get(sessionID)
	if !ok {
		return errResult(fmt.Errorf("会话 %s 不存在", sessionID)), nil
	}
	sess.Close()
	s.audit.Log(audit.Entry{Time: now(), Source: s.clientName, SessionID: sessionID, Tool: "ssh_session_close", Args: sanitizeArgs(args), Result: "ok", DurationMS: ms(start)})
	return textResult(fmt.Sprintf("会话 %s 已关闭", sessionID)), nil
}

// ---- SFTP 工具 ----

func (s *Server) getSession(sessionID string) (*sshx.Session, error) {
	sess, ok := s.pool.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("会话 %s 不存在，请先调用 ssh_ai_session_open", sessionID)
	}
	return sess, nil
}

// handleSFTPList ssh_sftp_list
func (s *Server) handleSFTPList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	args := req.GetArguments()
	sessionID, path := strArg(args, "session_id"), strArg(args, "path")

	sess, err := s.getSession(sessionID)
	if err != nil {
		return errResult(err), nil
	}
	entries, err := sess.SFTPList(path)
	status := "ok"
	if err != nil {
		status = "err: " + err.Error()
	}
	s.audit.Log(audit.Entry{Time: now(), Source: s.clientName, SessionID: sessionID, Tool: "ssh_sftp_list", Args: sanitizeArgs(args), Result: status, DurationMS: ms(start)})
	if err != nil {
		return errResult(fmt.Errorf("列出目录失败: %w", err)), nil
	}

	lines := make([]string, 0, len(entries)+1)
	lines = append(lines, fmt.Sprintf("目录 %s：", path))
	for _, e := range entries {
		kind := "文件"
		if e.IsDir {
			kind = "目录"
		}
		lines = append(lines, fmt.Sprintf("[%s] %-6s %10d  %s  %s", kind, e.Mode, e.Size, e.ModTime.Format("2006-01-02 15:04"), e.Name))
	}
	return textResult(strings.Join(lines, "\n")), nil
}

// handleSFTPUpload ssh_sftp_upload
func (s *Server) handleSFTPUpload(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	args := req.GetArguments()
	sessionID, lp, rp := strArg(args, "session_id"), strArg(args, "local_path"), strArg(args, "remote_path")

	sess, err := s.getSession(sessionID)
	if err != nil {
		return errResult(err), nil
	}
	if lp == "" || rp == "" {
		return errResult(fmt.Errorf("local_path 与 remote_path 必填")), nil
	}
	if err := sess.SFTPUpload(lp, rp, nil); err != nil {
		s.audit.Log(audit.Entry{Time: now(), Source: s.clientName, SessionID: sessionID, Tool: "ssh_sftp_upload", Args: sanitizeArgs(args), Result: "err: " + err.Error(), DurationMS: ms(start)})
		return errResult(fmt.Errorf("上传失败: %w", err)), nil
	}
	s.audit.Log(audit.Entry{Time: now(), Source: s.clientName, SessionID: sessionID, Tool: "ssh_sftp_upload", Args: sanitizeArgs(args), Result: "ok", DurationMS: ms(start)})
	return textResult(fmt.Sprintf("已上传 %s → %s", lp, rp)), nil
}

// handleSFTPDownload ssh_sftp_download
func (s *Server) handleSFTPDownload(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	args := req.GetArguments()
	sessionID, rp, lp := strArg(args, "session_id"), strArg(args, "remote_path"), strArg(args, "local_path")

	sess, err := s.getSession(sessionID)
	if err != nil {
		return errResult(err), nil
	}
	if lp == "" || rp == "" {
		return errResult(fmt.Errorf("remote_path 与 local_path 必填")), nil
	}
	if err := sess.SFTPDownload(rp, lp, nil); err != nil {
		s.audit.Log(audit.Entry{Time: now(), Source: s.clientName, SessionID: sessionID, Tool: "ssh_sftp_download", Args: sanitizeArgs(args), Result: "err: " + err.Error(), DurationMS: ms(start)})
		return errResult(fmt.Errorf("下载失败: %w", err)), nil
	}
	s.audit.Log(audit.Entry{Time: now(), Source: s.clientName, SessionID: sessionID, Tool: "ssh_sftp_download", Args: sanitizeArgs(args), Result: "ok", DurationMS: ms(start)})
	return textResult(fmt.Sprintf("已下载 %s → %s", rp, lp)), nil
}

// ---- 结果构造辅助 ----

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{mcp.TextContent{Type: "text", Text: text}}}
}

func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{mcp.TextContent{Type: "text", Text: err.Error()}}, IsError: true}
}

func now() string {
	return time.Now().Format(time.RFC3339)
}

func ms(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}
