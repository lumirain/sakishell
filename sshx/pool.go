package sshx

import (
	"sync"
)

// Pool 管理所有活跃 SSH 会话（用户会话与 AI 共享会话统一纳管）
type Pool struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewPool 创建会话池
func NewPool() *Pool {
	return &Pool{sessions: map[string]*Session{}}
}

// Add 注册会话
func (p *Pool) Add(s *Session) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessions[s.ID] = s
}

// Get 按 ID 获取会话
func (p *Pool) Get(id string) (*Session, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[id]
	return s, ok
}

// Remove 从池中移除（不负责关闭）
func (p *Pool) Remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.sessions, id)
}

// SessionInfo 会话池快照（不含锁，供 UI/MCP 展示）
type SessionInfo struct {
	ID       string      `json:"id"`
	Type     SessionType `json:"type"`
	HostPort string      `json:"hostPort"`
	Closed   bool        `json:"closed"`
}

// List 返回全部会话快照
func (p *Pool) List() []SessionInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]SessionInfo, 0, len(p.sessions))
	for _, s := range p.sessions {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		out = append(out, SessionInfo{ID: s.ID, Type: s.Type, HostPort: s.HostPort, Closed: closed})
	}
	return out
}

// CloseAll 关闭并清空全部会话（应用退出时调用）
func (p *Pool) CloseAll() {
	p.mu.Lock()
	items := make([]*Session, 0, len(p.sessions))
	for _, s := range p.sessions {
		items = append(items, s)
	}
	p.sessions = map[string]*Session{}
	p.mu.Unlock()

	for _, s := range items {
		s.Close()
	}
}
