// Package goredisv8 adapts github.com/go-redis/redis/v8 clients to the
// redisui.Client interface.
//
// go-redis v8 has no Monitor command, so Client.Monitor is implemented over a
// dedicated RESP2 connection using the injected client's Options.
package goredisv8

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/go-redis/redis/v8"

	"embedRedisClient/redisui/redisclient"
)

// New wraps an already-instantiated go-redis v8 client. The host keeps
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
	return startRawMonitor(ctx, rc.Options())
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
