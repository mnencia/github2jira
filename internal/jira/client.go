// Package jira provides a client for creating issues and executing workflow
// transitions in JIRA Cloud via the go-jira library.
package jira

import (
	"context"
	"fmt"
	"log"
	"strings"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
)

// Client wraps the go-jira Cloud client with convenience methods for issue
// creation and workflow transitions.
type Client struct {
	api     *jira.Client
	baseURL string
}

// CreateParams holds the fields needed to create a JIRA issue.
type CreateParams struct {
	Project      string
	Summary      string
	Description  string
	IssueType    string
	Component    string
	TargetStatus string
	Assignee     string
}

// CreatedIssue is returned after successful issue creation and contains the
// issue key (e.g. "PROJ-1234") and its browse URL.
type CreatedIssue struct {
	Key string
	URL string
}

// FindResult is returned when an existing JIRA issue is found that matches a
// GitHub item.
type FindResult struct {
	Key         string
	URL         string
	Summary     string
	Status      string
	Assignee    string
	Description string
}

// NewClient creates an authenticated JIRA Cloud client using basic auth
// (email + API token).
func NewClient(baseURL, user, token string) (*Client, error) {
	tp := jira.BasicAuthTransport{
		Username: user,
		APIToken: token,
	}

	api, err := jira.NewClient(baseURL, tp.Client())
	if err != nil {
		return nil, fmt.Errorf("creating JIRA client: %w", err)
	}

	return &Client{
		api:     api,
		baseURL: baseURL,
	}, nil
}

// ResolvedUser holds the account ID and display name of a resolved JIRA user.
type ResolvedUser struct {
	AccountID   string
	DisplayName string
}

// ResolveUser searches JIRA for a user matching the given query (email or
// display name) and returns the unique active match.
func (c *Client) ResolveUser(query string) (ResolvedUser, error) {
	users, resp, err := c.api.User.Find(context.Background(), query)
	if err != nil {
		if resp != nil {
			return ResolvedUser{}, fmt.Errorf("searching JIRA users (HTTP %d): %w", resp.StatusCode, err)
		}
		return ResolvedUser{}, fmt.Errorf("searching JIRA users: %w", err)
	}

	// The JIRA search API returns partial matches, so filter to exact
	// display name or email matches only.
	var active []jira.User
	for _, u := range users {
		if u.Active && (u.DisplayName == query || u.EmailAddress == query) {
			active = append(active, u)
		}
	}

	switch len(active) {
	case 0:
		return ResolvedUser{}, fmt.Errorf("no active JIRA user found for %q", query)
	case 1:
		return ResolvedUser{AccountID: active[0].AccountID, DisplayName: active[0].DisplayName}, nil
	default:
		names := make([]string, 0, len(active))
		for _, u := range active {
			names = append(names, fmt.Sprintf("%s (%s)", u.DisplayName, u.AccountID))
		}
		return ResolvedUser{}, fmt.Errorf("multiple JIRA users match %q: %s; add an explicit mapping in jira.users", query, strings.Join(names, ", "))
	}
}

// CreateIssue creates a new JIRA issue with the given parameters and returns
// the created issue key and browse URL.
func (c *Client) CreateIssue(params CreateParams) (*CreatedIssue, error) {
	issue := &jira.Issue{
		Fields: &jira.IssueFields{
			Project: jira.Project{
				Key: params.Project,
			},
			Summary:     params.Summary,
			Description: params.Description,
			Type: jira.IssueType{
				Name: params.IssueType,
			},
		},
	}

	if params.Component != "" {
		issue.Fields.Components = []*jira.Component{
			{Name: params.Component},
		}
	}

	if params.Assignee != "" {
		issue.Fields.Assignee = &jira.User{AccountID: params.Assignee}
	}

	created, resp, err := c.api.Issue.Create(context.Background(), issue)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("creating JIRA issue (HTTP %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("creating JIRA issue: %w", err)
	}

	result := &CreatedIssue{
		Key: created.Key,
		URL: fmt.Sprintf("%s/browse/%s", c.baseURL, created.Key),
	}

	if params.TargetStatus != "" {
		if err := c.TransitionTo(result.Key, params.TargetStatus); err != nil {
			log.Printf("warning: could not transition to %q: %v", params.TargetStatus, err)
		}
	}

	return result, nil
}

// TransitionTo moves an issue to the given workflow status by finding and
// executing the matching transition. Returns an error if no transition with
// the given name is available.
func (c *Client) TransitionTo(issueKey, targetStatus string) error {
	transitions, resp, err := c.api.Issue.GetTransitions(context.Background(), issueKey)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("listing transitions (HTTP %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("listing transitions: %w", err)
	}

	var transitionID string
	for _, t := range transitions {
		if t.Name == targetStatus {
			transitionID = t.ID
			break
		}
	}

	if transitionID == "" {
		available := make([]string, 0, len(transitions))
		for _, t := range transitions {
			available = append(available, t.Name)
		}
		return fmt.Errorf("transition %q not found, available: %v", targetStatus, available)
	}

	_, err = c.api.Issue.DoTransition(context.Background(), issueKey, transitionID)
	if err != nil {
		return fmt.Errorf("executing transition %q: %w", targetStatus, err)
	}

	return nil
}

// jqlQuote escapes s for use in a JQL double-quoted string.
func jqlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// FindExisting searches JIRA for issues in the given project whose summary
// references repo#number and whose description contains at least one of the
// provided GitHub URLs. Returns all verified matches with their status.
func (c *Client) FindExisting(project, repo string, number int, urls []string) ([]FindResult, error) {
	jql := fmt.Sprintf(`project = %s AND (summary ~ %s OR summary ~ %s)`,
		jqlQuote(project), jqlQuote(fmt.Sprintf("%s#%d", repo, number)), jqlQuote(fmt.Sprintf("#%d", number)))

	issues, resp, err := c.api.Issue.SearchV2JQL(context.Background(), jql, &jira.SearchOptionsV2{
		Fields:     []string{"summary", "description", "status", "assignee"},
		MaxResults: 10,
	})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("searching JIRA (HTTP %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("searching JIRA: %w", err)
	}

	var results []FindResult
	for _, issue := range issues {
		desc := issue.Fields.Description
		for _, u := range urls {
			if strings.Contains(desc, u) {
				statusName := ""
				if issue.Fields.Status != nil {
					statusName = issue.Fields.Status.Name
				}
				assignee := ""
				if issue.Fields.Assignee != nil {
					assignee = issue.Fields.Assignee.DisplayName
				}
				results = append(results, FindResult{
					Key:         issue.Key,
					URL:         fmt.Sprintf("%s/browse/%s", c.baseURL, issue.Key),
					Summary:     issue.Fields.Summary,
					Status:      statusName,
					Assignee:    assignee,
					Description: desc,
				})
				break
			}
		}
	}

	return results, nil
}

// UpdateDescription replaces the description of an existing JIRA issue.
func (c *Client) UpdateDescription(issueKey, description string) error {
	issue := &jira.Issue{
		Key: issueKey,
		Fields: &jira.IssueFields{
			Description: description,
		},
	}

	_, resp, err := c.api.Issue.Update(context.Background(), issue, nil)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("updating description (HTTP %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("updating description: %w", err)
	}

	return nil
}
