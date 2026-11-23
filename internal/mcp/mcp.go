// Package mcp implements a Model Context Protocol server (stdio, JSON-RPC 2.0)
// that lets an AI agent such as Claude Code use an OpenGit server through its HTTP API.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxOut = 100 << 10 // cap tool output at 100 KiB

// Server talks to an OpenGit instance.
type Server struct {
	BaseURL string // e.g. http://127.0.0.1:3000
	Token   string // ogt_... access token (optional for public read-only use)
	HTTP    *http.Client
	Version string
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func schema(required []string, props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

var readOnly = map[string]any{"readOnlyHint": true}

var tools = []tool{
	{"list_repos", "List repositories visible to the configured account (public ones plus the owner's private ones).", schema(nil, map[string]any{}), readOnly},
	{"get_repo", "Get repository metadata: description, visibility, default branch, and whether it is empty.",
		schema([]string{"owner", "name"}, map[string]any{"owner": str("Repository owner username"), "name": str("Repository name")}), readOnly},
	{"create_repo", "Create a new repository owned by the configured account. Returns its clone URL path.",
		schema([]string{"name"}, map[string]any{"name": str("New repository name (letters, digits, . _ -)"), "description": str("Optional description"),
			"private": map[string]any{"type": "boolean", "description": "Make the repository private (default false)"}}), nil},
	{"list_files", "List files and directories at a path in a repository.",
		schema([]string{"owner", "name"}, map[string]any{"owner": str("Repository owner"), "name": str("Repository name"),
			"path": str("Directory path, empty for the root"), "ref": str("Branch, tag or commit; defaults to the default branch")}), readOnly},
	{"read_file", "Read a text file (up to 1 MiB) from a repository.",
		schema([]string{"owner", "name", "path"}, map[string]any{"owner": str("Repository owner"), "name": str("Repository name"),
			"path": str("File path"), "ref": str("Branch, tag or commit; defaults to the default branch")}), readOnly},
	{"list_commits", "List recent commits (newest first).",
		schema([]string{"owner", "name"}, map[string]any{"owner": str("Repository owner"), "name": str("Repository name"),
			"ref": str("Branch, tag or commit; defaults to the default branch"), "limit": map[string]any{"type": "integer", "description": "1-100, default 30"}}), readOnly},
}

// Serve reads newline-delimited JSON-RPC messages from in and writes responses to out until EOF.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			enc.Encode(rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}})
			continue
		}
		if len(req.ID) == 0 { // notification: no response
			continue
		}
		res, e := s.handle(req)
		resp := rpcResp{JSONRPC: "2.0", ID: req.ID}
		if e != nil {
			resp.Error = e
		} else {
			resp.Result = res
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(req rpcReq) (any, *rpcErr) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "opengit", "version": s.Version},
			"instructions":    "Tools for browsing and creating repositories on an OpenGit server.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcErr{-32602, "invalid params"}
		}
		known := false
		for _, t := range tools {
			known = known || t.Name == p.Name
		}
		if !known {
			return nil, &rpcErr{-32602, "unknown tool: " + p.Name}
		}
		text, err := s.call(p.Name, p.Arguments)
		if err != nil {
			return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": err.Error()}}}, nil
		}
		if len(text) > maxOut {
			text = text[:maxOut] + "\n…[truncated]"
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}, nil
	}
	return nil, &rpcErr{-32601, "method not found: " + req.Method}
}

func arg(a map[string]any, k string) string {
	v, _ := a[k].(string)
	return v
}

func (s *Server) call(name string, a map[string]any) (string, error) {
	owner, repo := arg(a, "owner"), arg(a, "name")
	base := "/api/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	q := url.Values{}
	if v := arg(a, "ref"); v != "" {
		q.Set("ref", v)
	}
	switch name {
	case "list_repos":
		return s.do("GET", "/api/repos", nil)
	case "get_repo", "list_files", "read_file", "list_commits":
		if owner == "" || repo == "" {
			return "", fmt.Errorf("owner and name are required")
		}
	}
	switch name {
	case "get_repo":
		return s.do("GET", base, nil)
	case "create_repo":
		if repo == "" {
			return "", fmt.Errorf("name is required")
		}
		priv, _ := a["private"].(bool)
		return s.do("POST", "/api/repos", map[string]any{"name": repo, "description": arg(a, "description"), "private": priv})
	case "list_files":
		q.Set("path", arg(a, "path"))
		return s.do("GET", base+"/tree?"+q.Encode(), nil)
	case "read_file":
		if arg(a, "path") == "" {
			return "", fmt.Errorf("path is required")
		}
		q.Set("path", arg(a, "path"))
		out, err := s.do("GET", base+"/blob?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		var b struct {
			Binary  bool   `json:"binary"`
			Size    int64  `json:"size"`
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(out), &b) == nil {
			if b.Binary {
				return fmt.Sprintf("(binary file, %d bytes)", b.Size), nil
			}
			return b.Content, nil
		}
		return out, nil
	case "list_commits":
		if l, ok := a["limit"].(float64); ok {
			q.Set("limit", fmt.Sprint(int(l)))
		}
		return s.do("GET", base+"/commits?"+q.Encode(), nil)
	}
	return "", fmt.Errorf("unknown tool")
}

func (s *Server) do(method, path string, body any) (string, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(s.BaseURL, "/")+path, rd)
	if err != nil {
		return "", err
	}
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach OpenGit at %s: %v", s.BaseURL, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return "", fmt.Errorf("OpenGit: %s (HTTP %d)", e.Error, resp.StatusCode)
		}
		return "", fmt.Errorf("OpenGit: HTTP %d", resp.StatusCode)
	}
	return strings.TrimSpace(string(data)), nil
}
