package redisui

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	v8 "github.com/go-redis/redis/v8"
	"github.com/redis/go-redis/v9"

	"embedRedisClient/redisui/goredisv8"
	"embedRedisClient/redisui/goredisv9"
)

func optionsMux(t *testing.T, conn Connection) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	if err := Register(mux, Config{Prefix: "/r", Connections: []Connection{conn}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return mux
}

func getOptions(t *testing.T, mux *http.ServeMux, connID string) []Option {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r/api/redis/"+connID+"/options", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET options status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var opts []Option
	if err := json.Unmarshal(rec.Body.Bytes(), &opts); err != nil {
		t.Fatalf("unmarshal options: %v; body=%s", err, rec.Body.String())
	}
	return opts
}

func optionValues(opts []Option) map[string]string {
	m := make(map[string]string, len(opts))
	for _, o := range opts {
		m[o.Key] = o.Value
	}
	return m
}

func TestConnectionOptionsV9(t *testing.T) {
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

	mux := optionsMux(t, Connection{ID: "c1", Name: "c1", Client: goredisv9.New(raw), Enabled: true})
	opts := getOptions(t, mux, "c1")
	got := optionValues(opts)

	want := map[string]string{
		"Kind":                     "Single",
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
	if strings.Contains(fmt.Sprint(opts), "secret") {
		t.Fatalf("options leaked the password: %+v", opts)
	}
}

func TestConnectionOptionsV8(t *testing.T) {
	m := miniredis.RunT(t)
	raw := v8.NewClient(&v8.Options{
		Addr:     m.Addr(),
		Username: "bob",
		Password: "secret8",
		DB:       2,
		PoolSize: 5,
	})
	defer raw.Close()

	mux := optionsMux(t, Connection{ID: "c1", Name: "c1", Client: goredisv8.New(raw), Enabled: true})
	opts := getOptions(t, mux, "c1")
	got := optionValues(opts)

	want := map[string]string{
		"Kind":         "Single",
		"Address":      m.Addr(),
		"DB":           "2",
		"Username":     "bob",
		"Password Set": "yes",
		"Pool Size":    "5",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["Protocol"]; ok {
		t.Fatalf("v8 options must not expose Protocol: %q", got["Protocol"])
	}
	if strings.Contains(fmt.Sprint(opts), "secret8") {
		t.Fatalf("options leaked the password: %+v", opts)
	}
}

// TestConnectionOptionsFallbackWithoutProvider covers custom Client
// implementations that do not implement OptionsProvider: the endpoint still
// reports Kind and a best-effort Address.
func TestConnectionOptionsFallbackWithoutProvider(t *testing.T) {
	mux := optionsMux(t, Connection{ID: "c1", Name: "c1", Client: miniredisClient(t), Enabled: true})
	got := optionValues(getOptions(t, mux, "c1"))
	if got["Kind"] != "Single" {
		t.Errorf("Kind = %q, want Single", got["Kind"])
	}
	if got["Address"] == "" {
		t.Errorf("Address is empty; want a HostPort fallback")
	}
}

func TestConnectionOptionsClusterKind(t *testing.T) {
	cc := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs: []string{"127.0.0.1:7000", "127.0.0.1:7001"},
	})
	defer cc.Close()

	mux := optionsMux(t, Connection{ID: "c1", Name: "c1", Client: goredisv9.New(cc), Enabled: true})
	got := optionValues(getOptions(t, mux, "c1"))
	if got["Kind"] != "Cluster" {
		t.Errorf("Kind = %q, want Cluster", got["Kind"])
	}
	if !strings.Contains(got["Addresses"], "127.0.0.1:7000") {
		t.Errorf("Addresses = %q, want both cluster nodes", got["Addresses"])
	}
	if got["Address"] != "" {
		t.Errorf("cluster options should use Addresses, got Address=%q", got["Address"])
	}
}
