package common

import "strconv"

// Str2Int64 字符串转 int64。
// 解析失败或为空时返回 0，用于 handler 层路径/查询参数转换。
func Str2Int64(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}