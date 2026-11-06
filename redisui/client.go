package redisui

import "embedRedisClient/redisui/redisclient"

// Type aliases keep the public surface on redisui while the leaf package
// (redisclient) remains importable by version adapters without a cycle.
type (
	// Client is the minimal Redis surface the UI needs.
	Client = redisclient.Client
	// Command is one Redis command reply.
	Command = redisclient.Command
	// Subscription is a live PUBLISH/PSUBSCRIBE feed.
	Subscription = redisclient.Subscription
	// Message is one pub/sub message.
	Message = redisclient.Message
	// Monitor is a live MONITOR feed.
	Monitor = redisclient.Monitor
	// Kind describes the topology of an injected Redis client.
	Kind = redisclient.Kind
)

const (
	// KindSingle is a standalone Redis client: one server, selectable db.
	KindSingle = redisclient.KindSingle
	// KindCluster is a Redis Cluster client: a single db, no SELECT.
	KindCluster = redisclient.KindCluster
	// KindRing is a client-side sharded ring.
	KindRing = redisclient.KindRing
	// KindOther is any other UniversalClient implementation.
	KindOther = redisclient.KindOther
)

var (
	// ErrNil reports a Redis nil reply (for example GET of a missing key).
	ErrNil = redisclient.ErrNil
	// ErrDBUnsupported is returned when a client cannot scope a connection to
	// one database (cluster, ring, custom clients).
	ErrDBUnsupported = redisclient.ErrDBUnsupported
	// ErrMonitorUnsupported is returned when MONITOR is not available for the
	// injected client.
	ErrMonitorUnsupported = redisclient.ErrMonitorUnsupported
)
