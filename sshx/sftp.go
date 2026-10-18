package sshx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/sftp"
)

// SFTP 引擎：每个 SSH 会话在需要时可惰性创建一个 SFTP 客户端通道，
// 与 shell 通道并行使用（面板文件管理 与 MCP sftp 工具共用）。

// SFTPEntry 目录项
type SFTPEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"modTime"`
}

var sftpOnce struct {
	sync.Mutex
	bySession map[string]*sftp.Client
}

func init() { sftpOnce.bySession = map[string]*sftp.Client{} }

// EnsureSFTP 获取/创建该会话的 SFTP 客户端（线程安全）
func (s *Session) EnsureSFTP() (*sftp.Client, error) {
	sftpOnce.Lock()
	defer sftpOnce.Unlock()
	if c, ok := sftpOnce.bySession[s.ID]; ok {
		return c, nil
	}
	if s.client == nil {
		return nil, fmt.Errorf("会话未连接")
	}
	c, err := sftp.NewClient(s.client)
	if err != nil {
		return nil, fmt.Errorf("初始化 SFTP 失败: %w", err)
	}
	sftpOnce.bySession[s.ID] = c
	return c, nil
}

// CloseSFTP 关闭会话的 SFTP 客户端
func (s *Session) CloseSFTP() {
	sftpOnce.Lock()
	defer sftpOnce.Unlock()
	if c, ok := sftpOnce.bySession[s.ID]; ok {
		_ = c.Close()
		delete(sftpOnce.bySession, s.ID)
	}
}

// SFTPList 列出目录
func (s *Session) SFTPList(path string) ([]SFTPEntry, error) {
	c, err := s.EnsureSFTP()
	if err != nil {
		return nil, err
	}
	info, err := c.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("读取目录 %s 失败: %w", path, err)
	}
	out := make([]SFTPEntry, 0, len(info)+1)
	if path != "/" {
		out = append(out, SFTPEntry{Name: "..", IsDir: true})
	}
	for _, fi := range info {
		out = append(out, SFTPEntry{
			Name:    fi.Name(),
			IsDir:   fi.IsDir(),
			Size:    fi.Size(),
			Mode:    fi.Mode().String(),
			ModTime: fi.ModTime(),
		})
	}
	return out, nil
}

// ProgressFunc 传输进度回调（done/total 字节；total<0 表示未知）
type ProgressFunc func(done, total int64)

// SFTPUpload 上传本地文件到远端
func (s *Session) SFTPUpload(localPath, remotePath string, progress ProgressFunc) error {
	c, err := s.EnsureSFTP()
	if err != nil {
		return err
	}
	l, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("打开本地文件失败: %w", err)
	}
	defer l.Close()

	st, err := l.Stat()
	if err != nil {
		return fmt.Errorf("读取本地文件信息失败: %w", err)
	}
	total := st.Size()

	r, err := c.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("打开远端文件失败: %w", err)
	}
	defer r.Close()

	if err := copyWithProgress(r, l, total, progress); err != nil {
		return fmt.Errorf("上传失败: %w", err)
	}
	return nil
}

// SFTPDownload 下载远端文件到本地
func (s *Session) SFTPDownload(remotePath, localPath string, progress ProgressFunc) error {
	c, err := s.EnsureSFTP()
	if err != nil {
		return err
	}
	if dir := filepath.Dir(localPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建本地目录失败: %w", err)
		}
	}

	r, err := c.Open(remotePath)
	if err != nil {
		return fmt.Errorf("打开远端文件失败: %w", err)
	}
	defer r.Close()
	st, _ := r.Stat()
	total := int64(-1)
	if st != nil {
		total = st.Size()
	}

	l, err := os.OpenFile(localPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("创建本地文件失败: %w", err)
	}
	defer l.Close()

	if err := copyWithProgress(l, r, total, progress); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	return nil
}

// SFTPMkdir 创建远端目录
func (s *Session) SFTPMkdir(path string) error {
	c, err := s.EnsureSFTP()
	if err != nil {
		return err
	}
	return c.Mkdir(path)
}

// SFTPRemove 删除远端文件或空目录
func (s *Session) SFTPRemove(path string) error {
	c, err := s.EnsureSFTP()
	if err != nil {
		return err
	}
	st, err := c.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s 失败: %w", path, err)
	}
	if st.IsDir() {
		return c.RemoveDirectory(path)
	}
	return c.Remove(path)
}

// SFTPRename 重命名/移动
func (s *Session) SFTPRename(oldPath, newPath string) error {
	c, err := s.EnsureSFTP()
	if err != nil {
		return err
	}
	return c.Rename(oldPath, newPath)
}

// copyWithProgress 带进度回调的拷贝
func copyWithProgress(dst io.Writer, src io.Reader, total int64, progress ProgressFunc) error {
	if progress == nil {
		_, err := io.Copy(dst, src)
		return err
	}
	buf := make([]byte, 256*1024)
	var done int64
	lastReport := time.Now()
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if time.Since(lastReport) > 100*time.Millisecond {
				progress(done, total)
				lastReport = time.Now()
			}
		}
		if rerr == io.EOF {
			progress(done, total)
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}
