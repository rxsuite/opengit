// Package server implements the OpenGit HTTP server: repository management,
// git smart-HTTP hosting (via git http-backend) and the web UI.
package server

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed web
var webFS embed.FS

// Config configures a Server.
type Config struct {
	DataDir string // repositories live in DataDir/repos
	Token   string // if set, required (as basic-auth password or bearer) for writes
}

// Server is an http.Handler.
type Server struct {
	cfg      Config
	reposDir string
	gitPath  string
	mux      *http.ServeMux
}

var repoNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// ValidRepoName reports whether name is an acceptable repository name.
func ValidRepoName(name string) bool {
	return repoNameRE.MatchString(name) && !strings.HasSuffix(name, ".git") && !strings.Contains(name, "..")
}

// New creates a Server, creating the data directory if needed.
func New(cfg Config) (*Server, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git not found in PATH")
	}
	abs, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, reposDir: filepath.Join(abs, "repos"), gitPath: gitPath, mux: http.NewServeMux()}
	if err := os.MkdirAll(s.reposDir, 0o755); err != nil {
		return nil, err
	}
	sub, _ := fs.Sub(webFS, "web")
	s.mux.Handle("/", http.FileServerFS(sub))
	s.mux.HandleFunc("/api/repos", s.handleRepos)
	s.mux.HandleFunc("/git/", s.handleGit)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) authorized(r *http.Request) bool {
	if s.cfg.Token == "" {
		return true
	}
	got := ""
	if _, pw, ok := r.BasicAuth(); ok {
		got = pw
	} else if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		got = strings.TrimPrefix(h, "Bearer ")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Token)) == 1
}

func (s *Server) deny(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="OpenGit"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

type repoInfo struct {
	Name     string `json:"name"`
	CloneURL string `json:"clone_url"`
}

func (s *Server) listRepos() ([]repoInfo, error) {
	entries, err := os.ReadDir(s.reposDir)
	if err != nil {
		return nil, err
	}
	out := []repoInfo{}
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".git") {
			n := strings.TrimSuffix(e.Name(), ".git")
			out = append(out, repoInfo{Name: n, CloneURL: "/git/" + n + ".git"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		repos, err := s.listRepos()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(repos)
	case http.MethodPost:
		if !s.authorized(r) {
			s.deny(w)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad json"}`, 400)
			return
		}
		if !ValidRepoName(req.Name) {
			http.Error(w, `{"error":"invalid repository name"}`, 400)
			return
		}
		dir := filepath.Join(s.reposDir, req.Name+".git")
		if _, err := os.Stat(dir); err == nil {
			http.Error(w, `{"error":"repository exists"}`, 409)
			return
		}
		if out, err := exec.Command(s.gitPath, "init", "--bare", "--initial-branch=main", dir).CombinedOutput(); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, string(out)), 500)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(repoInfo{Name: req.Name, CloneURL: "/git/" + req.Name + ".git"})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// handleGit serves the git smart-HTTP protocol through git http-backend.
func (s *Server) handleGit(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/git/")
	name, _, _ := strings.Cut(rest, "/")
	if !strings.HasSuffix(name, ".git") || !ValidRepoName(strings.TrimSuffix(name, ".git")) {
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(filepath.Join(s.reposDir, name)); err != nil {
		http.NotFound(w, r)
		return
	}
	isWrite := strings.HasSuffix(r.URL.Path, "/git-receive-pack") || r.URL.Query().Get("service") == "git-receive-pack"
	if isWrite && !s.authorized(r) {
		s.deny(w)
		return
	}
	h := &cgi.Handler{
		Path: s.gitPath,
		Dir:  s.reposDir,
		Root: "/git",
		Args: []string{"http-backend"},
		Env: []string{
			"GIT_PROJECT_ROOT=" + s.reposDir,
			"GIT_HTTP_EXPORT_ALL=1",
			"REMOTE_USER=opengit",
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.receivepack",
			"GIT_CONFIG_VALUE_0=true",
		},
	}
	h.ServeHTTP(w, r)
}
