package redisui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// The cluster/nodes endpoint cannot be exercised end-to-end here because a
// real *redis.ClusterClient requires a live cluster to talk to (go-redis
// resolves nodes on first command). These tests cover the parts we can pin
// down offline: parsing the CLUSTER NODES text into node structs, and the
// endpoint's behavior for a non-cluster connection (empty node list).

func TestParseClusterNodes(t *testing.T) {
	raw := `e7a4f4f3f8e7c3d2a1b0c9d8e7f6a5b4c3d2e1f0 10.0.0.1:6379@16379 myself,master - 0 0 1 connected 0-5460
9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0 10.0.0.2:6379@16379 slave e7a4f4f3f8e7c3d2a1b0c9d8e7f6a5b4c3d2e1f0 0 0 1 connected 5461
`
	nodes := parseClusterNodes(raw)
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2; nodes=%+v", len(nodes), nodes)
	}

	first := nodes[0]
	if first.ID != "e7a4f4f3f8e7c3d2a1b0c9d8e7f6a5b4c3d2e1f0" {
		t.Errorf("first.ID = %q", first.ID)
	}
	if first.Host != "10.0.0.1" || first.Port != "6379" || first.Addr != "10.0.0.1:6379" {
		t.Errorf("first addr = %s:%s / %s", first.Host, first.Port, first.Addr)
	}
	if first.Role != "master" {
		t.Errorf("first.Role = %q, want master", first.Role)
	}
	if first.LinkState != "connected" {
		t.Errorf("first.LinkState = %q", first.LinkState)
	}
	wantSlots := []string{"0-5460"}
	if !reflect.DeepEqual(first.Slots, wantSlots) {
		t.Errorf("first.Slots = %v, want %v", first.Slots, wantSlots)
	}

	second := nodes[1]
	if second.Role != "slave" {
		t.Errorf("second.Role = %q, want slave", second.Role)
	}
	if second.Master != "e7a4f4f3f8e7c3d2a1b0c9d8e7f6a5b4c3d2e1f0" {
		t.Errorf("second.Master = %q", second.Master)
	}
}

func TestParseClusterNodesSkipsMalformedLines(t *testing.T) {
	raw := "only a few tokens\n\n10.0.0.1:6379@16379 myself,master - 0 0 1 connected 0-100\n"
	nodes := parseClusterNodes(raw)
	if len(nodes) != 1 {
		t.Fatalf("len(nodes) = %d, want 1 (malformed and blank lines skipped)", len(nodes))
	}
}

func TestClusterNodesNonClusterReturnsEmpty(t *testing.T) {
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: miniredisClient(t), Enabled: true})
	req := httptest.NewRequest(http.MethodGet, "/r/api/redis/c1/cluster/nodes", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp clusterNodesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("expected nil error, got %q", *resp.Error)
	}
	if resp.Nodes == nil || len(resp.Nodes) != 0 {
		t.Fatalf("nodes = %+v, want empty non-nil slice", resp.Nodes)
	}
}

func TestClusterNodesBadConnIDReturns404(t *testing.T) {
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: miniredisClient(t), Enabled: true})
	req := httptest.NewRequest(http.MethodGet, "/r/api/redis/nope/cluster/nodes", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
