# OpenGit

Lightweight, open-source, self-hostable git forge. Single Go binary, stdlib only, runs on Linux, macOS and Windows. See [PLAN.md](PLAN.md).

## Status

Working today:
- Accounts (argon2id passwords, access tokens; first account becomes admin; rate-limited login)
- Public and private repositories, create/delete via API or web UI
- `git clone` / `fetch` / `push` over smart HTTP (tokens or password; owner-only push)
- Web UI: repo list, file tree, file viewer, commit log, README (shown as plain text), animated pixel-art logo
- SQLite storage (pure Go, no cgo), single binary. Idle RSS measured around 50 MB after serving the UI (not yet a formal benchmark)

Not yet (roadmap in [PLAN.md](PLAN.md)): SSH, branches/tags UI, diffs, issues, pull requests, collaborators/orgs, webhooks, CI, markdown rendering. **It is not at GitHub parity yet.**

## Run

```bash
go run ./cmd/opengit -addr 127.0.0.1:3000 -data ./data
```

Open http://127.0.0.1:3000, create an account, create a repo, then:

```bash
git remote add origin http://127.0.0.1:3000/git/<you>/<repo>.git
git push -u origin main   # username = your username, password = your password or an ogt_ token
```

Use `-disable-registration` to close signups after the admin exists. Put it behind HTTPS (e.g. a reverse proxy) for anything beyond localhost. Requires `git` in PATH. Test with `go test ./...`.
