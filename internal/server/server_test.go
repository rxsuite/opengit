package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestValidRepoName(t *testing.T) {
	for n, want := range map[string]bool{"a": true, "my-repo_1.x": true, "": false, "../x": false, ".hidden": false, "a/b": false, "x.git": false, "a..b": false} {
		if got := ValidRepoName(n); got != want {
			t.Errorf("ValidRepoName(%q)=%v want %v", n, got, want)
		}
	}
}

func TestCreateCloneAndPush(t *testing.T) {
	s, err := New(Config{DataDir: t.TempDir(), Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	// create requires the token
	resp, _ := http.Post(ts.URL+"/api/repos", "application/json", strings.NewReader(`{"name":"hello"}`))
	if resp.StatusCode != 401 {
		t.Fatalf("unauthenticated create: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("POST", ts.URL+"/api/repos", strings.NewReader(`{"name":"hello"}`))
	req.Header.Set("Authorization", "Bearer secret")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	req.Body = nil
	resp, _ = http.Post(ts.URL+"/api/repos", "application/json", strings.NewReader(`{"name":"../evil"}`))
	if resp.StatusCode == 201 {
		t.Fatal("path traversal accepted")
	}

	work := t.TempDir()
	run(t, work, "init", "-b", "main", "a")
	a := filepath.Join(work, "a")
	os.WriteFile(filepath.Join(a, "README.md"), []byte("hi\n"), 0o644)
	run(t, a, "add", ".")
	run(t, a, "commit", "-m", "first")

	// push without credentials must fail
	cmd := exec.Command("git", "push", ts.URL+"/git/hello.git", "main")
	cmd.Dir = a
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := cmd.Run(); err == nil {
		t.Fatal("anonymous push succeeded")
	}
	authURL := strings.Replace(ts.URL, "http://", "http://u:secret@", 1)
	run(t, a, "push", authURL+"/git/hello.git", "main")

	// anonymous clone works (public read)
	run(t, work, "clone", ts.URL+"/git/hello.git", "b")
	got, err := os.ReadFile(filepath.Join(work, "b", "README.md"))
	if err != nil || string(got) != "hi\n" {
		t.Fatalf("clone content: %q %v", got, err)
	}

	resp, _ = http.Get(ts.URL + "/api/repos")
	var sb strings.Builder
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	sb.Write(buf[:n])
	if !strings.Contains(sb.String(), `"hello"`) {
		t.Fatalf("list: %s", sb.String())
	}
}
