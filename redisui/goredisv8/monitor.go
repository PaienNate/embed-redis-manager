package goredisv8

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"

	"embedRedisClient/redisui/redisclient"
)

// startRawMonitor implements MONITOR over a dedicated RESP2 connection.
//
// go-redis v8 has no Monitor command, but the Redis protocol is independent of
// the client library. The connection settings (Dialer/TLS/Username/Password/
// DB) are taken from the injected v8 client's Options, so auth, TLS and unix
// sockets keep working. MONITOR is only meaningful for a single-node client;
// the caller rejects cluster/ring clients.
func startRawMonitor(ctx context.Context, opt *redis.Options) (redisclient.Monitor, error) {
	network := opt.Network
	if network == "" {
		network = "tcp"
	}
	dialer := opt.Dialer
	if dialer == nil {
		dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := &net.Dialer{Timeout: opt.DialTimeout, KeepAlive: 5 * time.Minute}
			if opt.TLSConfig == nil {
				return d.DialContext(ctx, network, addr)
			}
			return tls.DialWithDialer(d, network, addr, opt.TLSConfig)
		}
	}

	conn, err := dialer(ctx, network, opt.Addr)
	if err != nil {
		return nil, fmt.Errorf("redisui: MONITOR dial: %w", err)
	}
	fail := func(err error) (redisclient.Monitor, error) {
		_ = conn.Close()
		return nil, err
	}

	// Bound the AUTH/SELECT/MONITOR handshake; the stream itself must be idle.
	handshake := opt.DialTimeout
	if handshake <= 0 {
		handshake = 5 * time.Second
	}
	_ = conn.SetDeadline(time.Now().Add(handshake))

	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)

	if opt.Password != "" {
		args := []string{"AUTH", opt.Password}
		if opt.Username != "" {
			args = []string{"AUTH", opt.Username, opt.Password}
		}
		if err := writeCommand(bw, args...); err != nil {
			return fail(fmt.Errorf("redisui: MONITOR AUTH: %w", err))
		}
		if err := readStatus(br); err != nil {
			return fail(fmt.Errorf("redisui: MONITOR AUTH: %w", err))
		}
	}
	if opt.DB > 0 {
		if err := writeCommand(bw, "SELECT", strconv.Itoa(opt.DB)); err != nil {
			return fail(fmt.Errorf("redisui: MONITOR SELECT: %w", err))
		}
		if err := readStatus(br); err != nil {
			return fail(fmt.Errorf("redisui: MONITOR SELECT: %w", err))
		}
	}
	if err := writeCommand(bw, "MONITOR"); err != nil {
		return fail(fmt.Errorf("redisui: MONITOR: %w", err))
	}
	if err := readStatus(br); err != nil {
		return fail(fmt.Errorf("redisui: MONITOR: %w", err))
	}

	// MONITOR is expected to stay idle: drop the handshake deadline.
	_ = conn.SetDeadline(time.Time{})

	m := &rawMonitor{
		conn:  conn,
		lines: make(chan string, 256),
		done:  make(chan struct{}),
	}
	go m.readLoop(br)
	return m, nil
}

type rawMonitor struct {
	conn  net.Conn
	lines chan string
	done  chan struct{}
	once  sync.Once
}

func (m *rawMonitor) Lines() <-chan string { return m.lines }

func (m *rawMonitor) Stop() {
	m.once.Do(func() {
		close(m.done)
		_ = m.conn.Close()
	})
}

func (m *rawMonitor) readLoop(br *bufio.Reader) {
	defer close(m.lines)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case '+':
			line = line[1:]
		case '-':
			// A server error ends the feed; the UI observes the closed stream.
			return
		default:
			// MONITOR emits simple strings; ignore anything else.
			continue
		}
		select {
		case m.lines <- line:
		case <-m.done:
			return
		}
	}
}

// writeCommand writes one RESP array. Arguments are written verbatim with an
// explicit byte length, so passwords or monitor output containing CRLF are
// still encoded correctly.
func writeCommand(w *bufio.Writer, args ...string) error {
	if _, err := fmt.Fprintf(w, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, a := range args {
		if _, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(a), a); err != nil {
			return err
		}
	}
	return w.Flush()
}

// readStatus reads a simple-string reply (+OK) or returns the server error.
func readStatus(r *bufio.Reader) error {
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if strings.HasPrefix(line, "-") {
		return errors.New(strings.TrimPrefix(line, "-"))
	}
	if !strings.HasPrefix(line, "+") {
		return fmt.Errorf("unexpected reply %q", line)
	}
	return nil
}
