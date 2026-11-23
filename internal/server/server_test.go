package server

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
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func gitFails(dir string, args ...string) bool {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd.Run() != nil
}

type client struct {
	t     *testing.T
	base  string
	token string
}

func (c *client) do(method, path string, body any) (int, []byte) {
	c.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func register(t *testing.T, base, user string) *client {
	c := &client{t: t, base: base}
	code, body := c.do("POST", "/api/register", map[string]string{"username": user, "password": "password-" + user})
	if code != 201 {
		t.Fatalf("register %s: %d %s", user, code, body)
	}
	var r struct{ Token string }
	json.Unmarshal(body, &r)
	c.token = r.Token
	return c
}

func TestEndToEnd(t *testing.T) {
	s, err := New(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewServer(s)
	defer ts.Close()
	anon := &client{t: t, base: ts.URL}

	alice := register(t, ts.URL, "alice")
	bob := register(t, ts.URL, "bob")

	if code, _ := anon.do("POST", "/api/repos", map[string]any{"name": "x"}); code != 401 {
		t.Fatalf("anon create: %d", code)
	}
	for _, bad := range []string{"../evil", "a/b", "x.git", "con", ""} {
		if code, _ := alice.do("POST", "/api/repos", map[string]any{"name": bad}); code != 400 {
			t.Fatalf("name %q accepted: %d", bad, code)
		}
	}
	if code, _ := alice.do("POST", "/api/repos", map[string]any{"name": "hello", "description": "demo"}); code != 201 {
		t.Fatalf("create: %d", code)
	}
	if code, _ := alice.do("POST", "/api/repos", map[string]any{"name": "secret", "private": true}); code != 201 {
		t.Fatalf("create private: %d", code)
	}

	// local repo with files
	work := t.TempDir()
	a := filepath.Join(work, "a")
	git(t, work, "init", "-b", "main", "a")
	os.WriteFile(filepath.Join(a, "README.md"), []byte("hi\n"), 0o644)
	os.MkdirAll(filepath.Join(a, "src"), 0o755)
	os.WriteFile(filepath.Join(a, "src", "main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(a, "bin.dat"), []byte{1, 0, 2}, 0o644)
	git(t, a, "add", ".")
	git(t, a, "commit", "-m", "first")

	u := func(user, tok, path string) string {
		return strings.Replace(ts.URL, "http://", "http://"+user+":"+tok+"@", 1) + path
	}
	if !gitFails(a, "push", ts.URL+"/git/alice/hello.git", "main") {
		t.Fatal("anonymous push succeeded")
	}
	if !gitFails(a, "push", u("bob", bob.token, "/git/alice/hello.git"), "main") {
		t.Fatal("non-owner push succeeded")
	}
	git(t, a, "push", u("alice", alice.token, "/git/alice/hello.git"), "main")
	git(t, a, "push", u("alice", alice.token, "/git/alice/secret.git"), "main")

	// public clone works, case-insensitive URL too
	git(t, work, "clone", ts.URL+"/git/Alice/HELLO.git", "b")
	if got, _ := os.ReadFile(filepath.Join(work, "b", "README.md")); string(got) != "hi\n" {
		t.Fatalf("clone content %q", got)
	}
	// password auth works for git too
	git(t, work, "clone", u("alice", "password-alice", "/git/alice/secret.git"), "c")

	// private repo is closed to anon and others
	if !gitFails(work, "clone", ts.URL+"/git/alice/secret.git", "d") {
		t.Fatal("anon cloned private repo")
	}
	if !gitFails(work, "clone", u("bob", bob.token, "/git/alice/secret.git"), "e") {
		t.Fatal("bob cloned private repo")
	}
	if code, _ := bob.do("GET", "/api/repos/alice/secret/tree", nil); code != 404 {
		t.Fatalf("bob tree of private: %d", code)
	}
	if code, _ := anon.do("GET", "/api/repos/alice/secret", nil); code != 404 {
		t.Fatalf("anon private repo meta: %d", code)
	}

	// listing visibility
	_, body := anon.do("GET", "/api/repos", nil)
	if strings.Contains(string(body), "secret") || !strings.Contains(string(body), "hello") {
		t.Fatalf("anon list: %s", body)
	}
	_, body = alice.do("GET", "/api/repos", nil)
	if !strings.Contains(string(body), "secret") {
		t.Fatalf("owner list: %s", body)
	}

	// browsing API
	_, body = anon.do("GET", "/api/repos/alice/hello/tree", nil)
	var tree []struct{ Name, Type string }
	json.Unmarshal(body, &tree)
	if len(tree) != 3 || tree[0].Name != "src" || tree[0].Type != "tree" {
		t.Fatalf("tree: %s", body)
	}
	_, body = anon.do("GET", "/api/repos/alice/hello/tree?path=src", nil)
	if !strings.Contains(string(body), "main.go") {
		t.Fatalf("subtree: %s", body)
	}
	_, body = anon.do("GET", "/api/repos/alice/hello/blob?path=README.md", nil)
	if !strings.Contains(string(body), `"content":"hi\n"`) {
		t.Fatalf("blob: %s", body)
	}
	_, body = anon.do("GET", "/api/repos/alice/hello/blob?path=bin.dat", nil)
	if !strings.Contains(string(body), `"binary":true`) {
		t.Fatalf("binary blob: %s", body)
	}
	_, body = anon.do("GET", "/api/repos/alice/hello/commits", nil)
	if !strings.Contains(string(body), `"subject":"first"`) {
		t.Fatalf("commits: %s", body)
	}
	for _, bad := range []string{"?ref=--output=/tmp/x", "?ref=a..b", "?path=../../etc/passwd", "?path=/etc/passwd"} {
		if code, _ := anon.do("GET", "/api/repos/alice/hello/tree"+bad, nil); code != 400 {
			t.Fatalf("%s accepted: %d", bad, code)
		}
	}

	// delete: only the owner
	if code, _ := bob.do("DELETE", "/api/repos/alice/hello", nil); code != 403 {
		t.Fatalf("bob delete: %d", code)
	}
	if code, _ := alice.do("DELETE", "/api/repos/alice/hello", nil); code != 204 {
		t.Fatalf("alice delete: %d", code)
	}
	if code, _ := anon.do("GET", "/api/repos/alice/hello", nil); code != 404 {
		t.Fatal("deleted repo still visible")
	}
}

func TestLoginAndRegistration(t *testing.T) {
	s, _ := New(Config{DataDir: t.TempDir(), DisableRegistration: true})
	defer s.Close()
	ts := httptest.NewServer(s)
	defer ts.Close()
	c := &client{t: t, base: ts.URL}
	if code, _ := c.do("POST", "/api/register", map[string]string{"username": "root", "password": "password-root"}); code != 201 {
		t.Fatalf("first user must be allowed: %d", code)
	}
	if code, _ := c.do("POST", "/api/register", map[string]string{"username": "eve", "password": "password-eve"}); code != 403 {
		t.Fatalf("registration should be closed: %d", code)
	}
	if code, _ := c.do("POST", "/api/login", map[string]string{"username": "root", "password": "nope"}); code != 401 {
		t.Fatalf("bad login: %d", code)
	}
	code, body := c.do("POST", "/api/login", map[string]string{"username": "root", "password": "password-root"})
	if code != 200 || !strings.Contains(string(body), "ogt_") {
		t.Fatalf("login: %d %s", code, body)
	}
	for i := 0; i < 12; i++ {
		code, _ = c.do("POST", "/api/login", map[string]string{"username": "root", "password": "bad"})
	}
	if code != 429 {
		t.Fatalf("brute force not limited: %d", code)
	}
}
