package secrets

import (
	"encoding/hex"
	"os"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	// 配置密钥
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	os.Setenv(encKeyEnv, hex.EncodeToString(key))
	defer os.Unsetenv(encKeyEnv)

	token := "glpat-xxxx-private-token"
	enc := Encrypt(token)
	if enc == token || len(enc) == 0 {
		t.Fatalf("配置密钥后应加密存储，got: %q", enc)
	}
	dec := Decrypt(enc)
	if dec != token {
		t.Fatalf("解密结果不一致: got %q want %q", dec, token)
	}
	// 空值
	if Encrypt("") != "" {
		t.Fatal("空令牌应返回空串")
	}
}

func TestPlaintextFallback(t *testing.T) {
	os.Unsetenv(encKeyEnv)
	token := "plain-token"
	if Encrypt(token) != token {
		t.Fatal("未配置密钥时应明文降级")
	}
	if Decrypt(token) != token {
		t.Fatal("未配置密钥时解密应原样返回")
	}
	if Decrypt("enc:nothex") != "enc:nothex" {
		t.Fatal("非法密文应原样返回")
	}
}
