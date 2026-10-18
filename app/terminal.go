package app

import (
	"fmt"
	"sync/atomic"

	"sakishell/sshx"
)

// 终端会话事件名（前端用 runtime.EventsOn 监听）
const (
	EvTerminalOutput = "terminal:output" // {id, data} PTY 输出
	EvTerminalClosed = "terminal:closed" // {id, reason} 会话关闭
)

var terminalSeq atomic.Uint64

// TerminalInfo 返回给前端的会话摘要
type TerminalInfo struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // user | ai_shared
	HostPort string `json:"hostPort"`
}

// OpenTerminal 打开一个 SSH 终端会话并返回会话 ID
// connID: 连接配置 ID；sessionType: "user"（手动终端）或 "ai_shared"（AI 共享会话）
func (a *App) OpenTerminal(connID, sessionType string, cols, rows int) (TerminalInfo, error) {
	conn, err := a.store.Get(connID)
	if err != nil {
		return TerminalInfo{}, err
	}

	seq := terminalSeq.Add(1)
	id := fmt.Sprintf("%s-%d", conn.ID, seq)
	typ := sshx.SessionType(sessionType)
	if typ != sshx.SessionAIShared {
		typ = sshx.SessionUser
	}

	sess := sshx.NewSession(id, typ)
	sess.OnData = func(sid string, data []byte) {
		a.emit(EvTerminalOutput, map[string]any{"id": sid, "data": string(data)})
	}
	sess.OnClose = func(sid string, err error) {
		a.pool.Remove(sid)
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		a.emit(EvTerminalClosed, map[string]any{"id": sid, "reason": reason})
	}

	if err := sess.Connect(conn.Host, conn.Port, conn.Username, conn.AuthType, conn.Password, conn.KeyPath, a.hostKeys.Callback()); err != nil {
		return TerminalInfo{}, err
	}
	if err := sess.OpenPtyShell(cols, rows); err != nil {
		sess.Close()
		return TerminalInfo{}, err
	}

	a.pool.Add(sess)
	return TerminalInfo{ID: id, Type: string(typ), HostPort: sess.HostPort}, nil
}

// TerminalInput 把用户键盘输入写入会话通道
func (a *App) TerminalInput(sessionID string, data string) error {
	sess, ok := a.pool.Get(sessionID)
	if !ok {
		return fmt.Errorf("会话 %s 不存在", sessionID)
	}
	return sess.Write([]byte(data))
}

// TerminalResize 调整会话 PTY 尺寸
func (a *App) TerminalResize(sessionID string, cols, rows int) error {
	sess, ok := a.pool.Get(sessionID)
	if !ok {
		return fmt.Errorf("会话 %s 不存在", sessionID)
	}
	return sess.Resize(cols, rows)
}

// CloseTerminal 关闭会话
func (a *App) CloseTerminal(sessionID string) error {
	sess, ok := a.pool.Get(sessionID)
	if !ok {
		return fmt.Errorf("会话 %s 不存在", sessionID)
	}
	sess.Close()
	return nil
}

// ListSessions 返回当前全部活跃会话
func (a *App) ListSessions() []TerminalInfo {
	list := a.pool.List()
	out := make([]TerminalInfo, 0, len(list))
	for _, s := range list {
		out = append(out, TerminalInfo{ID: s.ID, Type: string(s.Type), HostPort: s.HostPort})
	}
	return out
}
