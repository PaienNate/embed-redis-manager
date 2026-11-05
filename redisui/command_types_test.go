package redisui

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// The frontend HttpRedisClient adapts Go's reply shapes into the structures
// the value-editing components expect. These tests lock down the reply shapes
// the generic endpoint must preserve (raw redis reply via Do), in particular
// the *SCAN variants and ZRANGE WITHSCORES that the view/edit UI relies on.
// For the unordered *SCAN variants we assert shape (cursor + flat items)
// rather than element order, which is server-dependent.

func decodeArray(t *testing.T, raw json.RawMessage) []json.RawMessage {
	t.Helper()
	var arr struct {
		T string            `json:"t"`
		V []json.RawMessage `json:"v"`
	}
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("unmarshal reply: %v; raw=%s", err, raw)
	}
	if arr.T != "a" {
		t.Fatalf("reply tag = %q, want \"a\"; raw=%s", arr.T, raw)
	}
	return arr.V
}

func decodeTag(t *testing.T, raw json.RawMessage) (tag string, val string) {
	t.Helper()
	var el struct {
		T string `json:"t"`
		V string `json:"v"`
	}
	if err := json.Unmarshal(raw, &el); err != nil {
		t.Fatalf("unmarshal element: %v; raw=%s", err, raw)
	}
	return el.T, el.V
}

func decodeBin(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	tag, b64 := decodeTag(t, raw)
	if tag != "b" {
		t.Fatalf("element tag = %q, want \"b\"; raw=%s", tag, raw)
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("bad base64: %v", err)
	}
	return string(data)
}

func TestCommandHashScanShape(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	doCommand(t, mux, "c1", `{"cmd":"hset","args":["h","f1","v1","f2","v2"]}`)
	rec := doCommand(t, mux, "c1", `{"cmd":"hscan","args":["h","0","MATCH","*","COUNT","100"],"binary":true}`)

	var env struct {
		Reply json.RawMessage `json:"reply"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	outer := decodeArray(t, env.Reply)
	if len(outer) != 2 {
		t.Fatalf("hscan reply should be [cursor, items], got %d elems", len(outer))
	}
	if cur := decodeBin(t, outer[0]); cur != "0" {
		t.Fatalf("hscan cursor = %q, want \"0\"", cur)
	}
	items := decodeArray(t, outer[1])
	if len(items) != 4 {
		t.Fatalf("hscan items len = %d, want 4", len(items))
	}
	// alternating field/value binary pairs
	want := map[string]string{"f1": "v1", "f2": "v2"}
	got := map[string]string{}
	for i := 0; i < len(items); i += 2 {
		got[decodeBin(t, items[i])] = decodeBin(t, items[i+1])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hscan fields = %v, want %v", got, want)
	}
}

func TestCommandSetScanShape(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	doCommand(t, mux, "c1", `{"cmd":"sadd","args":["s","m1","m2"]}`)
	rec := doCommand(t, mux, "c1", `{"cmd":"sscan","args":["s","0","MATCH","*","COUNT","100"],"binary":true}`)

	var env struct {
		Reply json.RawMessage `json:"reply"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	outer := decodeArray(t, env.Reply)
	if len(outer) != 2 {
		t.Fatalf("sscan reply should be [cursor, items], got %d elems", len(outer))
	}
	if cur := decodeBin(t, outer[0]); cur != "0" {
		t.Fatalf("sscan cursor = %q, want \"0\"", cur)
	}
	items := decodeArray(t, outer[1])
	members := make([]string, 0, len(items))
	for _, it := range items {
		members = append(members, decodeBin(t, it))
	}
	sort.Strings(members)
	if want := []string{"m1", "m2"}; !reflect.DeepEqual(members, want) {
		t.Fatalf("sscan members = %v, want %v", members, want)
	}
}

func TestCommandZrangeWithscoresShape(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	doCommand(t, mux, "c1", `{"cmd":"zadd","args":["z","1.5","z1","2.5","z2"]}`)
	// Members are bulk strings ({t:"b"} in binary mode) but scores are exposed
	// by go-redis as a non-bulk type (mapped to {t:"s"} by the default branch),
	// so WITHSCORES yields a mixed array the frontend adapts in solveList.
	rec := doCommand(t, mux, "c1", `{"cmd":"zrange","args":["z","0","-1","WITHSCORES"],"binary":true}`)
	assertReply(t, rec,
		`{"t":"a","v":[`+
			`{"t":"b","v":"`+base64.StdEncoding.EncodeToString([]byte("z1"))+`"},`+
			`{"t":"s","v":"1.5"},`+
			`{"t":"b","v":"`+base64.StdEncoding.EncodeToString([]byte("z2"))+`"},`+
			`{"t":"s","v":"2.5"}]}`)
}

func TestCommandListRangeShape(t *testing.T) {
	c := miniredisClient(t)
	mux := commandServer(t, Connection{ID: "c1", Name: "c1", Client: c, Enabled: true})

	doCommand(t, mux, "c1", `{"cmd":"rpush","args":["l","a","b"]}`)
	rec := doCommand(t, mux, "c1", `{"cmd":"lrange","args":["l","0","-1"],"binary":true}`)
	// each list element is a base64 bulk string
	assertReply(t, rec,
		`{"t":"a","v":[`+
			`{"t":"b","v":"`+base64.StdEncoding.EncodeToString([]byte("a"))+`"},`+
			`{"t":"b","v":"`+base64.StdEncoding.EncodeToString([]byte("b"))+`"}]}`)
}
