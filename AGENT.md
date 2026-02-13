# AGENT.md

## Project overview

github2jira is a CLI tool that creates JIRA tracking tickets from GitHub issue
and pull request URLs. It fetches metadata via the GitHub GraphQL API,
auto-detects the JIRA issue type, checks for duplicates, resolves the assignee,
creates the ticket, and transitions it to the correct workflow status.

## Architecture

```
main.go              Entry point, delegates to cmd.Execute()
cmd/root.go          CLI orchestration (Cobra), workflow logic
internal/
  config/config.go   YAML config loading via Viper
  github/
    types.go         Data types (GitHubAuthor, IssueInfo, PRInfo)
    parser.go        GitHub URL parsing (owner/repo/number/kind)
    client.go        GraphQL client (raw HTTP, no typed library)
  issuetype/
    detect.go        JIRA issue type detection (labels + conventional commits)
  jira/
    client.go        JIRA Cloud API client (go-jira wrapper)
    client_test.go   Ginkgo/Gomega tests for ResolveUser
```

All application packages live under `internal/`. The GitHub client uses raw HTTP
for GraphQL queries to avoid an unnecessary typed library dependency.

## Build and run

```sh
go build -o github2jira .
go install .
```

There is no Makefile, goreleaser, or CI pipeline. Standard Go tooling only.

## Testing

Tests use **Ginkgo v2** and **Gomega**:

```sh
go test ./...
```

The JIRA client tests use a custom `http.RoundTripper` mock instead of
`httptest.Server` because go-jira's `User.Find` does not URL-encode query
parameters, causing Go's HTTP server to reject requests containing spaces.

## Linting

```sh
go vet ./...
golangci-lint run ./...
```

No `.golangci.yml` config file; default rules apply.

## Configuration

Config file location follows OS conventions (`os.UserConfigDir()`):
- Linux: `~/.config/github2jira/config.yaml`
- macOS: `~/Library/Application Support/github2jira/config.yaml`

```yaml
github:
  token: "ghp_..."

jira:
  url: "https://company.atlassian.net"
  user: "email@company.com"
  token: "jira_api_token"
  project: "PROJ"
  component: "MYCOMP"
  statuses:
    with_pr: "In Development"
    without_pr: "Ready"
    abandoned: "Abandoned"
  users:                     # optional GitHub login -> JIRA user mapping
    github-login: "jira-email@company.com"
```

Required fields: `github.token`, `jira.url`, `jira.user`, `jira.token`, `jira.project`, `jira.component`.

## Dependencies

| Dependency | Purpose |
|---|---|
| `github.com/andygrunwald/go-jira/v2/cloud` | JIRA Cloud REST API client |
| `github.com/spf13/cobra` | CLI framework |
| `github.com/spf13/viper` | Configuration file loading |
| `github.com/onsi/ginkgo/v2` | BDD test framework |
| `github.com/onsi/gomega` | Test matchers |

## Key design decisions

- **Issue type detection** uses a priority chain: GitHub labels first, then
  conventional commit prefixes in the PR title, defaulting to Housekeeping.
- **Duplicate detection** searches JIRA by summary pattern (`repo#number`) and
  verifies at least one GitHub URL appears in the description.
- **User resolution** calls JIRA's user search API then filters to exact display
  name or email matches only, since the API returns partial matches.
  Assignment failures are non-fatal (logged as warnings).
- **Existing issues** are partitioned into active and abandoned. Only active
  matches trigger description updates (appending missing links). Abandoned
  matches are reported but not modified.

## Commit conventions

- Conventional Commits 1.0.0 with semantic scope: `feat(jira):`, `fix(sync):`,
  `test(jira):`, `refactor(cmd):`, `docs:`.
- Always sign commits (`-s -S`).
- GitHub issues: add `Closes #123 ` (trailing space) in commit body.
- Keep messages concise, word wrap at 72 characters.
