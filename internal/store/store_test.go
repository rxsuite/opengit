package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestValidName(t *testing.T) {
	for n, want := range map[string]bool{"a": true, "my-repo_1.x": true, "": false, "../x": false, ".h": false, "a/b": false,
		"x.git": false, "X.GIT": false, "a..b": false, "con": false, "NUL": false, "api": false, "trail.": false} {
		if got := ValidName(n); got != want {
			t.Errorf("ValidName(%q)=%v want %v", n, got, want)
		}
	}
}

func TestUsersAndTokens(t *testing.T) {
	s := open(t)
	a, err := s.CreateUser("Alice", "password1")
	if err != nil || !a.IsAdmin {
		t.Fatalf("first user should be admin: %v %+v", err, a)
	}
	b, _ := s.CreateUser("bob", "password2")
	if b.IsAdmin {
		t.Fatal("second user must not be admin")
	}
	if _, err := s.CreateUser("alice", "password3"); !errors.Is(err, ErrExists) {
		t.Fatalf("case-insensitive duplicate: %v", err)
	}
	if _, err := s.CreateUser("carol", "short"); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("short password: %v", err)
	}
	if _, err := s.Authenticate("ALICE", "password1"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := s.Authenticate("alice", "wrong"); !errors.Is(err, ErrBadCreds) {
		t.Fatal("bad password accepted")
	}
	if _, err := s.Authenticate("nobody", "password1"); !errors.Is(err, ErrBadCreds) {
		t.Fatal("unknown user accepted")
	}
	tok, _ := s.CreateToken(a.ID, "t")
	if u, err := s.UserByToken(tok); err != nil || u.ID != a.ID {
		t.Fatalf("token lookup: %v", err)
	}
	if _, err := s.UserByToken(tok + "x"); !errors.Is(err, ErrNotFound) {
		t.Fatal("bad token accepted")
	}
}

func TestRepos(t *testing.T) {
	s := open(t)
	a, _ := s.CreateUser("alice", "password1")
	b, _ := s.CreateUser("bob", "password2")
	if _, err := s.CreateRepo(a, "pub", "", false); err != nil {
		t.Fatal(err)
	}
	s.CreateRepo(a, "priv", "", true)
	if _, err := s.CreateRepo(a, "PUB", "", false); !errors.Is(err, ErrExists) {
		t.Fatal("case-insensitive duplicate repo allowed")
	}
	if rs, _ := s.ListRepos(nil); len(rs) != 1 {
		t.Fatalf("anon sees %d", len(rs))
	}
	if rs, _ := s.ListRepos(b); len(rs) != 1 {
		t.Fatalf("bob sees %d", len(rs))
	}
	if rs, _ := s.ListRepos(a); len(rs) != 2 {
		t.Fatalf("alice sees %d", len(rs))
	}
}
