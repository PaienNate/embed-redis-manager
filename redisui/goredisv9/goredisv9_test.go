package goredisv9

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

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

func TestMonitorUnsupportedForNonSingle(t *testing.T) {
	m := miniredis.RunT(t)
	// The wrapper hides the concrete *redis.Client, standing in for a
	// cluster/ring/custom client.
	c := New(wrapped{UniversalClient: redis.NewClient(&redis.Options{Addr: m.Addr()})})
	if _, err := c.Monitor(context.Background()); !errors.Is(err, redisclient.ErrMonitorUnsupported) {
		t.Fatalf("Monitor Err = %v, want ErrMonitorUnsupported", err)
	}
}

func TestOptions(t *testing.T) {
	m := miniredis.RunT(t)
	raw := redis.NewClient(&redis.Options{
		Addr:      m.Addr(),
		Username:  "alice",
		Password:  "secret",
		DB:        3,
		PoolSize:  7,
		Protocol:  2,
		TLSConfig: &tls.Config{ServerName: "redis.example", InsecureSkipVerify: true},
	})
	defer raw.Close()

	opts := New(raw).(redisclient.OptionsProvider).Options()
	got := make(map[string]string, len(opts))
	for _, o := range opts {
		got[o.Key] = o.Value
		if strings.Contains(o.Value, "secret") {
			t.Fatalf("option %q leaked the password: %q", o.Key, o.Value)
		}
	}

	want := map[string]string{
		"Address":                  m.Addr(),
		"DB":                       "3",
		"Username":                 "alice",
		"Password Set":             "yes",
		"Protocol":                 "2",
		"Pool Size":                "7",
		"TLS":                      "on",
		"TLS Server Name":          "redis.example",
		"TLS Insecure Skip Verify": "yes",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestClusterAndRingOptions(t *testing.T) {
	cc := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:        []string{"127.0.0.1:7000", "127.0.0.1:7001"},
		MaxRedirects: 5,
	})
	defer cc.Close()
	got := optionMap(New(cc).(redisclient.OptionsProvider).Options())
	if got["Addresses"] != "127.0.0.1:7000, 127.0.0.1:7001" {
		t.Errorf("cluster Addresses = %q", got["Addresses"])
	}
	if got["Max Redirects"] != "5" {
		t.Errorf("Max Redirects = %q, want 5", got["Max Redirects"])
	}
	if got["Pool Size (per node)"] == "" {
		t.Error("cluster options missing Pool Size (per node)")
	}

	ring := redis.NewRing(&redis.RingOptions{
		Addrs: map[string]string{"b": "127.0.0.1:7001", "a": "127.0.0.1:7000"},
	})
	defer ring.Close()
	got = optionMap(New(ring).(redisclient.OptionsProvider).Options())
	if got["Addresses"] != "127.0.0.1:7000, 127.0.0.1:7001" {
		t.Errorf("ring Addresses = %q (want sorted)", got["Addresses"])
	}
	if got["Heartbeat Frequency"] == "" {
		t.Error("ring options missing Heartbeat Frequency")
	}
}

func optionMap(opts []redisclient.Option) map[string]string {
	m := make(map[string]string, len(opts))
	for _, o := range opts {
		m[o.Key] = o.Value
	}
	return m
}

type wrapped struct {
	redis.UniversalClient
}
