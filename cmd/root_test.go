// Tests in this file mutate package-level variables (dryRun, debug,
// loadConfig, newGitHub, newJira, userConfigDir). They must NOT use
// t.Parallel() — doing so would cause data races.
package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/mnencia/github2jira/internal/config"
	"github.com/mnencia/github2jira/internal/github"
	"github.com/mnencia/github2jira/internal/jira"
	"github.com/spf13/cobra"
)

type mockGitHubClient struct {
	fetchIssueFunc       func(ctx context.Context, owner, repo string, number int) (*github.IssueInfo, error)
	fetchPullRequestFunc func(ctx context.Context, owner, repo string, number int) (*github.PRInfo, error)
}

func (m mockGitHubClient) FetchIssue(ctx context.Context, owner, repo string, number int) (*github.IssueInfo, error) {
	if m.fetchIssueFunc == nil {
		return nil, errors.New("unexpected FetchIssue call")
	}
	return m.fetchIssueFunc(ctx, owner, repo, number)
}

func (m mockGitHubClient) FetchPullRequest(ctx context.Context, owner, repo string, number int) (*github.PRInfo, error) {
	if m.fetchPullRequestFunc == nil {
		return nil, errors.New("unexpected FetchPullRequest call")
	}
	return m.fetchPullRequestFunc(ctx, owner, repo, number)
}

type mockJiraClient struct {
	resolveUserFunc       func(query string) (jira.ResolvedUser, error)
	findExistingFunc      func(project, repo string, number int, urls []string) ([]jira.FindResult, error)
	updateDescriptionFunc func(issueKey, description string) error
	createIssueFunc       func(params jira.CreateParams) (*jira.CreatedIssue, error)
}

func (m mockJiraClient) ResolveUser(query string) (jira.ResolvedUser, error) {
	if m.resolveUserFunc == nil {
		return jira.ResolvedUser{}, errors.New("unexpected ResolveUser call")
	}
	return m.resolveUserFunc(query)
}

func (m mockJiraClient) FindExisting(project, repo string, number int, urls []string) ([]jira.FindResult, error) {
	if m.findExistingFunc == nil {
		return nil, errors.New("unexpected FindExisting call")
	}
	return m.findExistingFunc(project, repo, number, urls)
}

func (m mockJiraClient) UpdateDescription(issueKey, description string) error {
	if m.updateDescriptionFunc == nil {
		return errors.New("unexpected UpdateDescription call")
	}
	return m.updateDescriptionFunc(issueKey, description)
}

func (m mockJiraClient) CreateIssue(params jira.CreateParams) (*jira.CreatedIssue, error) {
	if m.createIssueFunc == nil {
		return nil, errors.New("unexpected CreateIssue call")
	}
	return m.createIssueFunc(params)
}

// ----- helpers -----

func runCommand(arg string) (string, error) {
	var output bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetContext(context.Background())

	err := run(command, []string{arg})
	return output.String(), err
}

func runCommandWithWriter(w io.Writer, arg string) error {
	command := &cobra.Command{}
	command.SetOut(w)
	command.SetErr(w)
	command.SetContext(context.Background())

	return run(command, []string{arg})
}

type errWriter struct {
	err error
}

func (w errWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func testConfig() *config.Config {
	return &config.Config{
		GitHub: config.GitHubConfig{Token: "gh-token"},
		Jira: config.JiraConfig{
			URL:       "https://jira.example",
			User:      "user@example.com",
			Token:     "jira-token",
			Project:   "PROJ",
			Component: "Backend",
			Statuses: config.StatusesConfig{
				WithPR:    "In Development",
				WithoutPR: "Ready",
				MergedPR:  "Done",
				Abandoned: "Abandoned",
			},
			Users: map[string]string{
				"octocat": "jira-user@example.com",
			},
		},
	}
}

// swapCommandDeps saves all mutable package-level dependencies, replaces them
// with safe defaults that fail explicitly, and registers a t.Cleanup to restore
// the originals. Every test must override the deps it needs after calling this.
func swapCommandDeps(t *testing.T) {
	t.Helper()

	oldDryRun := dryRun
	oldDebug := debug
	oldUserConfigDir := userConfigDir
	oldLoadConfig := loadConfig
	oldNewGitHub := newGitHub
	oldNewJira := newJira

	userConfigDir = func() (string, error) {
		return "/tmp", nil
	}
	loadConfig = func(string) (*config.Config, error) {
		return nil, errors.New("loadConfig not configured in test")
	}
	newGitHub = func(string) githubClient {
		t.Fatal("newGitHub not configured in test")
		return nil
	}
	newJira = func(string, string, string) (jiraClient, error) {
		t.Fatal("newJira not configured in test")
		return nil, nil
	}
	dryRun = false
	debug = false

	t.Cleanup(func() {
		dryRun = oldDryRun
		debug = oldDebug
		userConfigDir = oldUserConfigDir
		loadConfig = oldLoadConfig
		newGitHub = oldNewGitHub
		newJira = oldNewJira
	})
}

func assertContainsAll(t *testing.T, output string, expected ...string) {
	t.Helper()

	var missing []string
	for _, fragment := range expected {
		if !strings.Contains(output, fragment) {
			missing = append(missing, fragment)
		}
	}
	if len(missing) > 0 {
		for _, m := range missing {
			t.Errorf("expected output to contain %q", m)
		}
		t.Fatalf("output was:\n%s", output)
	}
}

// standardJiraMock returns a mockJiraClient suitable for create-path tests
// where the author is "octocat" (mapped to "jira-user@example.com" via
// testConfig). resolveUser succeeds, findExisting returns empty, createIssue
// records params into *created if non-nil.
func standardJiraMock(t *testing.T, created *jira.CreateParams) mockJiraClient {
	t.Helper()
	return mockJiraClient{
		resolveUserFunc: func(query string) (jira.ResolvedUser, error) {
			if query != "jira-user@example.com" {
				t.Fatalf("standardJiraMock: unexpected resolve query %q (only supports octocat/testConfig mapping)", query)
			}
			return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
		},
		findExistingFunc: func(string, string, int, []string) ([]jira.FindResult, error) {
			return nil, nil
		},
		createIssueFunc: func(params jira.CreateParams) (*jira.CreatedIssue, error) {
			if created != nil {
				*created = params
			}
			return &jira.CreatedIssue{Key: "PROJ-42", URL: "https://jira.example/browse/PROJ-42"}, nil
		},
	}
}

// ----- tests -----

func TestRunDryRunIssueFlow(t *testing.T) {
	swapCommandDeps(t)
	dryRun = true

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchIssueFunc: func(_ context.Context, owner, repo string, number int) (*github.IssueInfo, error) {
				if owner != "acme" || repo != "widget" || number != 123 {
					t.Fatalf("unexpected issue lookup: %s/%s#%d", owner, repo, number)
				}
				return &github.IssueInfo{
					Owner:  owner,
					Repo:   repo,
					Number: number,
					Title:  "Fix login flow",
					URL:    "https://github.com/acme/widget/issues/123",
					Labels: []string{"bug"},
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
					LinkedPRs: []github.PRInfo{{
						Number: 456,
						Title:  "fix(auth): tighten validation",
						URL:    "https://github.com/acme/widget/pull/456",
						State:  "OPEN",
					}},
				}, nil
			},
		}
	}

	var createCalled bool
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(query string) (jira.ResolvedUser, error) {
				if query != "jira-user@example.com" {
					t.Fatalf("unexpected resolve query: %s", query)
				}
				return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
			},
			findExistingFunc: func(project, repo string, number int, urls []string) ([]jira.FindResult, error) {
				if project != "PROJ" || repo != "widget" || number != 123 {
					t.Fatalf("unexpected duplicate lookup: %s %s#%d", project, repo, number)
				}
				expectedURLs := []string{
					"https://github.com/acme/widget/issues/123",
					"https://github.com/acme/widget/pull/456",
				}
				if !slices.Equal(urls, expectedURLs) {
					t.Fatalf("unexpected duplicate lookup urls: %#v", urls)
				}
				return nil, nil
			},
			createIssueFunc: func(params jira.CreateParams) (*jira.CreatedIssue, error) {
				createCalled = true
				return nil, nil
			},
		}, nil
	}

	output, err := runCommand("https://github.com/acme/widget/issues/123")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if createCalled {
		t.Fatal("expected dry-run not to create an issue")
	}

	assertContainsAll(t, output,
		"mode: dry-run",
		"project: PROJ",
		"type: Bug",
		"summary: widget#123 - Fix login flow",
		"description: Issue: [https://github.com/acme/widget/issues/123|https://github.com/acme/widget/issues/123|smart-link]",
		"PR: [https://github.com/acme/widget/pull/456|https://github.com/acme/widget/pull/456|smart-link]",
		"assignee: Jira User",
		"transition to: In Development",
	)
}

func TestRunCreateIssueFromPRWithLinkedIssue(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchPullRequestFunc: func(_ context.Context, owner, repo string, number int) (*github.PRInfo, error) {
				return &github.PRInfo{
					Owner:  owner,
					Repo:   repo,
					Number: number,
					Title:  "feat(auth): improve login flow",
					URL:    "https://github.com/acme/widget/pull/456",
					State:  "OPEN",
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
					LinkedIssues: []github.IssueInfo{{
						Number: 123,
						Title:  "Improve login flow",
						URL:    "https://github.com/acme/widget/issues/123",
						Labels: []string{"enhancement"},
					}},
				}, nil
			},
		}
	}

	var created jira.CreateParams
	newJira = func(_, _, _ string) (jiraClient, error) {
		return standardJiraMock(t, &created), nil
	}

	output, err := runCommand("https://github.com/acme/widget/pull/456")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if created.Summary != "widget#123 - Improve login flow" {
		t.Fatalf("unexpected summary: %s", created.Summary)
	}
	if created.IssueType != "Story" {
		t.Fatalf("unexpected issue type: %s", created.IssueType)
	}
	if created.TargetStatus != "In Development" {
		t.Fatalf("unexpected target status: %s", created.TargetStatus)
	}
	if created.Assignee != "acct-1" {
		t.Fatalf("unexpected assignee: %s", created.Assignee)
	}

	assertContainsAll(t, output,
		"mode: create",
		"created: PROJ-42  https://jira.example/browse/PROJ-42",
	)
}

func TestRunCreateIssueFromPRWithoutLinkedIssue(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchPullRequestFunc: func(_ context.Context, owner, repo string, number int) (*github.PRInfo, error) {
				return &github.PRInfo{
					Owner:  owner,
					Repo:   repo,
					Number: 789,
					Title:  "chore: update dependencies",
					URL:    "https://github.com/acme/widget/pull/789",
					State:  "OPEN",
					Labels: []string{},
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
				}, nil
			},
		}
	}

	var created jira.CreateParams
	newJira = func(_, _, _ string) (jiraClient, error) {
		return standardJiraMock(t, &created), nil
	}

	output, err := runCommand("https://github.com/acme/widget/pull/789")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if created.Summary != "widget#789 - chore: update dependencies" {
		t.Fatalf("unexpected summary: %s", created.Summary)
	}
	if created.IssueType != "Housekeeping" {
		t.Fatalf("unexpected issue type: %s", created.IssueType)
	}

	assertContainsAll(t, output,
		"mode: create",
		"summary: widget#789 - chore: update dependencies",
		"description: PR: [https://github.com/acme/widget/pull/789|https://github.com/acme/widget/pull/789|smart-link]",
		"transition to: In Development",
		"created: PROJ-42",
	)
}

func TestRunMergedPRTransitionsToDone(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchPullRequestFunc: func(_ context.Context, owner, repo string, number int) (*github.PRInfo, error) {
				return &github.PRInfo{
					Owner:  owner,
					Repo:   repo,
					Number: 99,
					Title:  "fix(core): crash on nil map",
					URL:    "https://github.com/acme/widget/pull/99",
					State:  "MERGED",
					Labels: []string{"bug"},
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
				}, nil
			},
		}
	}

	var created jira.CreateParams
	newJira = func(_, _, _ string) (jiraClient, error) {
		return standardJiraMock(t, &created), nil
	}

	output, err := runCommand("https://github.com/acme/widget/pull/99")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if created.TargetStatus != "Done" {
		t.Fatalf("expected merged PR to target Done, got: %s", created.TargetStatus)
	}

	assertContainsAll(t, output,
		"transition to: Done",
	)
}

func TestRunIssueWithoutPRTransitionsToReady(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchIssueFunc: func(_ context.Context, owner, repo string, number int) (*github.IssueInfo, error) {
				return &github.IssueInfo{
					Owner:  owner,
					Repo:   repo,
					Number: 10,
					Title:  "Add dark mode",
					URL:    "https://github.com/acme/widget/issues/10",
					Labels: []string{"enhancement"},
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
				}, nil
			},
		}
	}

	var created jira.CreateParams
	newJira = func(_, _, _ string) (jiraClient, error) {
		return standardJiraMock(t, &created), nil
	}

	output, err := runCommand("https://github.com/acme/widget/issues/10")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if created.TargetStatus != "Ready" {
		t.Fatalf("expected issue without PR to target Ready, got: %s", created.TargetStatus)
	}

	assertContainsAll(t, output,
		"transition to: Ready",
	)
}

func TestRunUpdatesSingleExistingActiveIssueForLinkedPR(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchPullRequestFunc: func(_ context.Context, owner, repo string, number int) (*github.PRInfo, error) {
				if owner != "acme" || repo != "widget" || number != 456 {
					t.Fatalf("unexpected PR lookup: %s/%s#%d", owner, repo, number)
				}
				return &github.PRInfo{
					Owner:  owner,
					Repo:   repo,
					Number: number,
					Title:  "feat(auth): improve login flow",
					URL:    "https://github.com/acme/widget/pull/456",
					State:  "OPEN",
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
					LinkedIssues: []github.IssueInfo{{
						Number: 123,
						Title:  "Improve login flow",
						URL:    "https://github.com/acme/widget/issues/123",
						Labels: []string{"enhancement"},
					}},
				}, nil
			},
		}
	}

	var updatedIssueKey string
	var updatedDescription string
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(query string) (jira.ResolvedUser, error) {
				if query != "jira-user@example.com" {
					t.Fatalf("unexpected resolve query: %s", query)
				}
				return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
			},
			findExistingFunc: func(project, repo string, number int, urls []string) ([]jira.FindResult, error) {
				if project != "PROJ" || repo != "widget" || number != 123 {
					t.Fatalf("expected linked issue to be canonical, got %s %s#%d", project, repo, number)
				}
				expectedURLs := []string{
					"https://github.com/acme/widget/issues/123",
					"https://github.com/acme/widget/pull/456",
				}
				if !slices.Equal(urls, expectedURLs) {
					t.Fatalf("unexpected duplicate lookup urls: %#v", urls)
				}
				return []jira.FindResult{{
					Key:         "PROJ-9",
					URL:         "https://jira.example/browse/PROJ-9",
					Summary:     "widget#123 - Improve login flow",
					Status:      "In Development",
					Assignee:    "Jira User",
					Description: "Issue: [https://github.com/acme/widget/issues/123|https://github.com/acme/widget/issues/123|smart-link]",
				}}, nil
			},
			updateDescriptionFunc: func(issueKey, description string) error {
				updatedIssueKey = issueKey
				updatedDescription = description
				return nil
			},
			createIssueFunc: func(params jira.CreateParams) (*jira.CreatedIssue, error) {
				t.Fatalf("unexpected CreateIssue call: %#v", params)
				return nil, nil
			},
		}, nil
	}

	output, err := runCommand("https://github.com/acme/widget/pull/456")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if updatedIssueKey != "PROJ-9" {
		t.Fatalf("unexpected updated issue key: %s", updatedIssueKey)
	}
	if !strings.Contains(updatedDescription, "PR: [https://github.com/acme/widget/pull/456|https://github.com/acme/widget/pull/456|smart-link]") {
		t.Fatalf("expected PR link to be appended, got: %s", updatedDescription)
	}

	assertContainsAll(t, output,
		"mode: update",
		"existing: PROJ-9  https://jira.example/browse/PROJ-9",
		"summary: widget#123 - Improve login flow (unchanged)",
		"status: In Development (unchanged)",
		"assignee: Jira User (unchanged)",
		"adding missing links:",
		"https://github.com/acme/widget/pull/456",
	)
}

func TestRunDryRunUpdateSkipsDescriptionUpdate(t *testing.T) {
	swapCommandDeps(t)
	dryRun = true

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchPullRequestFunc: func(_ context.Context, owner, repo string, number int) (*github.PRInfo, error) {
				return &github.PRInfo{
					Owner:  owner,
					Repo:   repo,
					Number: 456,
					Title:  "feat(auth): improve login flow",
					URL:    "https://github.com/acme/widget/pull/456",
					State:  "OPEN",
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
					LinkedIssues: []github.IssueInfo{{
						Number: 123,
						Title:  "Improve login flow",
						URL:    "https://github.com/acme/widget/issues/123",
						Labels: []string{"enhancement"},
					}},
				}, nil
			},
		}
	}
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(string) (jira.ResolvedUser, error) {
				return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
			},
			findExistingFunc: func(string, string, int, []string) ([]jira.FindResult, error) {
				return []jira.FindResult{{
					Key:         "PROJ-9",
					URL:         "https://jira.example/browse/PROJ-9",
					Summary:     "widget#123 - Improve login flow",
					Status:      "In Development",
					Assignee:    "Jira User",
					Description: "Issue: [https://github.com/acme/widget/issues/123|https://github.com/acme/widget/issues/123|smart-link]",
				}}, nil
			},
			updateDescriptionFunc: func(string, string) error {
				t.Fatal("should not update description in dry-run mode")
				return nil
			},
			createIssueFunc: func(jira.CreateParams) (*jira.CreatedIssue, error) {
				t.Fatal("should not create in dry-run mode")
				return nil, nil
			},
		}, nil
	}

	output, err := runCommand("https://github.com/acme/widget/pull/456")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	assertContainsAll(t, output,
		"mode: dry-run",
		"existing: PROJ-9",
		"adding missing links:",
		"https://github.com/acme/widget/pull/456",
	)
}

func TestRunMultipleExistingActiveIssuesDoesNotUpdate(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchIssueFunc: func(_ context.Context, owner, repo string, number int) (*github.IssueInfo, error) {
				return &github.IssueInfo{
					Owner:  owner,
					Repo:   repo,
					Number: 10,
					Title:  "Add dark mode",
					URL:    "https://github.com/acme/widget/issues/10",
					Labels: []string{"enhancement"},
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
				}, nil
			},
		}
	}
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(string) (jira.ResolvedUser, error) {
				return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
			},
			findExistingFunc: func(string, string, int, []string) ([]jira.FindResult, error) {
				return []jira.FindResult{
					{Key: "PROJ-1", URL: "https://jira.example/browse/PROJ-1", Summary: "s1", Status: "Ready", Description: "d1"},
					{Key: "PROJ-2", URL: "https://jira.example/browse/PROJ-2", Summary: "s2", Status: "In Development", Description: "d2"},
				}, nil
			},
			updateDescriptionFunc: func(string, string) error {
				t.Fatal("should not update when multiple active issues exist")
				return nil
			},
			createIssueFunc: func(jira.CreateParams) (*jira.CreatedIssue, error) {
				t.Fatal("should not create when existing issues found")
				return nil, nil
			},
		}, nil
	}

	output, err := runCommand("https://github.com/acme/widget/issues/10")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	assertContainsAll(t, output,
		"mode: update",
		"existing: PROJ-1",
		"existing: PROJ-2",
	)
}

func TestRunAbandonedOnlyExistingReportsWithoutUpdate(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchIssueFunc: func(_ context.Context, owner, repo string, number int) (*github.IssueInfo, error) {
				return &github.IssueInfo{
					Owner:  owner,
					Repo:   repo,
					Number: 10,
					Title:  "Add dark mode",
					URL:    "https://github.com/acme/widget/issues/10",
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
				}, nil
			},
		}
	}
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(string) (jira.ResolvedUser, error) {
				return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
			},
			findExistingFunc: func(string, string, int, []string) ([]jira.FindResult, error) {
				return []jira.FindResult{{
					Key:         "PROJ-5",
					URL:         "https://jira.example/browse/PROJ-5",
					Summary:     "widget#10 - Add dark mode",
					Status:      "Abandoned",
					Description: "Issue: link",
				}}, nil
			},
			updateDescriptionFunc: func(string, string) error {
				t.Fatal("should not update abandoned issues")
				return nil
			},
			createIssueFunc: func(jira.CreateParams) (*jira.CreatedIssue, error) {
				t.Fatal("should not create when existing issues found")
				return nil, nil
			},
		}, nil
	}

	output, err := runCommand("https://github.com/acme/widget/issues/10")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	assertContainsAll(t, output,
		"mode: update",
		"existing: PROJ-5",
		"status: Abandoned (unchanged)",
	)
}

func TestRunUserResolutionFailureIsNonFatal(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchIssueFunc: func(_ context.Context, owner, repo string, number int) (*github.IssueInfo, error) {
				return &github.IssueInfo{
					Owner:  owner,
					Repo:   repo,
					Number: 10,
					Title:  "Add dark mode",
					URL:    "https://github.com/acme/widget/issues/10",
					Labels: []string{"enhancement"},
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
				}, nil
			},
		}
	}

	var created jira.CreateParams
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(string) (jira.ResolvedUser, error) {
				return jira.ResolvedUser{}, errors.New("no matching user")
			},
			findExistingFunc: func(string, string, int, []string) ([]jira.FindResult, error) {
				return nil, nil
			},
			createIssueFunc: func(params jira.CreateParams) (*jira.CreatedIssue, error) {
				created = params
				return &jira.CreatedIssue{Key: "PROJ-42", URL: "https://jira.example/browse/PROJ-42"}, nil
			},
		}, nil
	}

	output, err := runCommand("https://github.com/acme/widget/issues/10")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if created.Assignee != "" {
		t.Fatalf("expected empty assignee on resolution failure, got: %s", created.Assignee)
	}
	if strings.Contains(output, "assignee:") {
		t.Fatalf("expected no assignee line in output, got:\n%s", output)
	}

	assertContainsAll(t, output,
		"mode: create",
		"created: PROJ-42",
	)
}

// ----- error path tests -----

func TestRunUserConfigDirError(t *testing.T) {
	swapCommandDeps(t)

	userConfigDir = func() (string, error) {
		return "", errors.New("no home directory")
	}

	_, err := runCommand("https://github.com/acme/widget/issues/1")
	if err == nil {
		t.Fatal("expected error from config dir failure")
	}
	if !strings.Contains(err.Error(), "finding config directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunConfigLoadError(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return nil, errors.New("config file not found")
	}

	_, err := runCommand("https://github.com/acme/widget/issues/1")
	if err == nil {
		t.Fatal("expected error from config load failure")
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInvalidURLError(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}

	_, err := runCommand("not-a-url")
	if err == nil {
		t.Fatal("expected error from invalid URL")
	}
	if !strings.Contains(err.Error(), "parsing URL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGitHubFetchError(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchIssueFunc: func(context.Context, string, string, int) (*github.IssueInfo, error) {
				return nil, errors.New("API rate limit exceeded")
			},
		}
	}

	_, err := runCommand("https://github.com/acme/widget/issues/1")
	if err == nil {
		t.Fatal("expected error from GitHub fetch failure")
	}
	if !strings.Contains(err.Error(), "fetching issue") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunJiraClientCreationError(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchIssueFunc: func(_ context.Context, owner, repo string, number int) (*github.IssueInfo, error) {
				return &github.IssueInfo{
					Owner: owner, Repo: repo, Number: number,
					Title:  "t",
					URL:    "https://github.com/acme/widget/issues/1",
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
				}, nil
			},
		}
	}
	newJira = func(_, _, _ string) (jiraClient, error) {
		return nil, errors.New("invalid JIRA URL")
	}

	_, err := runCommand("https://github.com/acme/widget/issues/1")
	if err == nil {
		t.Fatal("expected error from JIRA client creation failure")
	}
	if !strings.Contains(err.Error(), "creating JIRA client") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFindExistingError(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchIssueFunc: func(_ context.Context, owner, repo string, number int) (*github.IssueInfo, error) {
				return &github.IssueInfo{
					Owner: owner, Repo: repo, Number: number,
					Title:  "t",
					URL:    "https://github.com/acme/widget/issues/1",
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
				}, nil
			},
		}
	}
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(string) (jira.ResolvedUser, error) {
				return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
			},
			findExistingFunc: func(string, string, int, []string) ([]jira.FindResult, error) {
				return nil, errors.New("JIRA search timeout")
			},
		}, nil
	}

	_, err := runCommand("https://github.com/acme/widget/issues/1")
	if err == nil {
		t.Fatal("expected error from FindExisting failure")
	}
	if !strings.Contains(err.Error(), "checking for existing issue") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunUpdateDescriptionError(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchPullRequestFunc: func(_ context.Context, owner, repo string, number int) (*github.PRInfo, error) {
				return &github.PRInfo{
					Owner: owner, Repo: repo, Number: 456,
					Title:  "feat(auth): improve login flow",
					URL:    "https://github.com/acme/widget/pull/456",
					State:  "OPEN",
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
					LinkedIssues: []github.IssueInfo{{
						Number: 123,
						Title:  "Improve login flow",
						URL:    "https://github.com/acme/widget/issues/123",
						Labels: []string{"enhancement"},
					}},
				}, nil
			},
		}
	}
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(string) (jira.ResolvedUser, error) {
				return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
			},
			findExistingFunc: func(string, string, int, []string) ([]jira.FindResult, error) {
				return []jira.FindResult{{
					Key:         "PROJ-9",
					URL:         "https://jira.example/browse/PROJ-9",
					Summary:     "widget#123 - Improve login flow",
					Status:      "In Development",
					Description: "Issue: [https://github.com/acme/widget/issues/123|https://github.com/acme/widget/issues/123|smart-link]",
				}}, nil
			},
			updateDescriptionFunc: func(string, string) error {
				return errors.New("JIRA API error")
			},
		}, nil
	}

	_, err := runCommand("https://github.com/acme/widget/pull/456")
	if err == nil {
		t.Fatal("expected error from UpdateDescription failure")
	}
	if !strings.Contains(err.Error(), "updating existing issue description") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCreateIssueError(t *testing.T) {
	swapCommandDeps(t)

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchIssueFunc: func(_ context.Context, owner, repo string, number int) (*github.IssueInfo, error) {
				return &github.IssueInfo{
					Owner: owner, Repo: repo, Number: 10,
					Title:  "Add dark mode",
					URL:    "https://github.com/acme/widget/issues/10",
					Labels: []string{"enhancement"},
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
				}, nil
			},
		}
	}
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(string) (jira.ResolvedUser, error) {
				return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
			},
			findExistingFunc: func(string, string, int, []string) ([]jira.FindResult, error) {
				return nil, nil
			},
			createIssueFunc: func(jira.CreateParams) (*jira.CreatedIssue, error) {
				return nil, errors.New("JIRA create failed")
			},
		}, nil
	}

	_, err := runCommand("https://github.com/acme/widget/issues/10")
	if err == nil {
		t.Fatal("expected error from CreateIssue failure")
	}
	if !strings.Contains(err.Error(), "creating JIRA issue") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunDryRunCreateWrapsOutputWriteError(t *testing.T) {
	swapCommandDeps(t)
	dryRun = true

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchIssueFunc: func(_ context.Context, owner, repo string, number int) (*github.IssueInfo, error) {
				return &github.IssueInfo{
					Owner: owner, Repo: repo, Number: number,
					Title:  "Add dark mode",
					URL:    "https://github.com/acme/widget/issues/10",
					Labels: []string{"enhancement"},
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
				}, nil
			},
		}
	}

	var createCalled bool
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(string) (jira.ResolvedUser, error) {
				return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
			},
			findExistingFunc: func(string, string, int, []string) ([]jira.FindResult, error) {
				return nil, nil
			},
			createIssueFunc: func(jira.CreateParams) (*jira.CreatedIssue, error) {
				createCalled = true
				return nil, nil
			},
		}, nil
	}

	err := runCommandWithWriter(errWriter{err: errors.New("broken pipe")}, "https://github.com/acme/widget/issues/10")
	if err == nil {
		t.Fatal("expected write error")
	}
	if createCalled {
		t.Fatal("expected dry-run not to create an issue")
	}
	if !strings.Contains(err.Error(), "writing output: broken pipe") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunDryRunUpdateWrapsOutputWriteError(t *testing.T) {
	swapCommandDeps(t)
	dryRun = true

	loadConfig = func(string) (*config.Config, error) {
		return testConfig(), nil
	}
	newGitHub = func(string) githubClient {
		return mockGitHubClient{
			fetchPullRequestFunc: func(_ context.Context, owner, repo string, number int) (*github.PRInfo, error) {
				return &github.PRInfo{
					Owner: owner, Repo: repo, Number: number,
					Title:  "feat(auth): improve login flow",
					URL:    "https://github.com/acme/widget/pull/456",
					State:  "OPEN",
					Author: github.GitHubAuthor{Login: "octocat", Name: "Mona"},
					LinkedIssues: []github.IssueInfo{{
						Number: 123,
						Title:  "Improve login flow",
						URL:    "https://github.com/acme/widget/issues/123",
						Labels: []string{"enhancement"},
					}},
				}, nil
			},
		}
	}

	var updateCalled bool
	newJira = func(_, _, _ string) (jiraClient, error) {
		return mockJiraClient{
			resolveUserFunc: func(string) (jira.ResolvedUser, error) {
				return jira.ResolvedUser{AccountID: "acct-1", DisplayName: "Jira User"}, nil
			},
			findExistingFunc: func(string, string, int, []string) ([]jira.FindResult, error) {
				return []jira.FindResult{{
					Key:         "PROJ-9",
					URL:         "https://jira.example/browse/PROJ-9",
					Summary:     "widget#123 - Improve login flow",
					Status:      "In Development",
					Description: "Issue: [https://github.com/acme/widget/issues/123|https://github.com/acme/widget/issues/123|smart-link]",
				}}, nil
			},
			updateDescriptionFunc: func(string, string) error {
				updateCalled = true
				return nil
			},
		}, nil
	}

	err := runCommandWithWriter(errWriter{err: errors.New("broken pipe")}, "https://github.com/acme/widget/pull/456")
	if err == nil {
		t.Fatal("expected write error")
	}
	if updateCalled {
		t.Fatal("expected dry-run not to update the issue description")
	}
	if !strings.Contains(err.Error(), "writing output: broken pipe") {
		t.Fatalf("unexpected error: %v", err)
	}
}
