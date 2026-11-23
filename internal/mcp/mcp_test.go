package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rxsuite/opengit/internal/server"
)

func rpc(t *testing.T, s *Server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var rs []map[string]any
	dec := json.NewDecoder(&out)
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		rs = append(rs, m)
	}
	return rs
}

func text(t *testing.T, r map[string]any) (string, bool) {
	t.Helper()
	res := r["result"].(map[string]any)
	c := res["content"].([]any)[0].(map[string]any)["text"].(string)
	e, _ := res["isError"].(bool)
	return c, e
}

func TestMCP(t *testing.T) {
	srv, err := server.New(server.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, _ := http.Post(ts.URL+"/api/register", "application/json", strings.NewReader(`{"username":"alice","password":"password-alice"}`))
	var reg struct{ Token string }
	json.NewDecoder(resp.Body).Decode(&reg)

	s := &Server{BaseURL: ts.URL, Token: reg.Token, Version: "test"}
	rs := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_repo","arguments":{"name":"demo","description":"d"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"nope"}`,
		`not json`)
	if len(rs) != 5 { // notification gets no reply
		t.Fatalf("got %d responses", len(rs))
	}
	if rs[0]["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "opengit" {
		t.Fatal("bad initialize")
	}
	if n := len(rs[1]["result"].(map[string]any)["tools"].([]any)); n != 6 {
		t.Fatalf("tools=%d", n)
	}
	if c, e := text(t, rs[2]); e || !strings.Contains(c, `"demo"`) {
		t.Fatalf("create_repo: %v %s", e, c)
	}
	if rs[3]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatal("unknown method should error")
	}

	// push content, then read it back through the tools
	work := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = work
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(work, "hi.txt"), []byte("hello mcp\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "init")
	run("push", strings.Replace(ts.URL, "http://", "http://alice:"+reg.Token+"@", 1)+"/git/alice/demo.git", "main")

	call := func(id, name, args string) map[string]any {
		return rpc(t, s, `{"jsonrpc":"2.0","id":`+id+`,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`)[0]
	}
	if c, e := text(t, call("1", "list_files", `{"owner":"alice","name":"demo"}`)); e || !strings.Contains(c, "hi.txt") {
		t.Fatalf("list_files: %s", c)
	}
	if c, e := text(t, call("2", "read_file", `{"owner":"alice","name":"demo","path":"hi.txt"}`)); e || c != "hello mcp\n" {
		t.Fatalf("read_file: %q", c)
	}
	if c, e := text(t, call("3", "list_commits", `{"owner":"alice","name":"demo"}`)); e || !strings.Contains(c, `"init"`) {
		t.Fatalf("list_commits: %s", c)
	}
	if c, e := text(t, call("4", "read_file", `{"owner":"alice","name":"demo","path":"../../etc/passwd"}`)); !e {
		t.Fatalf("traversal should error: %s", c)
	}
	if _, e := text(t, call("5", "get_repo", `{"owner":"alice"}`)); !e {
		t.Fatal("missing name should error")
	}
	if r := rpc(t, s, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"delete_everything","arguments":{}}}`)[0]; r["error"] == nil {
		t.Fatal("unknown tool accepted")
	}
	// unreachable server surfaces as a tool error, not a crash
	dead := &Server{BaseURL: "http://127.0.0.1:1", Version: "t"}
	if _, e := text(t, rpc(t, dead, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_repos","arguments":{}}}`)[0]); !e {
		t.Fatal("expected error")
	}
}
