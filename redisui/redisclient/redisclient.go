// Package redisclient defines the minimal Redis client surface consumed by
// redisui. It is a leaf package (it imports no other redisui package) so that
// version adapters can implement it without importing redisui, which keeps
// same-package tests free of import cycles.
package redisclient

import (
	"context"
	"errors"
)

// Kind describes the topology of an injected Redis client.
type Kind int

const (
	// KindSingle is a standalone Redis client: one server, selectable db.
	KindSingle Kind = iota
	// KindCluster is a Redis Cluster client: a single db, no SELECT.
	KindCluster
	// KindRing is a client-side sharded ring.
	KindRing
	// KindOther is any other UniversalClient implementation (custom wrappers,
	// sentinel-derived clients, ...).
	KindOther
)

var (
	// ErrNil reports a Redis nil reply (for example GET of a missing key).
	// Adapters translate their go-redis nil sentinel into this value so the
	// core never needs to import go-redis.
	ErrNil = errors.New("redis: nil")

	// ErrDBUnsupported is returned by Client.WithDB when the client cannot
	// scope a connection to one database (cluster, ring, custom clients).
	ErrDBUnsupported = errors.New("redisui: this client type cannot scope a db to one connection")

	// ErrMonitorUnsupported is returned by Client.Monitor when MONITOR is not
	// available for the injected client.
	ErrMonitorUnsupported = errors.New("redisui: MONITOR is not supported for this client")
)

// Client is the minimal Redis surface the UI needs. Hosts wrap their
// already-instantiated go-redis client with redisui/goredisv9.New or
// redisui/goredisv8.New, or implement Client directly for another library.
type Client interface {
	// Do executes an arbitrary command and returns its reply. It is the only
	// path the generic command endpoint uses, so no typed go-redis command
	// belongs in this interface.
	Do(ctx context.Context, args ...interface{}) Command

	// Subscribe starts a PUBLISH subscription.
	Subscribe(ctx context.Context, channels ...string) Subscription
	// PSubscribe starts a pattern subscription.
	PSubscribe(ctx context.Context, patterns ...string) Subscription

	// Kind reports the client topology.
	Kind() Kind
	// HostPort returns the address shown in the UI (best effort; empty for
	// clients whose address cannot be determined).
	HostPort() (host, port string)
	// WithDB returns a client whose connection pool is pinned to db. It must
	// return ErrDBUnsupported for cluster/ring/custom clients.
	WithDB(db int) (Client, error)
	// ClusterNodes returns the raw CLUSTER NODES reply. Non-cluster clients
	// must return ("", nil) so the UI treats them as "not a cluster".
	ClusterNodes(ctx context.Context) (string, error)
	// Monitor starts a MONITOR feed. It must return ErrMonitorUnsupported when
	// the adapter cannot provide one.
	Monitor(ctx context.Context) (Monitor, error)
}

// Command is one Redis command reply.
type Command interface {
	Err() error
	Val() interface{}
}

// Subscription is a live PUBLISH/PSUBSCRIBE feed.
type Subscription interface {
	Messages() <-chan Message
	Close() error
}

// Message is one pub/sub message.
type Message struct {
	Channel string
	Pattern string
	Payload string
}

// Monitor is a live MONITOR feed.
type Monitor interface {
	Lines() <-chan string
	Stop()
}

// NewCommand builds a Command from an already-read reply. Adapters use it to
// translate their go-redis nil sentinel into ErrNil.
func NewCommand(val interface{}, err error) Command {
	return command{val: val, err: err}
}

type command struct {
	val interface{}
	err error
}

func (c command) Err() error       { return c.err }
func (c command) Val() interface{} { return c.val }
