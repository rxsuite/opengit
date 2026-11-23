// Package gitx reads git repositories by shelling out to the git binary.
// Every call is a short-lived subprocess, so memory is only used per request.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrBadArg = errors.New("invalid ref or path")
var ErrNotFound = errors.New("not found")
var ErrTooLarge = errors.New("file too large")

const MaxBlob = 1 << 20 // 1 MiB shown in the UI

var refRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@~^-]{0,199}$`)

// ValidRef rejects anything that could be parsed as an option or a path escape.
func ValidRef(ref string) bool { return refRE.MatchString(ref) && !strings.Contains(ref, "..") }

func validPath(p string) bool {
	if p == "" {
		return true
	}
	if strings.ContainsAny(p, "\x00\n\\") || strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

func run(dir string, max int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout = &limitWriter{&out, max}
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if errors.Is(err, errLimit) || out.Len() >= max && max > 0 {
			return out.Bytes(), ErrTooLarge
		}
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

var errLimit = errors.New("limit")

type limitWriter struct {
	b   *bytes.Buffer
	max int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.max > 0 && l.b.Len()+len(p) > l.max {
		l.b.Write(p[:l.max-l.b.Len()])
		return 0, errLimit
	}
	return l.b.Write(p)
}

// DefaultBranch returns HEAD's branch name ("" for an empty repository).
func DefaultBranch(dir string) string {
	out, err := run(dir, 4096, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// HasCommits reports whether ref resolves to a commit.
func HasCommits(dir, ref string) bool {
	if !ValidRef(ref) {
		return false
	}
	_, err := run(dir, 4096, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

type Entry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "tree" or "blob"
	Size int64  `json:"size"`
}

// Tree lists a directory at ref ("" path = root). Directories sort first.
func Tree(dir, ref, path string) ([]Entry, error) {
	if !ValidRef(ref) || !validPath(path) {
		return nil, ErrBadArg
	}
	spec := ref
	if path = strings.Trim(path, "/"); path != "" {
		spec = ref + ":" + path
	}
	out, err := run(dir, 4<<20, "ls-tree", "-z", "-l", spec)
	if err != nil {
		return nil, ErrNotFound
	}
	var es []Entry
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, name, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) < 4 {
			continue
		}
		sz, _ := strconv.ParseInt(f[3], 10, 64)
		typ := f[1]
		if typ == "commit" { // submodule
			typ = "blob"
		}
		es = append(es, Entry{Name: name, Type: typ, Size: sz})
	}
	// directories first, then by name (stable simple sort)
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && less(es[j], es[j-1]); j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
	return es, nil
}

func less(a, b Entry) bool {
	if a.Type != b.Type {
		return a.Type == "tree"
	}
	return strings.ToLower(a.Name) < strings.ToLower(b.Name)
}

type Blob struct {
	Size    int64  `json:"size"`
	Binary  bool   `json:"binary"`
	Content string `json:"content,omitempty"`
}

// ReadBlob returns a file's content (omitted when binary or over MaxBlob).
func ReadBlob(dir, ref, path string) (*Blob, error) {
	path = strings.Trim(path, "/")
	if !ValidRef(ref) || !validPath(path) || path == "" {
		return nil, ErrBadArg
	}
	spec := ref + ":" + path
	szOut, err := run(dir, 64, "cat-file", "-s", spec)
	if err != nil {
		return nil, ErrNotFound
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(string(szOut)), 10, 64)
	b := &Blob{Size: size}
	if size > MaxBlob {
		return b, ErrTooLarge
	}
	data, err := run(dir, MaxBlob+1, "cat-file", "blob", spec)
	if err != nil {
		return nil, ErrNotFound
	}
	if bytes.IndexByte(data, 0) >= 0 {
		b.Binary = true
		return b, nil
	}
	b.Content = strings.ToValidUTF8(string(data), "�")
	return b, nil
}

type Commit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

// Log returns up to n commits reachable from ref.
func Log(dir, ref string, n int) ([]Commit, error) {
	if !ValidRef(ref) {
		return nil, ErrBadArg
	}
	out, err := run(dir, 4<<20, "log", "-n", strconv.Itoa(n), "--format=%H%x1f%an%x1f%aI%x1f%s%x1e", ref, "--")
	if err != nil {
		return nil, ErrNotFound
	}
	cs := []Commit{}
	for _, rec := range strings.Split(string(out), "\x1e") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(f) == 4 {
			cs = append(cs, Commit{f[0], f[1], f[2], f[3]})
		}
	}
	return cs, nil
}
