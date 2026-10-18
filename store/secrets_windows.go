//go:build windows

package store

import (
	"encoding/base64"
	"fmt"
	"syscall"
	"unsafe"
)

// Windows DPAPI 凭据加密：使用当前用户主密钥加密，仅本用户可解密。

var (
	dllCrypt32            = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtectData   = dllCrypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = dllCrypt32.NewProc("CryptUnprotectData")
	procLocalFree          = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(data []byte) *dataBlob {
	if len(data) == 0 {
		return &dataBlob{}
	}
	b := &dataBlob{cbData: uint32(len(data))}
	b.pbData = &data[0]
	return b
}

func (b *dataBlob) bytes() []byte {
	if b.cbData == 0 || b.pbData == nil {
		return nil
	}
	return unsafe.Slice(b.pbData, int(b.cbData))
}

func (b *dataBlob) free() {
	if b.pbData != nil {
		procLocalFree.Call(uintptr(unsafe.Pointer(b.pbData)))
	}
}

// encryptWithDPAPI 用 DPAPI 加密
func encryptWithDPAPI(plain []byte) ([]byte, error) {
	in := newBlob(plain)
	var out dataBlob
	r1, _, e1 := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(in)),
		0, // description
		0, // entropy
		0, // reserved
		0, // prompt struct
		0, // flags
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("CryptProtectData 失败: %v", e1)
	}
	defer out.free()
	return out.bytes(), nil
}

// decryptWithDPAPI 用 DPAPI 解密
func decryptWithDPAPI(cipher []byte) ([]byte, error) {
	in := newBlob(cipher)
	var out dataBlob
	r1, _, e1 := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(in)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("CryptUnprotectData 失败: %v", e1)
	}
	defer out.free()
	return out.bytes(), nil
}

// EncryptSecret 加密敏感字段（密码），返回带前缀的 base64 密文
func EncryptSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	enc, err := encryptWithDPAPI([]byte(plain))
	if err != nil {
		return "", err
	}
	return "enc:v1:" + base64.StdEncoding.EncodeToString(enc), nil
}

// DecryptSecret 解密 EncryptSecret 的输出；无前缀视为旧明文（兼容迁移）
func DecryptSecret(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if len(stored) < 7 || stored[:7] != "enc:v1:" {
		return stored, nil // 旧版本明文，直接返回
	}
	raw, err := base64.StdEncoding.DecodeString(stored[7:])
	if err != nil {
		return "", fmt.Errorf("解码密文失败: %w", err)
	}
	plain, err := decryptWithDPAPI(raw)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
