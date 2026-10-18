// Package store 负责连接配置的持久化存储。
// 数据目录：
//   - Linux:   ~/.config/sakishell/
//   - Windows: %APPDATA%/sakishell/
// 密码字段在 M5 起加密落盘（Windows DPAPI / 其他平台权限保护）。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// Connection 一条 SSH 连接配置
type Connection struct {
	ID       string `json:"id"`                 // 唯一标识
	Name     string `json:"name"`               // 显示名称
	Group    string `json:"group"`              // 分组（空为默认组）
	Host     string `json:"host"`               // 主机地址
	Port     int    `json:"port"`               // 端口，默认 22
	Username string `json:"username"`           // 用户名
	AuthType string `json:"authType"`           // password | key
	Password string `json:"password,omitempty"` // 密码认证时的密码（M5 加密）
	KeyPath  string `json:"keyPath,omitempty"`  // 密钥认证时的私钥路径
}

// DefaultPort 默认 SSH 端口
const DefaultPort = 22

// Store 连接配置存储器
type Store struct {
	mu   sync.Mutex
	path string // 配置文件绝对路径
}

// New 创建 Store。dir 为空时使用系统用户配置目录。
// 若新数据目录（sakishell）尚不存在而旧目录（ssh-mcp-terminal）存在，自动迁移连接配置/主机密钥/审计，
// 兼容旧版本升级（旧目录保留不删除）。
func New(dir string) (*Store, error) {
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户配置目录失败: %w", err)
		}
		dir = filepath.Join(base, "sakishell")
		oldDir := filepath.Join(base, "ssh-mcp-terminal")
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if _, err := os.Stat(oldDir); err == nil {
				_ = migrateLegacyDir(oldDir, dir)
			}
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}
	return &Store{path: filepath.Join(dir, "connections.json")}, nil
}

// migrateLegacyDir 将旧数据目录内容复制到新目录（仅复制已知文件，失败不阻塞启动）
func migrateLegacyDir(oldDir, newDir string) error {
	entries := []string{"connections.json", "host_keys.json", "audit"}
	for _, name := range entries {
		src := filepath.Join(oldDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(newDir, name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			continue
		}
	}
	return nil
}

// DataDir 返回配置目录
func (s *Store) DataDir() string { return filepath.Dir(s.path) }

// List 返回全部连接，按分组+名称排序（密码解密为明文供 UI/连接使用）
func (s *Store) List() ([]Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conns, err := s.load()
	if err != nil {
		return nil, err
	}
	for i := range conns {
		if conns[i].Password != "" {
			plain, derr := DecryptSecret(conns[i].Password)
			if derr == nil {
				conns[i].Password = plain
			}
		}
	}
	sort.Slice(conns, func(i, j int) bool {
		if conns[i].Group != conns[j].Group {
			return conns[i].Group < conns[j].Group
		}
		return conns[i].Name < conns[j].Name
	})
	return conns, nil
}

// Get 按 ID 查询连接（密码解密为明文）
func (s *Store) Get(id string) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conns, err := s.load()
	if err != nil {
		return Connection{}, err
	}
	for _, c := range conns {
		if c.ID == id {
			if c.Password != "" {
				plain, derr := DecryptSecret(c.Password)
				if derr == nil {
					c.Password = plain
				}
			}
			return c, nil
		}
	}
	return Connection{}, fmt.Errorf("连接 %s 不存在", id)
}

// Save 新增或更新连接。ID 为空时自动生成；Name 重复时报错。
func (s *Store) Save(c Connection) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c.ID = strings.TrimSpace(c.ID)
	c.Name = strings.TrimSpace(c.Name)
	c.Host = strings.TrimSpace(c.Host)
	c.Username = strings.TrimSpace(c.Username)
	if c.Name == "" || c.Host == "" || c.Username == "" {
		return Connection{}, errors.New("名称、主机、用户名不能为空")
	}
	if c.Port <= 0 || c.Port > 65535 {
		c.Port = DefaultPort
	}
	if c.AuthType != "key" {
		c.AuthType = "password"
	}

	conns, err := s.load()
	if err != nil {
		return Connection{}, err
	}

	if c.ID == "" {
		c.ID = uuid.NewString()
	} else {
		// 更新模式：确认存在
		found := false
		for i := range conns {
			if conns[i].ID == c.ID {
				found = true
				break
			}
		}
		if !found {
			return Connection{}, fmt.Errorf("连接 %s 不存在，无法更新", c.ID)
		}
	}

	// 名称唯一性（同一分组内）
	for _, e := range conns {
		if e.ID != c.ID && strings.EqualFold(e.Name, c.Name) && strings.EqualFold(e.Group, c.Group) {
			return Connection{}, fmt.Errorf("分组 [%s] 下已存在同名连接 %q", c.Group, c.Name)
		}
	}

	// M5：敏感字段加密落盘（Windows DPAPI / 其他平台权限保护）
	enc := c
	if enc.AuthType == "password" && enc.Password != "" {
		se, err := EncryptSecret(enc.Password)
		if err != nil {
			return Connection{}, fmt.Errorf("加密密码失败: %w", err)
		}
		enc.Password = se
	}
	out := make([]Connection, 0, len(conns)+1)
	replaced := false
	for _, e := range conns {
		if e.ID == enc.ID {
			out = append(out, enc)
			replaced = true
		} else {
			out = append(out, e)
		}
	}
	if !replaced {
		out = append(out, enc)
	}
	return c, s.save(out)
}

// Delete 删除连接
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conns, err := s.load()
	if err != nil {
		return err
	}
	out := conns[:0]
	for _, c := range conns {
		if c.ID != id {
			out = append(out, c)
		}
	}
	if len(out) == len(conns) {
		return fmt.Errorf("连接 %s 不存在", id)
	}
	return s.save(out)
}

// load 读取全部连接（文件不存在时返回空列表）
func (s *Store) load() ([]Connection, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Connection{}, nil
		}
		return nil, fmt.Errorf("读取连接配置失败: %w", err)
	}
	var conns []Connection
	if err := json.Unmarshal(data, &conns); err != nil {
		return nil, fmt.Errorf("解析连接配置失败: %w", err)
	}
	if conns == nil {
		conns = []Connection{}
	}
	return conns, nil
}

// save 原子写入（临时文件 + rename），权限 0600
func (s *Store) save(conns []Connection) error {
	data, err := json.MarshalIndent(conns, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化连接配置失败: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("写入连接配置失败: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("保存连接配置失败: %w", err)
	}
	return nil
}
