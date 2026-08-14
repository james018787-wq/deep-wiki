// Package logger 项目统一日志工具。
// 支持 Info / Warn / Error 三级日志，默认输出到控制台，
// 预留文件输出扩展点（Sink 接口 + NewFileSink + SetOutput）。
// Error 自动打印堆栈；日志可携带 request_id（经 context 传递）。
package logger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// contextKey request_id 在 context 中的键。
type contextKey string

const requestIDKey contextKey = "request_id"

// WithRequestID 将 request_id 注入 context，供日志打印使用。
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFrom 从 context 中取出 request_id，未注入时返回空串。
func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// Sink 日志输出目标接口（扩展点：可扩展文件、远程等输出）。
type Sink interface {
	io.Writer
	Close() error
}

// consoleSink 控制台输出（默认目标）。
type consoleSink struct {
	w io.Writer
}

func (s *consoleSink) Write(p []byte) (int, error) { return s.w.Write(p) }
func (s *consoleSink) Close() error                { return nil }

// fileSink 文件输出目标。
type fileSink struct {
	f *os.File
}

func (s *fileSink) Write(p []byte) (int, error) { return s.f.Write(p) }
func (s *fileSink) Close() error                { return s.f.Close() }

var (
	mu     sync.RWMutex
	output Sink = &consoleSink{w: os.Stdout}
	logger      = log.New(output, "", log.LstdFlags|log.Lmicroseconds)
)

// NewFileSink 构建文件输出目标（扩展点，默认未启用）。
// 启用方式：sink, err := logger.NewFileSink("/var/log/app.log"); logger.SetOutput(sink)
func NewFileSink(path string) (Sink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &fileSink{f: f}, nil
}

// SetOutput 切换日志输出目标（预留扩展点：文件 / 远程等）。
func SetOutput(s Sink) {
	mu.Lock()
	defer mu.Unlock()
	output = s
	logger.SetOutput(s)
}

// Info 输出 INFO 级日志。
func Info(ctx context.Context, format string, args ...any) {
	logMsg(ctx, "INFO", format, args...)
}

// Warn 输出 WARN 级日志。
func Warn(ctx context.Context, format string, args ...any) {
	logMsg(ctx, "WARN", format, args...)
}

// Error 输出 ERROR 级日志，并附带调用堆栈。
func Error(ctx context.Context, format string, args ...any) {
	logMsg(ctx, "ERROR", format, args...)
}

// logMsg 组装并输出日志行。
// 格式：[级别] [request_id=xxx] 消息
// ERROR 级别追加 goroutine 调用堆栈（含具体出错位置）。
func logMsg(ctx context.Context, level, format string, args ...any) {
	var sb strings.Builder
	sb.WriteString("[")
	sb.WriteString(level)
	sb.WriteString("] ")
	if rid := RequestIDFrom(ctx); rid != "" {
		sb.WriteString("[request_id=")
		sb.WriteString(rid)
		sb.WriteString("] ")
	}
	sb.WriteString(fmt.Sprintf(format, args...))
	if level == "ERROR" {
		sb.WriteString("\n")
		sb.Write(debug.Stack())
	}

	mu.RLock()
	l := logger
	mu.RUnlock()
	l.Println(sb.String())
}

// GenerateRequestID 生成请求追踪 id（crypto/rand，16 位十六进制）。
// 供 gin 中间件生成 request_id 使用。
func GenerateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}