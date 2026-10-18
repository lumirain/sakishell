package app

import (
	"fmt"
	"sync/atomic"

	"sakishell/sshx"
)

// SFTP 事件
const (
	// EvSFTPProgress 传输进度：{id, done, total}
	EvSFTPProgress = "sftp:progress"
	// EvSFTPDone 传输完成：{id, action, ok, error}
	EvSFTPDone = "sftp:done"
)

var sftpSeq atomic.Uint64

// SFTPInfo SFTP 会话信息
type SFTPInfo struct {
	ID       string `json:"id"`
	HostPort string `json:"hostPort"`
}

// OpenSFTP 打开一个 SFTP 会话（仅文件通道，不启动 shell），返回会话 ID
func (a *App) OpenSFTP(connID string) (SFTPInfo, error) {
	conn, err := a.store.Get(connID)
	if err != nil {
		return SFTPInfo{}, err
	}
	seq := sftpSeq.Add(1)
	id := fmt.Sprintf("sftp-%s-%d", conn.ID, seq)

	sess := sshx.NewSession(id, sshx.SessionSFTP)
	if err := sess.Connect(conn.Host, conn.Port, conn.Username, conn.AuthType, conn.Password, conn.KeyPath, a.hostKeys.Callback()); err != nil {
		return SFTPInfo{}, err
	}
	if _, err := sess.EnsureSFTP(); err != nil {
		sess.Close()
		return SFTPInfo{}, err
	}
	a.pool.Add(sess)
	return SFTPInfo{ID: id, HostPort: sess.HostPort}, nil
}

// CloseSFTP 关闭 SFTP 会话
func (a *App) CloseSFTP(sessionID string) error {
	sess, ok := a.pool.Get(sessionID)
	if !ok {
		return fmt.Errorf("SFTP 会话 %s 不存在", sessionID)
	}
	sess.Close()
	return nil
}

// SFTPList 列出目录
func (a *App) SFTPList(sessionID, path string) ([]sshx.SFTPEntry, error) {
	sess, ok := a.pool.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("SFTP 会话 %s 不存在", sessionID)
	}
	return sess.SFTPList(path)
}

// SFTPUpload 上传文件（带进度事件）
func (a *App) SFTPUpload(sessionID, localPath, remotePath string) error {
	sess, ok := a.pool.Get(sessionID)
	if !ok {
		return fmt.Errorf("SFTP 会话 %s 不存在", sessionID)
	}
	return sess.SFTPUpload(localPath, remotePath, func(done, total int64) {
		a.emit(EvSFTPProgress, map[string]any{"id": sessionID, "done": done, "total": total})
	})
}

// SFTPDownload 下载文件（带进度事件）
func (a *App) SFTPDownload(sessionID, remotePath, localPath string) error {
	sess, ok := a.pool.Get(sessionID)
	if !ok {
		return fmt.Errorf("SFTP 会话 %s 不存在", sessionID)
	}
	return sess.SFTPDownload(remotePath, localPath, func(done, total int64) {
		a.emit(EvSFTPProgress, map[string]any{"id": sessionID, "done": done, "total": total})
	})
}

// SFTPMkdir 新建远端目录
func (a *App) SFTPMkdir(sessionID, path string) error {
	sess, ok := a.pool.Get(sessionID)
	if !ok {
		return fmt.Errorf("SFTP 会话 %s 不存在", sessionID)
	}
	return sess.SFTPMkdir(path)
}

// SFTPRemove 删除远端文件/空目录
func (a *App) SFTPRemove(sessionID, path string) error {
	sess, ok := a.pool.Get(sessionID)
	if !ok {
		return fmt.Errorf("SFTP 会话 %s 不存在", sessionID)
	}
	return sess.SFTPRemove(path)
}

// SFTPRename 重命名/移动
func (a *App) SFTPRename(sessionID, oldPath, newPath string) error {
	sess, ok := a.pool.Get(sessionID)
	if !ok {
		return fmt.Errorf("SFTP 会话 %s 不存在", sessionID)
	}
	return sess.SFTPRename(oldPath, newPath)
}
