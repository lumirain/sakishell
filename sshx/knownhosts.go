package sshx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
)

// hostKeyEntry 一条已记录的主机公钥
type hostKeyEntry struct {
	Host        string `json:"host"`        // host:port
	KeyType     string `json:"keyType"`     // e.g. ssh-ed25519
	PublicKey   string `json:"publicKey"`   // base64 编码的公钥 blob
	Fingerprint string `json:"fingerprint"` // SHA256 指纹
	FirstSeen   string `json:"firstSeen"`   // 首次记录时间（RFC3339）
}

// HostKeyStore 主机公钥信任存储（TOFU：首次连接记录，后续校验，类似 known_hosts）
type HostKeyStore struct {
	mu   sync.Mutex
	path string
	keys map[string]hostKeyEntry // key: "host:port"
}

// NewHostKeyStore 创建 TOFU 存储，keys 文件存放于 dir 下 host_keys.json
func NewHostKeyStore(dir string) (*HostKeyStore, error) {
	h := &HostKeyStore{
		path: filepath.Join(dir, "host_keys.json"),
		keys: map[string]hostKeyEntry{},
	}
	if err := h.load(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *HostKeyStore) load() error {
	data, err := os.ReadFile(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取主机密钥记录失败: %w", err)
	}
	var entries []hostKeyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("解析主机密钥记录失败: %w", err)
	}
	for _, e := range entries {
		h.keys[e.Host] = e
	}
	return nil
}

func (h *HostKeyStore) saveLocked() error {
	entries := make([]hostKeyEntry, 0, len(h.keys))
	for _, e := range h.keys {
		entries = append(entries, e)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}

// Callback 返回 ssh.HostKeyCallback：首次连接记录公钥，后续连接校验是否一致
func (h *HostKeyStore) Callback() ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		h.mu.Lock()
		defer h.mu.Unlock()

		addr := remote.String()
		fp := ssh.FingerprintSHA256(key)

		if prev, ok := h.keys[addr]; ok {
			if prev.Fingerprint != fp {
				return fmt.Errorf("主机 %s 的公钥已变更（指纹 %s → %s），可能遭受中间人攻击，请检查服务器",
					addr, prev.Fingerprint, fp)
			}
			return nil
		}

		h.keys[addr] = hostKeyEntry{
			Host:        addr,
			KeyType:     key.Type(),
			PublicKey:   string(key.Marshal()),
			Fingerprint: fp,
		}
		return h.saveLocked()
	}
}

// Reset 删除指定主机的信任记录（用户主动"忘记主机"时调用）
func (h *HostKeyStore) Reset(hostPort string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.keys[hostPort]; !ok {
		return errors.New("未找到该主机的信任记录")
	}
	delete(h.keys, hostPort)
	return h.saveLocked()
}

// List 返回全部已信任主机（host:port → 指纹）
func (h *HostKeyStore) List() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]string, len(h.keys))
	for addr, e := range h.keys {
		out[addr] = e.Fingerprint
	}
	return out
}
