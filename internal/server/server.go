// Package server implements the OpenGit HTTP server: accounts, repositories,
// code browsing API, git smart-HTTP hosting and the web UI.
package server

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rxsuite/opengit/internal/gitx"
	"github.com/rxsuite/opengit/internal/store"
)

//go:embed web
var webFS embed.FS

// Config configures a Server.
type Config struct {
	DataDir             string // holds opengit.db and repos/
	DisableRegistration bool   // after the first (admin) user exists, block signups
}

// Server is an http.Handler.
type Server struct {
	cfg      Config
	st       *store.Store
	reposDir string
	gitPath  string
	mux      *http.ServeMux
	rl       *limiter
}

// New creates a Server, creating the data directory and database if needed.
func New(cfg Config) (*Server, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git not found in PATH")
	}
	abs, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, reposDir: filepath.Join(abs, "repos"), gitPath: gitPath, mux: http.NewServeMux(), rl: newLimiter(10, time.Minute)}
	if err := os.MkdirAll(s.reposDir, 0o755); err != nil {
		return nil, err
	}
	if s.st, err = store.Open(filepath.Join(abs, "opengit.db")); err != nil {
		return nil, err
	}
	sub, _ := fs.Sub(webFS, "web")
	s.mux.Handle("/", http.FileServerFS(sub))
	s.mux.HandleFunc("POST /api/register", s.handleRegister)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("GET /api/user", s.handleUser)
	s.mux.HandleFunc("GET /api/repos", s.handleListRepos)
	s.mux.HandleFunc("POST /api/repos", s.handleCreateRepo)
	s.mux.HandleFunc("GET /api/repos/{owner}/{name}", s.handleGetRepo)
	s.mux.HandleFunc("DELETE /api/repos/{owner}/{name}", s.handleDeleteRepo)
	s.mux.HandleFunc("GET /api/repos/{owner}/{name}/tree", s.handleTree)
	s.mux.HandleFunc("GET /api/repos/{owner}/{name}/blob", s.handleBlob)
	s.mux.HandleFunc("GET /api/repos/{owner}/{name}/commits", s.handleCommits)
	s.mux.HandleFunc("/git/", s.handleGit)
	return s, nil
}

// Close releases the database.
func (s *Server) Close() error { return s.st.Close() }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !strings.HasPrefix(r.URL.Path, "/git/") {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'")
	}
	s.mux.ServeHTTP(w, r)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// authUser returns the authenticated user (nil if anonymous or credentials are bad).
// Basic auth accepts a token or an account password; Bearer accepts a token.
func (s *Server) authUser(r *http.Request) *store.User {
	secret, user := "", ""
	if u, pw, ok := r.BasicAuth(); ok {
		user, secret = u, pw
	} else if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		secret = strings.TrimPrefix(h, "Bearer ")
	}
	if secret == "" {
		return nil
	}
	if store.IsToken(secret) {
		if u, err := s.st.UserByToken(secret); err == nil {
			return u
		}
		return nil
	}
	key := clientIP(r) + "|pw"
	if user == "" || s.rl.blocked(key) {
		return nil
	}
	if u, err := s.st.Authenticate(user, secret); err == nil {
		return u
	}
	s.rl.add(key)
	return nil
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (s *Server) repoDir(rp *store.Repo) string {
	return filepath.Join(s.reposDir, strings.ToLower(rp.Owner), strings.ToLower(rp.Name)+".git")
}

func canRead(rp *store.Repo, u *store.User) bool {
	return !rp.Private || (u != nil && u.ID == rp.OwnerID)
}
func canWrite(rp *store.Repo, u *store.User) bool { return u != nil && u.ID == rp.OwnerID }

// repoForRead resolves {owner}/{name}; private repos look like 404 to others.
func (s *Server) repoForRead(w http.ResponseWriter, r *http.Request) *store.Repo {
	rp, err := s.st.GetRepo(r.PathValue("owner"), r.PathValue("name"))
	if err != nil || !canRead(rp, s.authUser(r)) {
		jsonErr(w, 404, "repository not found")
		return nil
	}
	return rp
}

// ---- accounts ----

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(v); err != nil {
		jsonErr(w, 400, "bad json")
		return false
	}
	return true
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.rl.allow(clientIP(r) + "|reg") {
		jsonErr(w, 429, "too many attempts")
		return
	}
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	if n, err := s.st.CountUsers(); err != nil {
		jsonErr(w, 500, "internal error")
		return
	} else if s.cfg.DisableRegistration && n > 0 {
		jsonErr(w, 403, "registration is disabled")
		return
	}
	u, err := s.st.CreateUser(c.Username, c.Password)
	switch {
	case errors.Is(err, store.ErrExists):
		jsonErr(w, 409, "username taken")
	case errors.Is(err, store.ErrInvalidName), errors.Is(err, store.ErrBadPassword):
		jsonErr(w, 400, err.Error())
	case err != nil:
		jsonErr(w, 500, "internal error")
	default:
		tok, _ := s.st.CreateToken(u.ID, "web")
		writeJSON(w, 201, map[string]any{"user": u, "token": tok})
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	key := clientIP(r) + "|pw"
	if s.rl.blocked(key) {
		jsonErr(w, 429, "too many failed attempts")
		return
	}
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	u, err := s.st.Authenticate(c.Username, c.Password)
	if err != nil {
		s.rl.add(key)
		jsonErr(w, 401, "invalid username or password")
		return
	}
	tok, err := s.st.CreateToken(u.ID, "web")
	if err != nil {
		jsonErr(w, 500, "internal error")
		return
	}
	writeJSON(w, 200, map[string]any{"user": u, "token": tok})
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(r)
	if u == nil {
		jsonErr(w, 401, "not signed in")
		return
	}
	writeJSON(w, 200, u)
}

// ---- repositories ----

func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) {
	rs, err := s.st.ListRepos(s.authUser(r))
	if err != nil {
		jsonErr(w, 500, "internal error")
		return
	}
	writeJSON(w, 200, rs)
}

func (s *Server) handleCreateRepo(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(r)
	if u == nil {
		jsonErr(w, 401, "sign in required")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Private     bool   `json:"private"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Description) > 500 {
		jsonErr(w, 400, "description too long")
		return
	}
	rp, err := s.st.CreateRepo(u, req.Name, req.Description, req.Private)
	switch {
	case errors.Is(err, store.ErrExists):
		jsonErr(w, 409, "repository exists")
		return
	case errors.Is(err, store.ErrInvalidName):
		jsonErr(w, 400, "invalid repository name")
		return
	case err != nil:
		jsonErr(w, 500, "internal error")
		return
	}
	dir := s.repoDir(rp)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err == nil {
		err = exec.Command(s.gitPath, "init", "--bare", "--initial-branch=main", dir).Run()
		if err == nil {
			writeJSON(w, 201, rp)
			return
		}
	}
	s.st.DeleteRepo(rp.ID)
	jsonErr(w, 500, "could not initialise repository")
}

func (s *Server) handleGetRepo(w http.ResponseWriter, r *http.Request) {
	rp := s.repoForRead(w, r)
	if rp == nil {
		return
	}
	dir := s.repoDir(rp)
	def := gitx.DefaultBranch(dir)
	writeJSON(w, 200, map[string]any{"repo": rp, "default_branch": def, "empty": !gitx.HasCommits(dir, "HEAD")})
}

func (s *Server) handleDeleteRepo(w http.ResponseWriter, r *http.Request) {
	rp, err := s.st.GetRepo(r.PathValue("owner"), r.PathValue("name"))
	u := s.authUser(r)
	if err != nil || !canRead(rp, u) {
		jsonErr(w, 404, "repository not found")
		return
	}
	if !canWrite(rp, u) {
		jsonErr(w, 403, "forbidden")
		return
	}
	if err := s.st.DeleteRepo(rp.ID); err != nil {
		jsonErr(w, 500, "internal error")
		return
	}
	os.RemoveAll(s.repoDir(rp))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) gitErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gitx.ErrBadArg):
		jsonErr(w, 400, "invalid ref or path")
	case errors.Is(err, gitx.ErrNotFound):
		jsonErr(w, 404, "not found")
	case errors.Is(err, gitx.ErrTooLarge):
		jsonErr(w, 413, "file too large to display")
	default:
		jsonErr(w, 500, "internal error")
	}
}

func (s *Server) refOrDefault(r *http.Request, dir string) string {
	if ref := r.URL.Query().Get("ref"); ref != "" {
		return ref
	}
	if d := gitx.DefaultBranch(dir); d != "" {
		return d
	}
	return "HEAD"
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	rp := s.repoForRead(w, r)
	if rp == nil {
		return
	}
	dir := s.repoDir(rp)
	es, err := gitx.Tree(dir, s.refOrDefault(r, dir), r.URL.Query().Get("path"))
	if err != nil {
		s.gitErr(w, err)
		return
	}
	writeJSON(w, 200, es)
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	rp := s.repoForRead(w, r)
	if rp == nil {
		return
	}
	dir := s.repoDir(rp)
	b, err := gitx.ReadBlob(dir, s.refOrDefault(r, dir), r.URL.Query().Get("path"))
	if err != nil {
		s.gitErr(w, err)
		return
	}
	writeJSON(w, 200, b)
}

func (s *Server) handleCommits(w http.ResponseWriter, r *http.Request) {
	rp := s.repoForRead(w, r)
	if rp == nil {
		return
	}
	dir := s.repoDir(rp)
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n < 1 || n > 100 {
		n = 30
	}
	cs, err := gitx.Log(dir, s.refOrDefault(r, dir), n)
	if err != nil {
		s.gitErr(w, err)
		return
	}
	writeJSON(w, 200, cs)
}

// ---- git smart HTTP: /git/{owner}/{name}.git/... ----

func (s *Server) handleGit(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/git/"), "/", 3)
	if len(parts) < 2 || !strings.HasSuffix(parts[1], ".git") {
		http.NotFound(w, r)
		return
	}
	rp, err := s.st.GetRepo(parts[0], strings.TrimSuffix(parts[1], ".git"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	u := s.authUser(r)
	isWrite := strings.HasSuffix(r.URL.Path, "/git-receive-pack") || r.URL.Query().Get("service") == "git-receive-pack"
	if !canRead(rp, u) || (isWrite && !canWrite(rp, u)) {
		switch {
		case u == nil:
			w.Header().Set("WWW-Authenticate", `Basic realm="OpenGit"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
		case !canRead(rp, u):
			http.NotFound(w, r)
		default:
			http.Error(w, "forbidden", http.StatusForbidden)
		}
		return
	}
	// map to the on-disk (lower-cased) path so case-insensitive URLs work everywhere
	rest := ""
	if len(parts) == 3 {
		rest = "/" + parts[2]
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/git/" + strings.ToLower(rp.Owner) + "/" + strings.ToLower(rp.Name) + ".git" + rest
	h := &cgi.Handler{
		Path: s.gitPath,
		Dir:  s.reposDir,
		Root: "/git",
		Args: []string{"http-backend"},
		Env: []string{
			"GIT_PROJECT_ROOT=" + s.reposDir,
			"GIT_HTTP_EXPORT_ALL=1",
			"REMOTE_USER=" + rp.Owner,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.receivepack",
			"GIT_CONFIG_VALUE_0=true",
		},
	}
	h.ServeHTTP(w, r2)
}

// ---- tiny in-memory rate limiter ----

type limiter struct {
	mu    sync.Mutex
	max   int
	win   time.Duration
	hits  map[string][]time.Time
	calls int
}

func newLimiter(max int, win time.Duration) *limiter {
	return &limiter{max: max, win: win, hits: map[string][]time.Time{}}
}

// blocked reports whether key has reached its limit within the window (without counting).
func (l *limiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, t := range l.hits[key] {
		if time.Since(t) < l.win {
			n++
		}
	}
	return n >= l.max
}

// add records one event for key.
func (l *limiter) add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.hits[key], time.Now())
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.calls++; l.calls%256 == 0 { // occasionally drop idle keys to bound memory
		for k, v := range l.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > l.win {
				delete(l.hits, k)
			}
		}
	}
	keep := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.win {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.max {
		l.hits[key] = keep
		return false
	}
	l.hits[key] = append(keep, now)
	return true
}
