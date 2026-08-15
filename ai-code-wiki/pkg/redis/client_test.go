package redis

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// newPipeClient 构建一个通过 net.Pipe 通信的 Client（test 端为 c2）。
func newPipeClient(t *testing.T) (*Client, net.Conn) {
	t.Helper()
	c1, c2 := net.Pipe()
	c := &Client{conn: c1, br: bufio.NewReader(c1), timeout: time.Second}
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })
	return c, c2
}

// writeServer 在 test 端直接写入回复（用于纯解码测试）。
func writeServer(t *testing.T, c2 net.Conn, reply string) {
	t.Helper()
	go func() {
		_, _ = io.WriteString(c2, reply)
	}()
}

// echoServer 在 test 端读取命令后写入回复，并返回收到的命令（用于 Do 往返测试）。
func echoServer(t *testing.T, c2 net.Conn, reply string) <-chan string {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := c2.Read(buf)
		got <- string(buf[:n])
		_, _ = io.WriteString(c2, reply)
	}()
	return got
}

func TestDecodeSimpleString(t *testing.T) {
	c, c2 := newPipeClient(t)
	writeServer(t, c2, "+OK\r\n")
	v, err := c.readValueLocked()
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if v != "OK" {
		t.Errorf("应为 OK，实际 %v", v)
	}
}

func TestDecodeIntegerAndBulk(t *testing.T) {
	c, c2 := newPipeClient(t)
	writeServer(t, c2, ":5\r\n$3\r\nfoo\r\n")

	v, err := c.readValueLocked()
	if err != nil || v.(int64) != 5 {
		t.Errorf("整数解码失败: v=%v err=%v", v, err)
	}
	v, err = c.readValueLocked()
	if err != nil || v.(string) != "foo" {
		t.Errorf("批量字符串解码失败: v=%v err=%v", v, err)
	}
}

func TestDecodeNullBulkAndArray(t *testing.T) {
	c, c2 := newPipeClient(t)
	writeServer(t, c2, "$-1\r\n*2\r\n:1\r\n$2\r\nhi\r\n")

	v, err := c.readValueLocked()
	if err != nil || v != nil {
		t.Errorf("null bulk 应为 nil: v=%v err=%v", v, err)
	}
	v, err = c.readValueLocked()
	if err != nil {
		t.Fatalf("数组解码失败: %v", err)
	}
	arr := v.([]any)
	if len(arr) != 2 || arr[0].(int64) != 1 || arr[1].(string) != "hi" {
		t.Errorf("数组解码异常: %v", arr)
	}
}

func TestDecodeError(t *testing.T) {
	c, c2 := newPipeClient(t)
	writeServer(t, c2, "-ERR boom\r\n")

	v, err := c.readValueLocked()
	if err != nil {
		t.Fatalf("readValueLocked 不应直接返回 err: %v", err)
	}
	e, ok := v.(error)
	if !ok || !strings.Contains(e.Error(), "boom") {
		t.Errorf("错误回复应以 error 值返回: v=%v", v)
	}
}

func TestDoRoundTrip(t *testing.T) {
	c, c2 := newPipeClient(t)
	got := echoServer(t, c2, ":1\r\n")

	v, err := c.Do(context.Background(), "EXISTS", "k")
	if err != nil || v.(int64) != 1 {
		t.Errorf("Do 往返失败: v=%v err=%v", v, err)
	}
	select {
	case cmd := <-got:
		want := "*2\r\n$6\r\nEXISTS\r\n$1\r\nk\r\n"
		if cmd != want {
			t.Errorf("命令编码异常:\n实际: %q\n期望: %q", cmd, want)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到命令")
	}
}

func TestDoWithArgsContainingSpacesAndCRLF(t *testing.T) {
	c, c2 := newPipeClient(t)
	got := echoServer(t, c2, "+OK\r\n")

	_, err := c.Do(context.Background(), "SET", "k", "a b\r\nc")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	select {
	case cmd := <-got:
		if !strings.Contains(cmd, "$6\r\na b\r\nc\r\n") {
			t.Errorf("含特殊字符参数编码异常: %q", cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到命令")
	}
}
