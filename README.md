# github2jira

A CLI tool that creates JIRA tracking tickets from GitHub issue and pull request URLs.

It fetches metadata from GitHub via the GraphQL API, auto-detects the JIRA issue type, checks for existing duplicates, creates the ticket, and transitions it to the appropriate workflow status.

## Installation

```sh
go install github.com/mnencia/github2jira@latest
```

Or build from source:

```sh
git clone https://github.com/mnencia/github2jira.git
cd github2jira
go build -o github2jira .
```

## Configuration

The configuration file is stored in the OS user config directory:

- Linux: `$XDG_CONFIG_HOME/github2jira/config.yaml` (default `~/.config/github2jira/config.yaml`)
- macOS: `~/Library/Application Support/github2jira/config.yaml`
- Windows: `%AppData%\github2jira\config.yaml`

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
    with_pr: "In Development"   # default: In Development
    without_pr: "Ready"         # default: Ready
    merged_pr: "Done"           # default: Done
    abandoned: "Abandoned"      # default: Abandoned
  users:
    github-login: "jira-email@company.com"
```

The config file contains API tokens and should be kept private:

```sh
chmod 600 ~/.config/github2jira/config.yaml
```

### Required fields

- `github.token` -- GitHub personal access token with repo read access
- `jira.url` -- JIRA Cloud instance base URL
- `jira.user` -- JIRA account email
- `jira.token` -- JIRA API token (generated at https://id.atlassian.com/manage-profile/security/api-tokens)

### Required project fields

- `jira.project` -- JIRA project key (no default, must be set)
- `jira.component` -- JIRA component name (no default, must be set)

### Optional fields (with defaults)

- `jira.statuses.with_pr` -- workflow transition when an open PR exists (default: `In Development`)
- `jira.statuses.without_pr` -- workflow transition when no PR exists (default: `Ready`)
- `jira.statuses.merged_pr` -- workflow transition when the PR is merged (default: `Done`)
- `jira.statuses.abandoned` -- status name that marks issues as abandoned during duplicate detection (default: `Abandoned`)
- `jira.users` -- GitHub login to JIRA user mapping (see below)

### User mapping for auto-assignment

Created JIRA tickets are automatically assigned to the GitHub PR or issue author. The author's GitHub display name is used to search for a matching JIRA user. When the display name doesn't match (different naming conventions, pseudonyms, etc.), add an explicit mapping in `jira.users`:

```yaml
jira:
  users:
    github-login: "jira-email@company.com"
    other-login: "JIRA Display Name"
```

Values can be an email address, display name, or any string the JIRA user search API resolves. If resolution fails (no match or multiple matches), a warning is logged and the ticket is created without an assignee.

## Usage

```sh
github2jira <github-url>
```

### Examples

Create a JIRA ticket from a GitHub issue:

```sh
github2jira https://github.com/my-org/my-repo/issues/123
```

Create a JIRA ticket from a GitHub pull request:

```sh
github2jira https://github.com/my-org/my-repo/pull/456
```

The tool prints the issue details and the created JIRA issue key and URL on success:

```
mode: create
project: PROJ
type: Bug
summary: my-repo#456 - fix(backup): correct retention policy
description: PR: [https://...|https://...|smart-link]
assignee: John Doe
transition to: In Development
created: PROJ-1234  https://company.atlassian.net/browse/PROJ-1234
```

### Dry-run mode

Use `--dry-run` (`-n`) to preview what would be done without writing to JIRA:

```sh
github2jira --dry-run https://github.com/my-org/my-repo/pull/456
```

Example output when a new ticket would be created:

```
mode: dry-run
project: PROJ
type: Bug
summary: my-repo#456 - fix(backup): correct retention policy
description: PR: [https://...|https://...|smart-link]
assignee: John Doe
transition to: In Development
```

The output matches a real run except for `mode: dry-run` instead of `mode: create` and the absent `created:` line — no write API calls are made.

### Debug mode

Use `--debug` (`-d`) to print a detailed trace of each logical step to stderr. This is useful for troubleshooting GitHub/JIRA resolution and understanding how the tool derives issue type, PR state, and target status.

```sh
github2jira --debug --dry-run https://github.com/my-org/my-repo/pull/456
```

Debug output goes to stderr, so it can be separated from normal output with standard redirection (`2>/dev/null` to suppress, `2>trace.log` to capture).

## How it works

1. **Parse the GitHub URL** to extract the owner, repo, number, and whether it's an issue or PR.

2. **Fetch metadata from GitHub** via the GraphQL API. For issues, this includes labels and linked PRs (via `closedByPullRequestsReferences`). For PRs, this includes labels, linked issues (via `closingIssuesReferences`), and their labels.

3. **Resolve the canonical item.** When a PR URL is provided and the PR links to a GitHub issue, the issue is used as the canonical work item for the JIRA ticket title. This reflects the convention that the issue represents the work, and the PR is the implementation.

4. **Auto-detect the JIRA issue type** using a priority-based heuristic:
   - GitHub labels: `bug` -> Bug, `enhancement` or `feature` -> Story
   - PR title conventional commit prefix: `fix` -> Bug, `feat` -> Story, `chore`/`test`/`ci`/`docs`/`refactor`/`build`/`perf` -> Housekeeping
   - Default: Housekeeping

5. **Check for existing duplicates.** Before creating a new ticket, the tool searches the JIRA project for issues whose summary references the same `repo#number` and whose description contains at least one of the GitHub URLs. If matches are found, all are listed with their key, URL, and status. When exactly one active match exists, any missing GitHub links are appended to its description. No new ticket is created.

6. **Resolve the JIRA assignee.** The GitHub author's display name (or an explicit `jira.users` mapping) is searched in JIRA. If exactly one active user matches, the ticket is assigned to them. Failures are non-fatal.

7. **Create the JIRA ticket** with the detected issue type, project, component, and a summary in the format `repo#number - title`. The description contains the GitHub URL(s).

8. **Transition to the appropriate status.** If the PR is merged, the ticket is transitioned to the `merged_pr` status. If an open PR exists (directly or linked from the issue), the ticket is transitioned to the `with_pr` status. Otherwise, it's transitioned to the `without_pr` status. Transition failures are non-fatal -- the ticket is already created, and a warning is printed to stderr.

## License

See [LICENSE](LICENSE).
