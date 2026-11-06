// Package goredisv9 adapts github.com/redis/go-redis/v9 clients to the
// redisui.Client interface.
package goredisv9

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"

	"embedRedisClient/redisui/redisclient"
)

// New wraps an already-instantiated go-redis v9 client. The host keeps
// ownership of c: the adapter never closes it.
func New(c redis.UniversalClient) redisclient.Client {
	return &client{rc: c}
}

type client struct {
	rc redis.UniversalClient
}

var _ redisclient.Client = (*client)(nil)

func (c *client) Do(ctx context.Context, args ...interface{}) redisclient.Command {
	cmd := c.rc.Do(ctx, args...)
	err := cmd.Err()
	if errors.Is(err, redis.Nil) {
		err = redisclient.ErrNil
	}
	return redisclient.NewCommand(cmd.Val(), err)
}

func (c *client) Kind() redisclient.Kind {
	switch c.rc.(type) {
	case *redis.Client:
		return redisclient.KindSingle
	case *redis.ClusterClient:
		return redisclient.KindCluster
	case *redis.Ring:
		return redisclient.KindRing
	default:
		return redisclient.KindOther
	}
}

func (c *client) HostPort() (string, string) {
	var addr string
	switch v := c.rc.(type) {
	case *redis.Client:
		addr = v.Options().Addr
	case *redis.ClusterClient:
		if addrs := v.Options().Addrs; len(addrs) > 0 {
			addr = addrs[0]
		}
	case *redis.Ring:
		// Map iteration is random; choose deterministically.
		for _, a := range v.Options().Addrs {
			if addr == "" || a < addr {
				addr = a
			}
		}
	}
	return splitHostPort(addr)
}

func splitHostPort(addr string) (host, port string) {
	if addr == "" {
		return "", ""
	}
	if h, p, err := net.SplitHostPort(addr); err == nil {
		return h, p
	}
	return addr, ""
}

func (c *client) WithDB(db int) (redisclient.Client, error) {
	rc, ok := c.rc.(*redis.Client)
	if !ok {
		return nil, redisclient.ErrDBUnsupported
	}
	opts := *rc.Options() // shallow copy; NewClient deep-clones it
	opts.DB = db
	// Use a fresh disabled config rather than the host's pointer: the shallow
	// copy would share the host's live *Config (raced by its initConn), and
	// nil would default to ModeAuto, which registers a global handler for
	// every derived client.
	opts.MaintNotificationsConfig = &maintnotifications.Config{Mode: maintnotifications.ModeDisabled}
	return &client{rc: redis.NewClient(&opts)}, nil
}

func (c *client) ClusterNodes(ctx context.Context) (string, error) {
	if c.Kind() != redisclient.KindCluster {
		return "", nil
	}
	return c.rc.ClusterNodes(ctx).Result()
}

func (c *client) Subscribe(ctx context.Context, channels ...string) redisclient.Subscription {
	return newSubscription(c.rc.Subscribe(ctx, channels...))
}

func (c *client) PSubscribe(ctx context.Context, patterns ...string) redisclient.Subscription {
	return newSubscription(c.rc.PSubscribe(ctx, patterns...))
}

func (c *client) Monitor(ctx context.Context) (redisclient.Monitor, error) {
	rc, ok := c.rc.(*redis.Client)
	if !ok {
		return nil, redisclient.ErrMonitorUnsupported
	}

	// MONITOR hijacks its connection, so use a dedicated client and a sticky
	// connection: the poisoned connection is returned to the dedicated pool
	// (never the host's pool) and closed when the monitor stops.
	opts := *rc.Options()
	opts.PoolSize = 1
	opts.MinIdleConns = 0
	opts.MaxIdleConns = 1
	opts.MaintNotificationsConfig = &maintnotifications.Config{Mode: maintnotifications.ModeDisabled}
	opts.Protocol = 2 // MONITOR emits plain RESP2 simple strings
	// MONITOR is expected to stay idle: never impose a socket read deadline.
	opts.ReadTimeout = -2
	opts.WriteTimeout = -2

	mon := redis.NewClient(&opts)
	conn := mon.Conn()
	mctx, cancel := context.WithCancel(ctx)
	ch := make(chan string, 256)
	mn := conn.Monitor(mctx, ch)
	mn.Start()
	return &monitor{cli: mon, cancel: cancel, ch: ch}, nil
}

type monitor struct {
	cli    *redis.Client
	cancel context.CancelFunc
	ch     chan string
	once   sync.Once
}

var _ redisclient.Monitor = (*monitor)(nil)

func (m *monitor) Lines() <-chan string { return m.ch }

func (m *monitor) Stop() {
	m.once.Do(func() {
		m.cancel()
		// Close the dedicated pool directly. ConnPool.Close closes the
		// underlying socket (which unblocks readMonitor's pending Peek)
		// without putting the connection back into a pool. Calling
		// conn.Close() instead would run pool.putConn -> HasBufferedData on
		// the same bufio.Reader that readMonitor is still filling, which is a
		// data race (and the host pool must never see this connection anyway).
		_ = m.cli.Close()
	})
}

// subscription pumps one go-redis PubSub into redisclient.Message values.
type subscription struct {
	ps   *redis.PubSub
	out  chan redisclient.Message
	done chan struct{}
	once sync.Once
}

var _ redisclient.Subscription = (*subscription)(nil)

func newSubscription(ps *redis.PubSub) *subscription {
	s := &subscription{
		ps:   ps,
		out:  make(chan redisclient.Message, 256),
		done: make(chan struct{}),
	}
	go func() {
		defer close(s.out)
		src := ps.Channel()
		for {
			select {
			case <-s.done:
				return
			case m, ok := <-src:
				if !ok {
					return
				}
				select {
				case s.out <- redisclient.Message{Channel: m.Channel, Pattern: m.Pattern, Payload: m.Payload}:
				case <-s.done:
					return
				}
			}
		}
	}()
	return s
}

func (s *subscription) Messages() <-chan redisclient.Message { return s.out }

func (s *subscription) Close() error {
	s.once.Do(func() {
		close(s.done)
		_ = s.ps.Close()
	})
	return nil
}
