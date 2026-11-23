// Package store is OpenGit's SQLite persistence layer (users, tokens, repos).
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

var (
	ErrExists      = errors.New("already exists")
	ErrNotFound    = errors.New("not found")
	ErrInvalidName = errors.New("invalid name")
	ErrBadPassword = errors.New("password must be at least 8 characters")
	ErrBadCreds    = errors.New("invalid credentials")
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

type Repo struct {
	ID          int64  `json:"id"`
	OwnerID     int64  `json:"-"`
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
}

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS users(
  id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  pw_hash TEXT NOT NULL, is_admin INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS tokens(
  id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  hash TEXT NOT NULL UNIQUE, name TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS repos(
  id INTEGER PRIMARY KEY, owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL COLLATE NOCASE, description TEXT NOT NULL DEFAULT '',
  private INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, UNIQUE(owner_id, name));
`

// Open opens (creating if needed) the database at path. Use ":memory:" in tests.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4) // keep memory small
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// Names that are unsafe as directory names on Windows or collide with routes.
var reserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true, "api": true, "git": true, "admin": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "lpt1": true, "lpt2": true, "lpt3": true}

// ValidName reports whether n is usable as a username or repository name.
func ValidName(n string) bool {
	l := strings.ToLower(n)
	return nameRE.MatchString(n) && !strings.Contains(n, "..") && !strings.HasSuffix(l, ".git") &&
		!strings.HasSuffix(n, ".") && !reserved[l]
}

// argon2id parameters: OWASP minimum profile (19 MiB), deliberately modest for low-RAM hosts.
const (
	aTime, aMem, aPar, aKey = 2, 19 * 1024, 1, 32
)

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	k := argon2.IDKey([]byte(pw), salt, aTime, aMem, aPar, aKey)
	return fmt.Sprintf("argon2id$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(k))
}

func checkPassword(pw, enc string) bool {
	p := strings.Split(enc, "$")
	if len(p) != 3 || p[0] != "argon2id" {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(p[1])
	want, e2 := base64.RawStdEncoding.DecodeString(p[2])
	if e1 != nil || e2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, aTime, aMem, aPar, aKey)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// CreateUser adds a user. The very first user becomes an administrator.
func (s *Store) CreateUser(username, password string) (*User, error) {
	if !ValidName(username) {
		return nil, ErrInvalidName
	}
	if len(password) < 8 {
		return nil, ErrBadPassword
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO users(username,pw_hash,is_admin,created_at) VALUES(?,?,?,?)`,
		username, hashPassword(password), n == 0, time.Now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrExists
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: username, IsAdmin: n == 0}, nil
}

// dummyHash equalises timing for unknown usernames.
var dummyHash = hashPassword("dummy-password")

// Authenticate verifies a username/password pair.
func (s *Store) Authenticate(username, password string) (*User, error) {
	var u User
	var enc string
	err := s.db.QueryRow(`SELECT id,username,is_admin,pw_hash FROM users WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.IsAdmin, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		checkPassword(password, dummyHash)
		return nil, ErrBadCreds
	}
	if err != nil {
		return nil, err
	}
	if !checkPassword(password, enc) {
		return nil, ErrBadCreds
	}
	return &u, nil
}

const tokenPrefix = "ogt_"

func hashToken(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

// IsToken reports whether s looks like an OpenGit access token.
func IsToken(s string) bool { return strings.HasPrefix(s, tokenPrefix) }

// CreateToken issues a new access token for the user; the plaintext is only returned here.
func (s *Store) CreateToken(userID int64, name string) (string, error) {
	b := make([]byte, 32)
	rand.Read(b)
	tok := tokenPrefix + hex.EncodeToString(b)
	_, err := s.db.Exec(`INSERT INTO tokens(user_id,hash,name,created_at) VALUES(?,?,?,?)`,
		userID, hashToken(tok), name, time.Now().Unix())
	return tok, err
}

// UserByToken resolves a token to its user.
func (s *Store) UserByToken(tok string) (*User, error) {
	var u User
	err := s.db.QueryRow(`SELECT u.id,u.username,u.is_admin FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.hash=?`,
		hashToken(tok)).Scan(&u.ID, &u.Username, &u.IsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (s *Store) CreateRepo(owner *User, name, desc string, private bool) (*Repo, error) {
	if !ValidName(name) {
		return nil, ErrInvalidName
	}
	res, err := s.db.Exec(`INSERT INTO repos(owner_id,name,description,private,created_at) VALUES(?,?,?,?,?)`,
		owner.ID, name, desc, private, time.Now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrExists
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Repo{ID: id, OwnerID: owner.ID, Owner: owner.Username, Name: name, Description: desc, Private: private}, nil
}

func (s *Store) DeleteRepo(id int64) error {
	_, err := s.db.Exec(`DELETE FROM repos WHERE id=?`, id)
	return err
}

// GetRepo looks a repository up case-insensitively.
func (s *Store) GetRepo(owner, name string) (*Repo, error) {
	var r Repo
	err := s.db.QueryRow(`SELECT r.id,r.owner_id,u.username,r.name,r.description,r.private
		FROM repos r JOIN users u ON u.id=r.owner_id WHERE u.username=? AND r.name=?`, owner, name).
		Scan(&r.ID, &r.OwnerID, &r.Owner, &r.Name, &r.Description, &r.Private)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

// ListRepos returns public repos plus all of viewer's own (viewer may be nil).
func (s *Store) ListRepos(viewer *User) ([]Repo, error) {
	var vid int64 = -1
	if viewer != nil {
		vid = viewer.ID
	}
	rows, err := s.db.Query(`SELECT r.id,r.owner_id,u.username,r.name,r.description,r.private
		FROM repos r JOIN users u ON u.id=r.owner_id WHERE r.private=0 OR r.owner_id=? ORDER BY u.username,r.name LIMIT 500`, vid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Repo{}
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.Owner, &r.Name, &r.Description, &r.Private); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountUsers returns the number of registered users.
func (s *Store) CountUsers() (n int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return
}
