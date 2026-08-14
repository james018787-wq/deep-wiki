// Package astphp PHP 源码简易解析。
// MVP 实现：基于正则 + 花括号配平提取具名函数，不引入重型 PHP 解析库，
// 不做深度 AST 语法分析。
package astphp

import (
	"regexp"
	"strings"
)

// FuncItem PHP 函数的最小解析切片。
type FuncItem struct {
	FuncName string // 函数名称
	Code     string // 函数源码片段（函数声明 + 完整函数体）
}

// funcRe 匹配 PHP 函数声明：function [&] 函数名 (
// 仅匹配具名函数；匿名函数（function (...) {...} 无函数名）不会命中。
var funcRe = regexp.MustCompile(`\bfunction\s+&?\s*([A-Za-z_\x80-\xff][A-Za-z0-9_\x80-\xff]*)\s*\(`)

// ParsePHPFile 解析 PHP 源码字符串，提取全部具名函数。
//
// MVP 简易实现步骤：
//  1. 预扫描出全部注释（//、#、/* */）与字符串区域，规避注释/字符串内的
//     function 关键字造成误匹配；
//  2. 用正则定位函数声明（function 关键字 + 函数名 + 左括号），
//     跳过落在注释或字符串区域内的命中；
//  3. 从声明后查找函数体起始 '{'（跳过字符串/注释），
//     以花括号配平截取完整函数源码片段；
//     无函数体（抽象/接口方法）则跳过。
//
// 已知边界（MVP 可接受）：
//  - 属性声明中字符串值内出现完整函数声明文本仍可能误匹配（极少见）；
//  - 不包含可见性修饰符（public/private 等），片段从 function 关键字开始。
func ParsePHPFile(content string) ([]FuncItem, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	inactive := scanInactive(content)
	items := make([]FuncItem, 0, 8)

	for _, m := range funcRe.FindAllStringSubmatchIndex(content, -1) {
		declStart := m[0] // function 关键字位置
		parenEnd := m[1]  // 左括号之后，用于查找函数体
		name := content[m[2]:m[3]]

		// 规避注释内函数：function 关键字落在注释/字符串区域则跳过
		if inRanges(inactive, declStart) {
			continue
		}

		closeBrace := findBodyEnd(content, parenEnd)
		if closeBrace < 0 {
			continue // 无函数体（抽象/接口方法），跳过
		}

		items = append(items, FuncItem{
			FuncName: name,
			Code:     content[declStart : closeBrace+1],
		})
	}
	return items, nil
}

// scanInactive 扫描源码，返回全部"非活动"字节区间（字符串字面量 + 注释）。
// 用于判断某个 position 是否位于注释或字符串内部。
func scanInactive(s string) [][2]int {
	var ranges [][2]int
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\'' || s[i] == '"':
			end := skipString(s, i)
			ranges = append(ranges, [2]int{i, end})
			i = end
		case strings.HasPrefix(s[i:], "//") || strings.HasPrefix(s[i:], "#"):
			end := i
			for end < len(s) && s[end] != '\n' {
				end++
			}
			ranges = append(ranges, [2]int{i, end})
			i = end
		case strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				end = len(s)
			} else {
				end = i + 2 + end + 2
			}
			ranges = append(ranges, [2]int{i, end})
			i = end
		default:
			i++
		}
	}
	return ranges
}

// skipString 跳过从 i 开始的字符串字面量（单引号/双引号），返回结束位置。
// 支持反斜杠转义。
func skipString(s string, i int) int {
	quote := s[i]
	i++
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == quote {
			return i + 1
		}
		i++
	}
	return len(s)
}

// inRanges 判断 position 是否落在任一区间内。
func inRanges(ranges [][2]int, pos int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

// findBodyEnd 从 from 位置向后查找函数体起始 '{'（跳过字符串/注释），
// 再以花括号配平返回对应 '}' 的下标；无函数体返回 -1。
func findBodyEnd(s string, from int) int {
	bodyStart := -1
	i := from
	for i < len(s) {
		switch {
		case s[i] == '\'' || s[i] == '"':
			i = skipString(s, i)
		case strings.HasPrefix(s[i:], "//") || strings.HasPrefix(s[i:], "#"):
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case strings.HasPrefix(s[i:], "/*"):
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				i = len(s)
			} else {
				i = i + 2 + j + 2
			}
		case s[i] == '{':
			bodyStart = i
			i = len(s) // 跳出循环进入配平阶段
		default:
			i++
		}
	}
	if bodyStart < 0 {
		return -1
	}

	depth := 0
	for k := bodyStart; k < len(s); k++ {
		switch {
		case s[k] == '\'' || s[k] == '"':
			k = skipString(s, k) - 1
		case strings.HasPrefix(s[k:], "//") || strings.HasPrefix(s[k:], "#"):
			for k < len(s) && s[k] != '\n' {
				k++
			}
			k--
		case strings.HasPrefix(s[k:], "/*"):
			j := strings.Index(s[k+2:], "*/")
			if j < 0 {
				k = len(s)
			} else {
				k = k + 2 + j + 2 - 1
			}
		case s[k] == '{':
			depth++
		case s[k] == '}':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return -1
}