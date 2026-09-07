package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// 密码存储安全增强：
//  - 加密密钥不写死在代码里，从环境变量 SEAT_SECRET 或独立密钥文件 secret.key 读取
//  - 使用 AES-256-GCM（带认证标签），相比旧的固定密钥 AES-CBC 更安全

// ensureSecretKey 获取存储密钥（32字节）。优先环境变量 SEAT_SECRET，
// 其次读取 dataDir/secret.key，都没有则生成并写入 secret.key（0600）。
func ensureSecretKey(dataDir string) ([]byte, error) {
	if env := os.Getenv("SEAT_SECRET"); env != "" {
		sum := sha256.Sum256([]byte(env))
		return sum[:], nil
	}
	keyFile := filepath.Join(dataDir, "secret.key")
	if b, err := os.ReadFile(keyFile); err == nil && len(b) >= 16 {
		sum := sha256.Sum256(b)
		return sum[:], nil
	}
	// 生成新密钥并落盘
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyFile, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// encryptSecret AES-256-GCM 加密，返回  base64(nonce || ciphertext)。
func encryptSecret(key, plain []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, plain, nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// decryptSecret 解密 encryptSecret 的输出。
func decryptSecret(key []byte, b64 string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("密文长度不足")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}
