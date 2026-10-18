//go:build !windows

package store

// 非 Windows 平台（Linux / macOS）凭据保护策略：
//   - 配置目录与文件权限为 0700 / 0600（见 store.go 的 MkdirAll/WriteFile）
//   - 若系统运行 Secret Service（gnome-keyring / kwallet），可后续接入
//     99designs/keyring 提升为系统钥匙串；当前版本以文件权限 + 用户隔离为准。
// 密码字段原样存储（不落盘明文日志；审计已做脱敏）。

// EncryptSecret 非 Windows 平台返回原样（权限由文件系统 0600 保证）
func EncryptSecret(plain string) (string, error) {
	return plain, nil
}

// DecryptSecret 非 Windows 平台返回原样
func DecryptSecret(stored string) (string, error) {
	return stored, nil
}
