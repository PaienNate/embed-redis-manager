package goredisv8

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	"embedRedisClient/redisui/redisclient"
)

func newTestAdapter(t *testing.T) (redisclient.Client, *miniredis.Miniredis) {
	t.Helper()
	m := miniredis.RunT(t)
	return New(redis.NewClient(&redis.Options{Addr: m.Addr()})), m
}

func TestDoAndNil(t *testing.T) {
	c, _ := newTestAdapter(t)
	ctx := context.Background()

	if err := c.Do(ctx, "SET", "k", "v").Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	got := c.Do(ctx, "GET", "k")
	if got.Err() != nil || got.Val() != "v" {
		t.Fatalf("GET = %v, %v", got.Val(), got.Err())
	}

	missing := c.Do(ctx, "GET", "missing")
	if !errors.Is(missing.Err(), redisclient.ErrNil) {
		t.Fatalf("missing Err = %v, want ErrNil", missing.Err())
	}
	if missing.Val() != nil {
		t.Fatalf("missing Val = %v, want nil", missing.Val())
	}
}

func TestKindHostPortAndWithDB(t *testing.T) {
	c, _ := newTestAdapter(t)
	ctx := context.Background()

	if c.Kind() != redisclient.KindSingle {
		t.Fatalf("Kind = %v, want KindSingle", c.Kind())
	}
	host, port := c.HostPort()
	if host == "" || port == "" {
		t.Fatalf("HostPort = %q, %q", host, port)
	}

	db1, err := c.WithDB(1)
	if err != nil {
		t.Fatalf("WithDB(1): %v", err)
	}
	if err := db1.Do(ctx, "SET", "k", "v1").Err(); err != nil {
		t.Fatalf("db1 SET: %v", err)
	}
	if err := c.Do(ctx, "GET", "k").Err(); !errors.Is(err, redisclient.ErrNil) {
		t.Fatalf("db0 saw db1's key: %v", err)
	}
	if got := db1.Do(ctx, "GET", "k"); got.Err() != nil || got.Val() != "v1" {
		t.Fatalf("db1 GET = %v, %v", got.Val(), got.Err())
	}
}

func TestClusterNodesNonCluster(t *testing.T) {
	c, _ := newTestAdapter(t)
	raw, err := c.ClusterNodes(context.Background())
	if err != nil || raw != "" {
		t.Fatalf("ClusterNodes = %q, %v; want empty, nil", raw, err)
	}
}

func TestSubscribe(t *testing.T) {
	c, m := newTestAdapter(t)
	sub := c.Subscribe(context.Background(), "ch")
	defer sub.Close()

	// Publish until the server has registered the subscription.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.Publish("ch", "hello") > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	select {
	case msg := <-sub.Messages():
		if msg.Channel != "ch" || msg.Payload != "hello" {
			t.Fatalf("message = %+v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a message")
	}
}

type wrapped struct {
	redis.UniversalClient
}

func TestMonitorUnsupportedForNonSingle(t *testing.T) {
	m := miniredis.RunT(t)
	// The wrapper hides the concrete *redis.Client, standing in for a
	// cluster/ring/custom client.
	c := New(wrapped{UniversalClient: redis.NewClient(&redis.Options{Addr: m.Addr()})})
	if _, err := c.Monitor(context.Background()); !errors.Is(err, redisclient.ErrMonitorUnsupported) {
		t.Fatalf("Monitor Err = %v, want ErrMonitorUnsupported", err)
	}
}

// TestRawMonitorProtocol exercises the RESP handshake (AUTH/SELECT/MONITOR)
// and line parsing against a fake server, without requiring a live Redis.
func TestRawMonitorProtocol(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	commands := make(chan []string, 3)
	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		bw := bufio.NewWriter(conn)
		for i := 0; i < 3; i++ {
			cmd, err := readRESPCommand(br)
			if err != nil {
				serverErr <- err
				return
			}
			commands <- cmd
			if _, err := bw.WriteString("+OK\r\n"); err != nil {
				serverErr <- err
				return
			}
			if err := bw.Flush(); err != nil {
				serverErr <- err
				return
			}
		}
		if _, err := bw.WriteString("+1700000000.000000 [2 127.0.0.1:1234] \"set\" \"k\" \"v\"\r\n"); err != nil {
			serverErr <- err
			return
		}
		if err := bw.Flush(); err != nil {
			serverErr <- err
			return
		}
		_, _ = io.Copy(io.Discard, br)
	}()

	// NewClient runs Options.init, so Dialer/timeouts match production.
	rc := redis.NewClient(&redis.Options{
		Addr:     ln.Addr().String(),
		Password: "pass",
		DB:       2,
	})
	defer rc.Close()

	mon, err := New(rc).Monitor(context.Background())
	if err != nil {
		t.Fatalf("Monitor: %v", err)
	}
	defer mon.Stop()

	want := [][]string{{"AUTH", "pass"}, {"SELECT", "2"}, {"MONITOR"}}
	for i, w := range want {
		select {
		case got := <-commands:
			if !reflect.DeepEqual(got, w) {
				t.Fatalf("command[%d] = %v, want %v", i, got, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for command[%d]", i)
		}
	}

	select {
	case line := <-mon.Lines():
		if !strings.Contains(line, `"set" "k" "v"`) {
			t.Fatalf("line = %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a monitor line")
	}

	select {
	case err := <-serverErr:
		t.Fatalf("fake server: %v", err)
	default:
	}
}

// TestMonitorStreamsCommands is the live-Redis counterpart, skipped when no
// integration Redis is available (same convention as redisui/stream_test.go).
func TestMonitorStreamsCommands(t *testing.T) {
	addr := os.Getenv("REDISUI_TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6389"
	}
	probe := redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 500 * time.Millisecond})
	defer probe.Close()
	if err := probe.Ping(context.Background()).Err(); err != nil {
		t.Skipf("integration Redis not reachable at %s: %v", addr, err)
	}

	mon, err := New(redis.NewClient(&redis.Options{Addr: addr})).Monitor(context.Background())
	if err != nil {
		t.Fatalf("Monitor: %v", err)
	}
	defer mon.Stop()

	time.Sleep(150 * time.Millisecond)
	writer := redis.NewClient(&redis.Options{Addr: addr})
	defer writer.Close()
	if err := writer.Set(context.Background(), "mon:v8:key", "v8", 0).Err(); err != nil {
		t.Fatalf("writer set: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case line, ok := <-mon.Lines():
			if !ok {
				t.Fatal("monitor stream closed")
			}
			if strings.Contains(line, `"set" "mon:v8:key" "v8"`) {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for a MONITOR line")
		}
	}
}

func readRESPCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("array header: %q", line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		sizeLine, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		sizeLine = strings.TrimSuffix(strings.TrimSuffix(sizeLine, "\n"), "\r")
		if !strings.HasPrefix(sizeLine, "$") {
			return nil, fmt.Errorf("bulk header: %q", sizeLine)
		}
		size, err := strconv.Atoi(sizeLine[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		if _, err := r.Discard(2); err != nil {
			return nil, err
		}
		args = append(args, string(buf))
	}
	return args, nil
}
