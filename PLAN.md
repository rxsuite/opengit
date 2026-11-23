# OpenGit: plan

A lightweight, open-source, self-hostable GitHub alternative.
Runs on macOS, Windows 11, and Linux. Target: **idle under ~50 MB RAM**, one binary, no external services.

## 1. Scope

"GitHub" is several products. Build in this order:

1. Git hosting over HTTP(S) and SSH (clone, push, fetch)
2. Web UI: browse repos, files, commits, diffs, blame
3. Users, orgs, permissions, SSH keys, access tokens
4. Pull requests and code review
5. Issues, labels, milestones
6. CI (simple runner), releases, webhooks, REST API

Non-goals for v1: Copilot-style AI, marketplace, social network features, Actions-compatible YAML.

## 2. Existing projects (check before building)

Gitea and Forgejo (Go) already do most of this at roughly 100-200 MB RAM. Decide early:
- **Option A:** contribute to or fork Forgejo and slim it down.
- **Option B (this plan):** build from scratch with a stricter memory budget and a smaller feature set.

Pick B only if the RAM target and simplicity are the differentiator.

## 3. Tech stack

| Concern | Choice | Why |
|---|---|---|
| Language | **Go** | Static single binary, easy cross-compilation to mac/win/linux, low RAM, good SSH/HTTP libraries. (Rust is the alternative: lower RAM, slower to build.) |
| Git operations | Shell out to the system `git` (`git http-backend`, `upload-pack`, `receive-pack`) | Correct and fast. Memory is only used during requests. Bundle or require git. Use go-git only for light read paths. |
| Storage | **SQLite** (WAL mode) via a pure-Go driver (`modernc.org/sqlite`) | No DB server, no cgo, cross-compiles cleanly |
| Web UI | Server-rendered Go templates + **htmx**, small vanilla JS, no SPA | Tiny RAM, no Node at runtime |
| SSH server | `golang.org/x/crypto/ssh` built in | No dependence on OpenSSH, which is awkward on Windows |
| Search | SQLite FTS5 | No Elasticsearch |
| Background jobs | In-process queue persisted in SQLite | No Redis |
| CI | Separate optional runner binary | Keeps the core small |

## 4. Memory budget rules

- Single process, no sidecars. Cap goroutines and set `GOMEMLIMIT`.
- Stream everything (pack data, diffs, file views). Never load whole blobs. Truncate large diffs and files.
- Cache with bounded LRUs only. Render syntax highlighting server-side with size limits.
- Heavy work (clone, gc, diff) runs in short-lived `git` subprocesses.
- Add a CI benchmark: start the server, load N repos, fail the build if idle RSS exceeds the budget.

## 5. Cross-platform concerns

- Build matrix: `linux/amd64,arm64`, `darwin/amd64,arm64`, `windows/amd64,arm64`.
- Paths: use `filepath` everywhere. Handle Windows long paths and case-insensitive file systems (macOS/Windows) when naming repos.
- Windows: run as a service (`golang.org/x/sys/windows/svc`), macOS: launchd plist, Linux: systemd unit.
- Ship installers: `.msi` or winget, Homebrew, `.deb`/`.rpm`, plus a plain tarball and Docker image.
- CI runs the test suite on all three OSes.

## 6. Architecture

```
cmd/opengit          main binary (serve, admin CLI, migrate)
internal/git         git subprocess wrapper, smart-HTTP, hooks
internal/ssh         embedded SSH server
internal/db          SQLite, migrations
internal/auth        sessions, tokens, password hashing (argon2id), optional OIDC
internal/web         handlers, templates, htmx partials
internal/api         REST API (GitHub-compatible subset where cheap)
internal/pulls       PR model, merge logic (merge/squash/rebase)
internal/issues
internal/jobs        background queue, webhooks
runner/              optional CI runner
```

Data layout: `data/repos/<owner>/<name>.git` (bare repos), `data/opengit.db`, `data/config.toml`.

## 7. Milestones

| # | Milestone | Outcome |
|---|---|---|
| M0 | Skeleton | Repo layout, license (AGPL-3.0 or MIT/Apache, decide), CI on 3 OSes, config loading, SQLite migrations |
| M1 | Git hosting | Create repo, clone/push over HTTP + SSH, user accounts, tokens |
| M2 | Web UI read | File tree, README rendering, commit log, diffs, blame, branches/tags |
| M3 | Collaboration | Issues, comments, labels, notifications (email optional) |
| M4 | Pull requests | Branch compare, review comments, merge strategies, protected branches |
| M5 | Ecosystem | Webhooks, REST API, releases, OIDC login |
| M6 | CI | Runner binary, simple pipeline file, logs, status checks on PRs |
| M7 | Packaging | Installers for all three OSes, docs site, RAM benchmark published |

## 8. Security (from day one)

argon2id password hashing, CSRF protection, strict path sanitization for repo names, sandboxed `git` subprocess args (no shell), rate limiting, signed sessions, optional 2FA (TOTP), SSH key and token scopes, regular dependency audit.

## 9. Testing

- Unit tests for the db and permission logic
- Integration tests that drive real `git clone/push` against the server
- End-to-end UI tests with Playwright
- RAM and performance benchmark gate in CI

## 10. First steps

1. Choose license and name.
2. Decide Option A (fork Forgejo) vs Option B (from scratch).
3. Scaffold the Go module, CI matrix, and SQLite migrations (M0).
4. Prove the core in a spike: serve one bare repo over smart-HTTP and measure RSS on all three OSes.
