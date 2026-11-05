package redisui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"
)

// clusterNode is one node from a cluster connection's CLUSTER NODES output.
// CLUSTER NODES returns one line per node of the form:
//
//	<id> <ip:port@cport> <flags> <master> <ping-sent> <pong-recv>
//	<config-epoch> <link-state> <slot> <slot> ...
//
// We expose the fields the frontend renders plus the parsed host/port/role so
// the UI can display a node table without re-parsing raw text.
type clusterNode struct {
	ID        string   `json:"id"`
	Addr      string   `json:"addr"` // "<host>:<port>" (cluster-bus "@cport" stripped)
	Host      string   `json:"host"`
	Port      string   `json:"port"`
	Flags     string   `json:"flags"`  // comma-separated node flags
	Master    string   `json:"master"` // master node id (for replicas)
	LinkState string   `json:"linkState"`
	Role      string   `json:"role"`  // "master" | "slave" | "unknown"
	Slots     []string `json:"slots"` // slot ranges, e.g. ["0-5460", "5461"]
}

// clusterNodesResponse is the GET .../cluster/nodes body. Error is null on
// success; Nodes is an empty array for a non-cluster connection.
type clusterNodesResponse struct {
	Error *string       `json:"error"`
	Nodes []clusterNode `json:"nodes"`
}

// handleClusterNodes serves GET {prefix}/api/redis/{connID}/cluster/nodes. For
// a *redis.ClusterClient it returns the parsed CLUSTER NODES list; any other
// client type (single node, ring, sentinel) returns an empty node list rather
// than an error, so the frontend can treat "no nodes" as "not a cluster".
func (s *server) handleClusterNodes(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.conns[r.PathValue("connID")]
	if !ok || !conn.Enabled {
		http.Error(w, "redisui: connection not found", http.StatusNotFound)
		return
	}

	cluster, ok := conn.Client.(*redis.ClusterClient)
	if !ok {
		writeClusterNodes(w, nil)
		return
	}

	raw, err := cluster.ClusterNodes(r.Context()).Result()
	if err != nil {
		msg := err.Error()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(clusterNodesResponse{Error: &msg})
		return
	}

	writeClusterNodes(w, parseClusterNodes(raw))
}

func writeClusterNodes(w http.ResponseWriter, nodes []clusterNode) {
	if nodes == nil {
		nodes = []clusterNode{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(clusterNodesResponse{Nodes: nodes})
}

// parseClusterNodes parses the multiline CLUSTER NODES text into node structs.
func parseClusterNodes(raw string) []clusterNode {
	nodes := []clusterNode{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// A node has at least 8 whitespace-separated fields before the slot list.
		if len(fields) < 8 {
			continue
		}

		host, port := splitClusterAddr(fields[1])
		flags := fields[2]
		role := clusterNodeRole(flags)
		slots := fields[8:]

		nodes = append(nodes, clusterNode{
			ID:        fields[0],
			Addr:      host + ":" + port,
			Host:      host,
			Port:      port,
			Flags:     flags,
			Master:    fields[3],
			LinkState: fields[7],
			Role:      role,
			Slots:     slots,
		})
	}
	return nodes
}

// splitClusterAddr turns "<host>:<port>@<cport>" into host and port, dropping
// the cluster-bus "@<cport>" suffix (which is not useful to display).
func splitClusterAddr(addr string) (host, port string) {
	if i := strings.IndexByte(addr, '@'); i >= 0 {
		addr = addr[:i]
	}
	h, p, _ := strings.Cut(addr, ":")
	return h, p
}

func clusterNodeRole(flags string) string {
	if strings.Contains(flags, "slave") || strings.Contains(flags, "replica") {
		return "slave"
	}
	if strings.Contains(flags, "master") {
		return "master"
	}
	return "unknown"
}
