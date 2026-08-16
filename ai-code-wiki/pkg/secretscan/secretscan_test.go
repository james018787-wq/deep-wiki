package secretscan

import "testing"

// TestScan 验证常见敏感信息可被正确检出（类型/风险/脱敏/行号）。
func TestScan(t *testing.T) {
	content := "package main\n" +
		"\n" +
		"func main() {\n" +
		"\tapiKey := \"sk-abcdefghijklmnop123456\"\n" +
		"\tpassword := \"root1234\"\n" +
		"\tconfig := map[string]string{\"password\": \"P@ssw0rd-demo\"}\n" +
		"\tsk := \"sk-proj-fakefakefakefakefake123\"\n" +
		"}\n" +
		"// ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789\n" +
		"const key = `-----BEGIN RSA PRIVATE KEY-----`\n"

	findings := Scan(content, "secret.go")
	if len(findings) < 6 {
		t.Fatalf("期望检出 6 类敏感信息, got %d: %+v", len(findings), findings)
	}
	byType := map[string][]*Finding{}
	for _, f := range findings {
		byType[f.Type] = append(byType[f.Type], f)
	}
	if len(byType["openai_key"]) < 2 {
		t.Fatalf("openai_key 应检出 2 处（含 sk-proj-），got %d", len(byType["openai_key"]))
	}
	if len(byType["password"]) < 2 {
		t.Fatalf("password 应检出 2 处（含带引号键名），got %d", len(byType["password"]))
	}
	if len(byType["github_token"]) != 1 {
		t.Fatalf("github_token 应检出 1 处")
	}
	if len(byType["private_key"]) != 1 {
		t.Fatalf("private_key 应检出 1 处")
	}
	// 脱敏断言：不出现完整密钥
	for _, f := range findings {
		if len(f.Secret) > 20 {
			t.Fatalf("脱敏失效: %s", f.Secret)
		}
	}
}

// TestScanNoFalsePositive 验证常规赋值文本不误报。
func TestScanNoFalsePositive(t *testing.T) {
	content := "const name = \"admin\"\n" +
		"username := \"james\"\n" +
		"return nil\n"
	if findings := Scan(content, "ok.go"); len(findings) != 0 {
		t.Fatalf("不应误报: %+v", findings)
	}
}

// TestMask 验证脱敏规则。
func TestMask(t *testing.T) {
	if got := Mask("sk-abcdefghijklmnop123456"); got == "sk-abcdefghijklmnop123456" || len(got) > 16 {
		t.Fatalf("Mask 失效: %s", got)
	}
	if Mask("short") != "****" {
		t.Fatal("短串应整体脱敏")
	}
}
