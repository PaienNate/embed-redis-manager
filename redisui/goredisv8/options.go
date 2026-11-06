package goredisv8

import (
	"crypto/tls"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"

	"embedRedisClient/redisui/redisclient"
)

// Options implements redisclient.OptionsProvider: it describes the injected
// client's configuration for the UI card. Only safe values are emitted — never
// the password or TLS certificate material.
func (c *client) Options() []redisclient.Option {
	switch v := c.rc.(type) {
	case *redis.Client:
		return singleOptions(v.Options())
	case *redis.ClusterClient:
		return clusterOptions(v.Options())
	case *redis.Ring:
		return ringOptions(v.Options())
	default:
		return nil
	}
}

func singleOptions(o *redis.Options) []redisclient.Option {
	opts := []redisclient.Option{
		{Key: "Address", Value: o.Addr},
		{Key: "Network", Value: optDash(o.Network)},
		{Key: "DB", Value: strconv.Itoa(o.DB)},
	}
	opts = optAuth(opts, o.Username, o.Password)
	opts = optTLS(opts, o.TLSConfig)
	opts = optPool(opts, o.PoolFIFO, o.PoolSize, o.MinIdleConns, o.MaxConnAge, o.PoolTimeout, o.IdleTimeout, o.IdleCheckFrequency, false)
	opts = optTimeouts(opts, o.DialTimeout, o.ReadTimeout, o.WriteTimeout, o.MaxRetries, o.MinRetryBackoff, o.MaxRetryBackoff)
	opts = append(opts,
		redisclient.Option{Key: "OnConnect", Value: optYesNo(o.OnConnect != nil)},
		redisclient.Option{Key: "Limiter", Value: optYesNo(o.Limiter != nil)},
	)
	return opts
}

func clusterOptions(o *redis.ClusterOptions) []redisclient.Option {
	opts := []redisclient.Option{
		{Key: "Addresses", Value: strings.Join(o.Addrs, ", ")},
		{Key: "Max Redirects", Value: strconv.Itoa(o.MaxRedirects)},
		{Key: "Read Only", Value: optYesNo(o.ReadOnly)},
		{Key: "Route By Latency", Value: optYesNo(o.RouteByLatency)},
		{Key: "Route Randomly", Value: optYesNo(o.RouteRandomly)},
		{Key: "New Client", Value: optYesNo(o.NewClient != nil)},
	}
	opts = optAuth(opts, o.Username, o.Password)
	opts = optTLS(opts, o.TLSConfig)
	opts = optPool(opts, o.PoolFIFO, o.PoolSize, o.MinIdleConns, o.MaxConnAge, o.PoolTimeout, o.IdleTimeout, o.IdleCheckFrequency, true)
	opts = optTimeouts(opts, o.DialTimeout, o.ReadTimeout, o.WriteTimeout, o.MaxRetries, o.MinRetryBackoff, o.MaxRetryBackoff)
	opts = append(opts, redisclient.Option{Key: "OnConnect", Value: optYesNo(o.OnConnect != nil)})
	return opts
}

func ringOptions(o *redis.RingOptions) []redisclient.Option {
	opts := []redisclient.Option{
		{Key: "Addresses", Value: strings.Join(sortedAddrs(o.Addrs), ", ")},
		{Key: "Shard Count", Value: strconv.Itoa(len(o.Addrs))},
		{Key: "DB", Value: strconv.Itoa(o.DB)},
		{Key: "Heartbeat Frequency", Value: optDuration(o.HeartbeatFrequency)},
		{Key: "New Client", Value: optYesNo(o.NewClient != nil)},
	}
	opts = optAuth(opts, o.Username, o.Password)
	opts = optTLS(opts, o.TLSConfig)
	opts = optPool(opts, o.PoolFIFO, o.PoolSize, o.MinIdleConns, o.MaxConnAge, o.PoolTimeout, o.IdleTimeout, o.IdleCheckFrequency, false)
	opts = optTimeouts(opts, o.DialTimeout, o.ReadTimeout, o.WriteTimeout, o.MaxRetries, o.MinRetryBackoff, o.MaxRetryBackoff)
	opts = append(opts,
		redisclient.Option{Key: "OnConnect", Value: optYesNo(o.OnConnect != nil)},
		redisclient.Option{Key: "Limiter", Value: optYesNo(o.Limiter != nil)},
	)
	return opts
}

func optYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func optDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func optDuration(d time.Duration) string {
	switch {
	case d < 0:
		return "disabled"
	case d == 0:
		return "no timeout"
	default:
		return d.String()
	}
}

func optIdle(d time.Duration) string {
	switch {
	case d < 0:
		return "disabled"
	case d == 0:
		return "no limit"
	default:
		return d.String()
	}
}

func optDelay(d time.Duration) string {
	if d <= 0 {
		return "none"
	}
	return d.String()
}

func optAuth(opts []redisclient.Option, username, password string) []redisclient.Option {
	if username != "" {
		opts = append(opts, redisclient.Option{Key: "Username", Value: username})
	}
	opts = append(opts, redisclient.Option{Key: "Password Set", Value: optYesNo(password != "")})
	return opts
}

func optTLS(opts []redisclient.Option, cfg *tls.Config) []redisclient.Option {
	if cfg == nil {
		return append(opts, redisclient.Option{Key: "TLS", Value: "off"})
	}
	opts = append(opts, redisclient.Option{Key: "TLS", Value: "on"})
	if cfg.ServerName != "" {
		opts = append(opts, redisclient.Option{Key: "TLS Server Name", Value: cfg.ServerName})
	}
	opts = append(opts, redisclient.Option{Key: "TLS Insecure Skip Verify", Value: optYesNo(cfg.InsecureSkipVerify)})
	if cfg.RootCAs != nil {
		opts = append(opts, redisclient.Option{Key: "TLS Root CAs", Value: "custom"})
	} else {
		opts = append(opts, redisclient.Option{Key: "TLS Root CAs", Value: "system"})
	}
	if n := len(cfg.Certificates); n > 0 {
		opts = append(opts, redisclient.Option{Key: "TLS Client Certificates", Value: strconv.Itoa(n)})
	}
	if cfg.MinVersion != 0 {
		opts = append(opts, redisclient.Option{Key: "TLS Min Version", Value: tls.VersionName(cfg.MinVersion)})
	}
	if cfg.MaxVersion != 0 {
		opts = append(opts, redisclient.Option{Key: "TLS Max Version", Value: tls.VersionName(cfg.MaxVersion)})
	}
	if len(cfg.NextProtos) > 0 {
		opts = append(opts, redisclient.Option{Key: "TLS Next Protos", Value: strings.Join(cfg.NextProtos, ", ")})
	}
	return opts
}

func optPool(opts []redisclient.Option, fifo bool, size, minIdle int, maxConnAge, poolTimeout, idleTimeout, idleCheckFrequency time.Duration, perNode bool) []redisclient.Option {
	sizeKey := "Pool Size"
	if perNode {
		sizeKey = "Pool Size (per node)"
	}
	return append(opts,
		redisclient.Option{Key: sizeKey, Value: strconv.Itoa(size)},
		redisclient.Option{Key: "Pool FIFO", Value: optYesNo(fifo)},
		redisclient.Option{Key: "Min Idle Conns", Value: strconv.Itoa(minIdle)},
		redisclient.Option{Key: "Max Conn Age", Value: optIdle(maxConnAge)},
		redisclient.Option{Key: "Pool Timeout", Value: optDuration(poolTimeout)},
		redisclient.Option{Key: "Idle Timeout", Value: optIdle(idleTimeout)},
		redisclient.Option{Key: "Idle Check Frequency", Value: optDuration(idleCheckFrequency)},
	)
}

func optTimeouts(opts []redisclient.Option, dial, read, write time.Duration, retries int, minBackoff, maxBackoff time.Duration) []redisclient.Option {
	return append(opts,
		redisclient.Option{Key: "Dial Timeout", Value: optDuration(dial)},
		redisclient.Option{Key: "Read Timeout", Value: optDuration(read)},
		redisclient.Option{Key: "Write Timeout", Value: optDuration(write)},
		redisclient.Option{Key: "Max Retries", Value: strconv.Itoa(retries)},
		redisclient.Option{Key: "Min Retry Backoff", Value: optDelay(minBackoff)},
		redisclient.Option{Key: "Max Retry Backoff", Value: optDelay(maxBackoff)},
	)
}

func sortedAddrs(m map[string]string) []string {
	addrs := make([]string, 0, len(m))
	for _, a := range m {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	return addrs
}
