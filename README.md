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

## Try it on a Mac

Easiest, no Gatekeeper warnings (needs `brew install go git`):

```bash
git clone https://github.com/rxsuite/opengit.git && cd opengit
git checkout claude/wonderful-meitner-rnry7c
go build -o opengit ./cmd/opengit
./opengit -addr 127.0.0.1:3000 -data ./data
```

Or use the prebuilt binaries: the `build` GitHub Actions workflow uploads `opengit-binaries` (macOS arm64/amd64, Linux, Windows) as a run artifact, and `./scripts/build.sh` builds them all locally into `dist/`. A downloaded unsigned binary is quarantined by macOS; clear it with `xattr -d com.apple.quarantine ./opengit-darwin-arm64 && chmod +x ./opengit-darwin-arm64`.

### Signed macOS releases

`.github/workflows/release.yml` builds a universal binary, signs it with a Developer ID Application certificate, notarizes it with Apple, and publishes a GitHub Release when you push a tag (`git tag v0.1.0 && git push origin v0.1.0`). Add these repository secrets first (Settings > Secrets and variables > Actions):

| Secret | What |
|---|---|
| `MACOS_CERT_P12` | base64 of your exported "Developer ID Application" certificate (`base64 -i cert.p12 \| pbcopy`) |
| `MACOS_CERT_PASSWORD` | password you set when exporting the .p12 |
| `MACOS_SIGN_IDENTITY` | e.g. `Developer ID Application: Your Name (TEAMID)` |
| `APPLE_API_KEY_P8` | base64 of an App Store Connect API key (.p8) |
| `APPLE_API_KEY_ID` / `APPLE_API_ISSUER_ID` | from App Store Connect > Users and Access > Integrations |

Untested until the secrets exist; expect to fix small things on the first run.

## Use it from Claude Code (MCP)

`opengit mcp` is a stdio MCP server that talks to a running OpenGit server. Tools: `list_repos`, `get_repo`, `create_repo`, `list_files`, `read_file`, `list_commits` (no delete tool, on purpose).

1. Start the server and create an account in the browser (or `curl -s -XPOST localhost:3000/api/register -d '{"username":"me","password":"a-long-password"}'`; the response contains a `token`).
2. Register it with Claude Code, using the absolute path of your binary:

```bash
claude mcp add opengit --env OPENGIT_URL=http://127.0.0.1:3000 --env OPENGIT_TOKEN=ogt_your_token -- /absolute/path/to/opengit mcp
```

3. In Claude Code run `/mcp` to confirm `opengit` is connected, then ask e.g. "list my OpenGit repos" or "read README.md from me/hello".

Keep the token private; it grants whatever your account can do. Leave `OPENGIT_TOKEN` unset for read-only access to public repos.

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
