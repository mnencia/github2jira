package cmd

import (
	"bytes"
	"context"
	"errors"
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

func TestRunDryRunIssueFlow(t *testing.T) {
	restore := swapCommandDeps(t)
	defer restore()

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

func TestRunUpdatesSingleExistingActiveIssueForLinkedPR(t *testing.T) {
	restore := swapCommandDeps(t)
	defer restore()

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

func runCommand(arg string) (string, error) {
	var output bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetContext(context.Background())

	err := run(command, []string{arg})
	return output.String(), err
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

func swapCommandDeps(t *testing.T) func() {
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
	dryRun = false
	debug = false

	return func() {
		dryRun = oldDryRun
		debug = oldDebug
		userConfigDir = oldUserConfigDir
		loadConfig = oldLoadConfig
		newGitHub = oldNewGitHub
		newJira = oldNewJira
	}
}

func assertContainsAll(t *testing.T, output string, expected ...string) {
	t.Helper()

	for _, fragment := range expected {
		if !strings.Contains(output, fragment) {
			t.Fatalf("expected output to contain %q, got:\n%s", fragment, output)
		}
	}
}
