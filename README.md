# OpenGit

Lightweight, open-source, self-hostable git forge. Single Go binary, stdlib only, runs on Linux, macOS and Windows. See [PLAN.md](PLAN.md).

## Status (M0/M1 started)

Working: create repos, clone/fetch (public) and push (token-protected) over smart HTTP, web page with the animated pixel-art logo.
Not yet: accounts, SSH, code browsing, issues, pull requests, CI. It is **not** at GitHub parity; that is the roadmap.

## Run

```bash
OPENGIT_TOKEN=changeme go run ./cmd/opengit -addr 127.0.0.1:3000 -data ./data
git clone http://127.0.0.1:3000/git/<repo>.git
git push http://user:changeme@127.0.0.1:3000/git/<repo>.git main
```

Requires `git` in PATH. Test: `go test ./...`
