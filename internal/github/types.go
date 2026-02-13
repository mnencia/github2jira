package github

// GitHubAuthor holds the login and display name of a GitHub user.
type GitHubAuthor struct {
	Login string
	Name  string
}

// IssueInfo holds metadata about a GitHub issue, including any pull requests
// that reference it via the closedByPullRequestsReferences field.
type IssueInfo struct {
	Owner     string
	Repo      string
	Number    int
	Title     string
	URL       string
	Labels    []string
	Author    GitHubAuthor
	LinkedPRs []PRInfo
}

// PRInfo holds metadata about a GitHub pull request, including any issues
// it closes via the closingIssuesReferences field.
type PRInfo struct {
	Owner        string
	Repo         string
	Number       int
	Title        string
	URL          string
	State        string
	Labels       []string
	Author       GitHubAuthor
	LinkedIssues []IssueInfo
}
