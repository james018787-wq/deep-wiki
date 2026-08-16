// Package secrets 提供仓库访问令牌等敏感字段的对称加解密。
// 密钥来自环境变量 REPO_TOKEN_KEY（hex 编码 32 字节）；未配置时降级为明文存储
// （仅用于本地开发，生产必须配置），加密值带 "enc:" 前缀便于识别。
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
)

const encKeyEnv = "REPO_TOKEN_KEY"
const encPrefix = "enc:"

// loadKey 从环境变量加载 32 字节 AES 密钥；缺失或非法返回 nil（调用方降级明文）。
func loadKey() []byte {
	raw := os.Getenv(encKeyEnv)
	if raw == "" {
		return nil
	}
	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil
	}
	return key
}

// Encrypt AES-GCM 加密明文；未配置密钥时原样返回（明文降级）。
func Encrypt(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	key := loadKey()
	if key == nil {
		return plaintext
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return plaintext
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return plaintext
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return plaintext
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + hex.EncodeToString(sealed)
}

// Decrypt 解密 enc: 前缀密文；无前缀（明文存储）或解密失败时原样返回。
func Decrypt(cipherText string) string {
	if !strings.HasPrefix(cipherText, encPrefix) {
		return cipherText
	}
	key := loadKey()
	if key == nil {
		return cipherText
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(cipherText, encPrefix))
	if err != nil {
		return cipherText
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return cipherText
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return cipherText
	}
	if len(raw) < gcm.NonceSize() {
		return cipherText
	}
	nonce, data := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return cipherText
	}
	return string(plain)
}
