package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// exitMarkerRe 匹配 AI 命令结束标记，捕获退出码
var exitMarkerRe = regexp.MustCompile(`\[AI exit (-?\d+)\]`)

// ansiRe 剥离终端转义序列（含 bracketed paste 等 ? 参数序列）
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07]*\x07`)

// promptRe 匹配以交互 shell 提示符开头的行（user@host:/path$ ...），整行连同命令回显一起剥离
var promptRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+@[A-Za-z0-9_.-]+(:[^$]*)?\$`)

// echoRe 匹配 echo 命令回显行（PTY 回显的注入命令）
var echoRe = regexp.MustCompile(`^echo -e ".*"$`)

// SessionType 会话类型
type SessionType string

const (
	SessionUser     SessionType = "user"      // 用户手动终端会话
	SessionAIShared SessionType = "ai_shared" // AI 共享会话（AI + 用户共用 PTY）
	SessionSFTP     SessionType = "sftp"      // SFTP 文件管理会话（无 shell）
)

// Session 一个活跃的 SSH 会话：一条 TCP 连接 + 一个交互 shell 通道
type Session struct {
	ID       string
	Type     SessionType
	HostPort string // host:port，便于溯源

	client  *ssh.Client
	channel ssh.Channel
	reqs    <-chan *ssh.Request

	mu     sync.Mutex
	closed bool

	// execMu 串行化 AI 命令执行（一次仅一个）
	execMu sync.Mutex
	exec   *execState

	// 输出回调：PTY 收到数据时触发（前端 xterm 写入 + M2 起供 MCP 收集）
	OnData func(sessionID string, data []byte)
	// 会话关闭回调（正常或异常）
	OnClose func(sessionID string, err error)
}

// execState 一次 AI 命令注入的执行状态
type execState struct {
	collecting bool
	buf        bytes.Buffer
	done       chan struct{}
	exitCode   int
}

// ExecResult AI 命令执行结果
type ExecResult struct {
	Output   string `json:"output"`   // 命令输出（已剥离 ANSI 与标记行）
	ExitCode int    `json:"exitCode"` // 退出码
}

// ExecInShellError 执行失败（非零退出码）时返回，携带输出供 AI 参考
type ExecInShellError struct {
	Result ExecResult
}

func (e *ExecInShellError) Error() string {
	return fmt.Sprintf("命令执行失败，退出码 %d", e.Result.ExitCode)
}

// NewSession 创建会话壳（尚未连接）
func NewSession(id string, typ SessionType) *Session {
	return &Session{ID: id, Type: typ}
}

// Connect 建立 SSH 连接（含认证与主机校验）
func (s *Session) Connect(host string, port int, username, authType, password, keyPath string, hostKeyCb ssh.HostKeyCallback) error {
	if host == "" || username == "" {
		return fmt.Errorf("主机与用户名不能为空")
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	s.HostPort = addr

	cfg := &ssh.ClientConfig{
		User:            username,
		HostKeyCallback: hostKeyCb,
		Timeout:         15 * time.Second,
	}

	switch authType {
	case "key":
		if keyPath == "" {
			return fmt.Errorf("密钥认证需要提供私钥路径")
		}
		signer, err := loadPrivateKey(keyPath)
		if err != nil {
			return fmt.Errorf("加载私钥失败: %w", err)
		}
		cfg.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	default: // password
		if password == "" {
			return fmt.Errorf("密码认证需要提供密码")
		}
		cfg.Auth = []ssh.AuthMethod{ssh.Password(password)}
	}

	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return fmt.Errorf("连接 %s 失败: %w", addr, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SSH 握手失败: %w", err)
	}
	s.client = ssh.NewClient(sshConn, chans, reqs)
	return nil
}

// OpenPtyShell 请求 PTY 并启动用户默认 shell（交互终端模式）
func (s *Session) OpenPtyShell(cols, rows int) error {
	if s.client == nil {
		return fmt.Errorf("会话未连接")
	}
	ch, reqs, err := s.client.OpenChannel("session", nil)
	if err != nil {
		return fmt.Errorf("打开会话通道失败: %w", err)
	}

	// 请求 PTY（xterm-256color）
	ptyReq := struct {
		Term   string
		Cols   uint32
		Rows   uint32
		Width  uint32
		Height uint32
		Modes  string
	}{"xterm-256color", uint32(cols), uint32(rows), uint32(cols * 8), uint32(rows * 16), ""}
	ok, err := ch.SendRequest("pty-req", true, ssh.Marshal(&ptyReq))
	if err != nil || !ok {
		ch.Close()
		return fmt.Errorf("请求 PTY 失败 (err=%v, ok=%v)", err, ok)
	}

	// 启动 shell
	ok, err = ch.SendRequest("shell", true, nil)
	if err != nil || !ok {
		ch.Close()
		return fmt.Errorf("启动 shell 失败 (err=%v, ok=%v)", err, ok)
	}

	s.channel = ch
	s.reqs = reqs

	go s.readLoop()
	go s.handleRequests()
	return nil
}

// readLoop 持续读取通道输出并回调
func (s *Session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.channel.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.notifyData(chunk)
		}
		if err != nil {
			s.closeWithError(err)
			return
		}
	}
}

// notifyData 推送输出给 UI，并喂给进行中的 AI 命令收集器
func (s *Session) notifyData(data []byte) {
	if s.OnData != nil {
		s.OnData(s.ID, data)
	}
	s.execMu.Lock()
	defer s.execMu.Unlock()
	if s.exec != nil && s.exec.collecting {
		s.exec.buf.Write(data)
		if m := exitMarkerRe.FindSubmatch(s.exec.buf.Bytes()); m != nil {
			if code, err := strconv.Atoi(string(m[1])); err == nil {
				s.exec.exitCode = code
				s.exec.collecting = false
				close(s.exec.done)
			}
		}
	}
}

// ExecInShell 把命令注入共享 PTY shell 执行，返回输出与退出码。
// 命令与标记行会在终端中可见（用户可观察 AI 操作）；同一会话同一时刻只允许一个 AI 命令。
// 输出边界通过唯一结束标记定位，避免混入后续用户输入。
func (s *Session) ExecInShell(ctx context.Context, cmd string, timeout time.Duration) (*ExecResult, error) {
	if strings.TrimSpace(cmd) == "" {
		return nil, errors.New("命令不能为空")
	}

	s.execMu.Lock()
	if s.exec != nil && s.exec.collecting {
		s.execMu.Unlock()
		return nil, errors.New("该会话已有 AI 命令在执行中，请等待完成")
	}
	st := &execState{done: make(chan struct{})}
	s.exec = st
	s.execMu.Unlock()

	// 注入命令：用 echo -e 生成开始/结束标记（含 ANSI 美化与退出码）。
	// 注意不能用裸 ANSI 包裹标记文本——那会被 shell 当作命令解析；
	// echo -e 输出的才是终端显示层的内容，同时标记文本保持可被正则匹配。
	st.collecting = true
	inject := fmt.Sprintf("\necho -e \"\\033[2m[AI exec]\\033[0m\"\n%s\necho -e \"\\033[2m[AI exit $?]\\033[0m\"\n", cmd)
	if err := s.Write([]byte(inject)); err != nil {
		return nil, fmt.Errorf("写入命令失败: %w", err)
	}

	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	select {
	case <-st.done:
		out := cleanOutput(st.buf.String())
		res := ExecResult{Output: out, ExitCode: st.exitCode}
		if st.exitCode != 0 {
			return &res, &ExecInShellError{Result: res}
		}
		return &res, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("命令执行超时（%s），输出可能不完整", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// cleanOutput 剥离 ANSI 序列与 PTY 噪音（提示符、回显、标记行），返回干净的 stdout
func cleanOutput(raw string) string {
	clean := ansiRe.ReplaceAllString(raw, "")
	lines := strings.Split(clean, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "[AI exec]" || t == "" || strings.HasPrefix(t, "[AI exit ") {
			continue
		}
		if promptRe.MatchString(t) || echoRe.MatchString(t) {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// handleRequests 处理通道请求（如远程窗口变化，忽略即可）
func (s *Session) handleRequests() {
	for req := range s.reqs {
		switch req.Type {
		case "shell", "pty-req", "window-change":
			if req.WantReply {
				req.Reply(true, nil)
			}
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// Write 写入数据到通道（用户键盘输入 / AI 命令注入）
func (s *Session) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.channel == nil {
		return fmt.Errorf("会话已关闭")
	}
	_, err := s.channel.Write(data)
	return err
}

// Resize 调整 PTY 窗口尺寸
func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.channel == nil {
		return fmt.Errorf("会话已关闭")
	}
	if cols < 2 || rows < 2 {
		cols, rows = 80, 24
	}
	req := struct {
		Cols   uint32
		Rows   uint32
		Width  uint32
		Height uint32
	}{uint32(cols), uint32(rows), uint32(cols * 8), uint32(rows * 16)}
	_, err := s.channel.SendRequest("window-change", false, ssh.Marshal(&req))
	return err
}

// Close 主动关闭会话
func (s *Session) Close() error {
	s.closeWithError(nil)
	return nil
}

func (s *Session) closeWithError(err error) {
	s.mu.Lock()
	already := s.closed
	s.closed = true
	ch := s.channel
	s.mu.Unlock()

	if already {
		return
	}
	if ch != nil {
		ch.Close()
	}
	if s.client != nil {
		s.client.Close()
	}
	if s.OnClose != nil {
		s.OnClose(s.ID, err)
	}
}

// IsClosed 判断会话是否已关闭
func (s *Session) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// loadPrivateKey 从路径加载私钥（支持 PEM 与加密私钥提示走 ssh-agent 场景之外的简单情况）
func loadPrivateKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		// 尝试解析加密私钥：M1 简化不支持口令私钥，给出明确提示
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			return nil, fmt.Errorf("私钥已加密（带口令），当前版本暂不支持口令私钥，请使用无口令私钥或密码认证")
		}
		return nil, err
	}
	return signer, nil
}
