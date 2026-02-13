package github

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// URLKind distinguishes between GitHub issue and pull request URLs.
type URLKind string

const (
	// KindIssue represents a GitHub issue URL.
	KindIssue URLKind = "issue"
	// KindPullRequest represents a GitHub pull request URL.
	KindPullRequest URLKind = "pull"
)

// ParsedURL contains the components extracted from a GitHub issue or PR URL.
type ParsedURL struct {
	Owner  string
	Repo   string
	Number int
	Kind   URLKind
}

// ParseURL extracts the owner, repo, number, and kind from a GitHub URL of the
// form https://github.com/{owner}/{repo}/{issues|pull}/{number}.
func ParseURL(rawURL string) (*ParsedURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if u.Host != "github.com" {
		return nil, fmt.Errorf("not a github.com URL: %s", u.Host)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 {
		return nil, fmt.Errorf("expected URL format: https://github.com/{owner}/{repo}/{issues|pull}/{number}")
	}

	owner := parts[0]
	repo := parts[1]
	kindStr := parts[2]
	numberStr := parts[3]

	var kind URLKind
	switch kindStr {
	case "issues":
		kind = KindIssue
	case "pull":
		kind = KindPullRequest
	default:
		return nil, fmt.Errorf("unsupported URL type %q: expected 'issues' or 'pull'", kindStr)
	}

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		return nil, fmt.Errorf("invalid issue/PR number %q: %w", numberStr, err)
	}

	return &ParsedURL{
		Owner:  owner,
		Repo:   repo,
		Number: number,
		Kind:   kind,
	}, nil
}
