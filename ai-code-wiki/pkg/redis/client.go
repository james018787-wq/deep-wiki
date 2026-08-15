// Package redis 轻量 Redis 客户端（标准库实现，RESP2 协议）。
//
// 背景：构建环境无外网，无法引入 go-redis 依赖；本包以 net 标准库实现
// 本项目所需的少量命令（PING/SET/GET/DEL/EXISTS/INCR/EVAL）。
//
// 设计：
//   - 单连接串行复用（互斥锁保护），出错自动断开，下次调用自动重连；
//   - 支持 context 超时（读写 deadline 取自 ctx，未设超时用默认值）；
//   - 调用方按 fail-open 策略处理 Redis 故障（熔断/限流降级，不阻断业务）。
//
// 说明：连接无池化，满足当前限流/熔断低频调用场景；若后续需要高并发，
// 建议在有网络的环境替换为 github.com/redis/go-redis/v9。
package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout 单次命令默认超时。
const DefaultTimeout = 2 * time.Second

// ErrNil 命令返回 nil（如 GET 不存在的 key）。
var ErrNil = errors.New("redis: nil")

// Client 轻量 Redis 客户端。
type Client struct {
	addr     string
	password string
	db       int
	timeout  time.Duration

	mu   sync.Mutex
	conn net.Conn
	br   *bufio.Reader
}

// NewClient 构建 Redis 客户端。addr 为空时默认 127.0.0.1:6379。
func NewClient(addr, password string, db int) *Client {
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:6379"
	}
	return &Client{addr: addr, password: password, db: db, timeout: DefaultTimeout}
}

// Ping 连通性探测。
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Do(ctx, "PING")
	return err
}

// Close 关闭底层连接。
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropLocked()
	return nil
}

// Do 执行一条 Redis 命令（参数为字符串形式）。
// 返回值为解码后的 RESP 回复：string / int64 / []any（嵌套）/ nil；错误回复返回 error。
func (c *Client) Do(ctx context.Context, args ...string) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(args) == 0 {
		return nil, fmt.Errorf("redis: 命令为空")
	}
	if err := c.ensureConnLocked(ctx); err != nil {
		return nil, err
	}
	if err := c.writeCommandLocked(ctx, args); err != nil {
		c.dropLocked()
		return nil, err
	}
	reply, err := c.readReplyLocked(ctx)
	if err != nil {
		c.dropLocked()
		return nil, err
	}
	if e, ok := reply.(error); ok {
		return nil, e
	}
	return reply, nil
}

// Eval 执行 Lua 脚本。keys/args 分别对应脚本 KEYS[]/ARGV[]。
func (c *Client) Eval(ctx context.Context, script string, keys, args []string) (any, error) {
	cmd := make([]string, 0, 3+len(keys)+len(args))
	cmd = append(cmd, "EVAL", script, strconv.Itoa(len(keys)))
	cmd = append(cmd, keys...)
	cmd = append(cmd, args...)
	return c.Do(ctx, cmd...)
}

// ensureConnLocked 建立（或复用）连接，并完成 AUTH/SELECT 握手。
func (c *Client) ensureConnLocked(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return fmt.Errorf("redis: 连接 %s 失败: %w", c.addr, err)
	}
	c.conn = conn
	c.br = bufio.NewReader(conn)

	// 握手：AUTH -> SELECT
	if c.password != "" {
		if err := c.writeCommandLocked(ctx, []string{"AUTH", c.password}); err != nil {
			c.dropLocked()
			return err
		}
		if _, err := c.readReplyLocked(ctx); err != nil {
			c.dropLocked()
			return fmt.Errorf("redis: AUTH 失败: %w", err)
		}
	}
	if c.db > 0 {
		if err := c.writeCommandLocked(ctx, []string{"SELECT", strconv.Itoa(c.db)}); err != nil {
			c.dropLocked()
			return err
		}
		if _, err := c.readReplyLocked(ctx); err != nil {
			c.dropLocked()
			return fmt.Errorf("redis: SELECT 失败: %w", err)
		}
	}
	return nil
}

// writeCommandLocked 以 RESP 数组形式写入命令。
func (c *Client) writeCommandLocked(ctx context.Context, args []string) error {
	var sb strings.Builder
	sb.WriteString("*" + strconv.Itoa(len(args)) + "\r\n")
	for _, a := range args {
		sb.WriteString("$" + strconv.Itoa(len(a)) + "\r\n" + a + "\r\n")
	}
	if err := c.conn.SetWriteDeadline(deadline(ctx, c.timeout)); err != nil {
		return err
	}
	if _, err := c.conn.Write([]byte(sb.String())); err != nil {
		return fmt.Errorf("redis: 写入失败: %w", err)
	}
	return nil
}

// readReplyLocked 读取并解码一条 RESP 回复。
func (c *Client) readReplyLocked(ctx context.Context) (any, error) {
	if err := c.conn.SetReadDeadline(deadline(ctx, c.timeout)); err != nil {
		return nil, err
	}
	return c.readValueLocked()
}

// readValueLocked 递归解码 RESP 值。
func (c *Client) readValueLocked() (any, error) {
	line, err := readLine(c.br)
	if err != nil {
		return nil, err
	}
	if len(line) == 0 {
		return nil, fmt.Errorf("redis: 空回复")
	}
	switch line[0] {
	case '+': // 简单字符串
		return line[1:], nil
	case '-': // 错误
		return fmt.Errorf("redis: %s", line[1:]), nil
	case ':': // 整数
		n, err := strconv.ParseInt(line[1:], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("redis: 整数解析失败 %q", line)
		}
		return n, nil
	case '$': // 批量字符串
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, fmt.Errorf("redis: 长度解析失败 %q", line)
		}
		if n < 0 {
			return nil, nil // null bulk
		}
		data, err := readN(c.br, n+2) // 数据 + \r\n
		if err != nil {
			return nil, err
		}
		return string(data[:n]), nil
	case '*': // 数组
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, fmt.Errorf("redis: 数组长度解析失败 %q", line)
		}
		if n < 0 {
			return nil, nil // null array
		}
		arr := make([]any, 0, n)
		for i := 0; i < n; i++ {
			v, err := c.readValueLocked()
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("redis: 未知回复类型 %q", line)
	}
}

// readLine 读取一行（含 \r\n），返回去除 \r\n 的内容。
func readLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("redis: 读取失败: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readN 精确读取 n 字节。
func readN(br *bufio.Reader, n int) ([]byte, error) {
	data := make([]byte, n)
	if _, err := readFull(br, data); err != nil {
		return nil, err
	}
	return data, nil
}

// readFull 读取完整填充 buf。
func readFull(br *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := br.Read(buf[total:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// deadline 计算读写 deadline：优先 ctx 截止时间，否则用默认超时。
func deadline(ctx context.Context, timeout time.Duration) time.Time {
	if dl, ok := ctx.Deadline(); ok {
		return dl
	}
	return time.Now().Add(timeout)
}

// dropLocked 关闭并丢弃当前连接（调用方需持有锁）。
func (c *Client) dropLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.br = nil
	}
}
