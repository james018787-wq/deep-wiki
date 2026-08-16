package service

import "testing"

// TestParseAnalyzeJSON 验证 LLM 输出各种容错解析场景。
func TestParseAnalyzeJSON(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
		wantMod string
	}{
		{
			name:    "纯净 JSON",
			raw:     `{"related_modules":["order"],"related_functions":[],"analysis":"a","risk_points":["r"],"suggestion":"s"}`,
			wantMod: "order",
		},
		{
			name:    "markdown 代码块包裹",
			raw:     "```json\n{\"related_modules\":[\"order\"],\"analysis\":\"a\"}\n```",
			wantMod: "order",
		},
		{
			name:    "前后多余解释文本",
			raw:     "好的，分析如下：\n{\"related_modules\":[\"user\"],\"analysis\":\"a\"}\n以上就是结果。",
			wantMod: "user",
		},
		{
			name:    "字符串内含花括号",
			raw:     `{"related_modules":["order"],"analysis":"代码示例 { func() } 正常"}`,
			wantMod: "order",
		},
		{
			name:    "非 JSON",
			raw:     "抱歉，我无法回答。",
			wantErr: true,
		},
		{
			name:    "空输出",
			raw:     "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := parseAnalyzeJSON(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("应解析失败, got %+v", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v (raw=%q)", err, tc.raw)
			}
			if len(out.RelatedModules) == 0 || out.RelatedModules[0] != tc.wantMod {
				t.Fatalf("模块不匹配: got %v want %s", out.RelatedModules, tc.wantMod)
			}
		})
	}
}
