// Package audit 提供 AI 操作审计日志（JSONL 逐条追加落盘）。
// 记录每次 MCP 工具调用的来源、参数（密码脱敏）、结果状态与耗时，
// 与前端 AI 会话视图共同构成"对用户可见、可审计"的能力。
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Entry 一条审计记录
type Entry struct {
	Time       string `json:"time"`       // RFC3339 时间
	Source     string `json:"source"`     // MCP 客户端名 / 传输类型（stdio/http）
	SessionID  string `json:"sessionId"`  // AI 共享会话 ID
	Tool       string `json:"tool"`       // 工具名
	Args       any    `json:"args"`       // 参数（密码等敏感字段已脱敏）
	Result     string `json:"result"`     // 执行结果摘要（成功 / 错误信息）
	DurationMS int64  `json:"durationMs"` // 耗时（毫秒）
}

// Logger 审计日志写入器
type Logger struct {
	mu   sync.Mutex
	path string
	f    *os.File

	// OnLog 每条记录写入后回调（GUI 模式用于实时推送前端操作记录侧栏）
	OnLog func(e Entry)
}

// New 创建审计日志（追加模式）。dir 为数据目录，日志位于 dir/audit/audit.jsonl。
func New(dir string) (*Logger, error) {
	auditDir := filepath.Join(dir, "audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建审计目录失败: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(auditDir, "audit.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开审计日志失败: %w", err)
	}
	return &Logger{path: filepath.Join(auditDir, "audit.jsonl"), f: f}, nil
}

// Close 关闭日志文件
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		return l.f.Close()
	}
	return nil
}

// Log 追加一条审计记录
func (l *Logger) Log(e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = l.f.Write(append(data, '\n'))
	_ = l.f.Sync()
	if l.OnLog != nil {
		l.OnLog(e)
	}
}

// Path 返回审计日志文件路径
func (l *Logger) Path() string { return l.path }

// ReadTail 读取审计日志末尾 N 条记录（用于 UI 初始化回放）
func ReadTail(path string, n int) []Entry {
	if n <= 0 {
		n = 50
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := make([]Entry, 0, len(lines))
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(ln), &e); err == nil {
			out = append(out, e)
		}
	}
	return out
}
