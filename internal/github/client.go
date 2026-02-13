// Package github provides a client for fetching issue and pull request metadata
// from the GitHub GraphQL API.
//
// It uses raw HTTP rather than a typed GraphQL library because the queries are
// static and few, avoiding an unnecessary dependency.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const maxResponseSize = 10 << 20 // 10 MiB

const graphqlEndpoint = "https://api.github.com/graphql"

// Client communicates with the GitHub GraphQL API using a personal access token.
type Client struct {
	token      string
	httpClient *http.Client
}

// NewClient creates a GitHub GraphQL client authenticated with the given token.
func NewClient(token string) *Client {
	return &Client{
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) do(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(graphqlRequest{Query: query, Variables: variables})
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("warning: closing response body: %v", err)
		}
	}()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp graphqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	return gqlResp.Data, nil
}

const issueQuery = `
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    issue(number: $number) {
      title
      url
      author {
        login
        ... on User {
          name
        }
      }
      labels(first: 20) {
        nodes {
          name
        }
      }
      closedByPullRequestsReferences(first: 10) {
        nodes {
          number
          title
          url
          state
        }
      }
    }
  }
}
`

// FetchIssue retrieves a GitHub issue along with its labels and any pull
// requests linked via the "Development" sidebar (closedByPullRequestsReferences).
func (c *Client) FetchIssue(ctx context.Context, owner, repo string, number int) (*IssueInfo, error) {
	data, err := c.do(ctx, issueQuery, map[string]any{
		"owner":  owner,
		"repo":   repo,
		"number": number,
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Repository struct {
			Issue struct {
				Title  string `json:"title"`
				URL    string `json:"url"`
				Author struct {
					Login string `json:"login"`
					Name  string `json:"name"`
				} `json:"author"`
				Labels struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"labels"`
				ClosedByPullRequestsReferences struct {
					Nodes []struct {
						Number int    `json:"number"`
						Title  string `json:"title"`
						URL    string `json:"url"`
						State  string `json:"state"`
					} `json:"nodes"`
				} `json:"closedByPullRequestsReferences"`
			} `json:"issue"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing issue data: %w", err)
	}

	issue := result.Repository.Issue

	labels := make([]string, 0, len(issue.Labels.Nodes))
	for _, l := range issue.Labels.Nodes {
		labels = append(labels, l.Name)
	}

	linkedPRs := make([]PRInfo, 0, len(issue.ClosedByPullRequestsReferences.Nodes))
	for _, pr := range issue.ClosedByPullRequestsReferences.Nodes {
		linkedPRs = append(linkedPRs, PRInfo{
			Owner:  owner,
			Repo:   repo,
			Number: pr.Number,
			Title:  pr.Title,
			URL:    pr.URL,
			State:  pr.State,
		})
	}

	return &IssueInfo{
		Owner:  owner,
		Repo:   repo,
		Number: number,
		Title:  issue.Title,
		URL:    issue.URL,
		Labels: labels,
		Author: GitHubAuthor{
			Login: issue.Author.Login,
			Name:  issue.Author.Name,
		},
		LinkedPRs: linkedPRs,
	}, nil
}

const pullRequestQuery = `
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      title
      url
      state
      author {
        login
        ... on User {
          name
        }
      }
      labels(first: 20) {
        nodes {
          name
        }
      }
      closingIssuesReferences(first: 10) {
        nodes {
          number
          title
          url
          labels(first: 20) {
            nodes {
              name
            }
          }
        }
      }
    }
  }
}
`

// FetchPullRequest retrieves a GitHub pull request along with any issues it
// closes via the "Development" sidebar (closingIssuesReferences). Linked issues
// include their labels for issue type detection.
func (c *Client) FetchPullRequest(ctx context.Context, owner, repo string, number int) (*PRInfo, error) {
	data, err := c.do(ctx, pullRequestQuery, map[string]any{
		"owner":  owner,
		"repo":   repo,
		"number": number,
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Repository struct {
			PullRequest struct {
				Title  string `json:"title"`
				URL    string `json:"url"`
				State  string `json:"state"`
				Author struct {
					Login string `json:"login"`
					Name  string `json:"name"`
				} `json:"author"`
				Labels struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"labels"`
				ClosingIssuesReferences struct {
					Nodes []struct {
						Number int    `json:"number"`
						Title  string `json:"title"`
						URL    string `json:"url"`
						Labels struct {
							Nodes []struct {
								Name string `json:"name"`
							} `json:"nodes"`
						} `json:"labels"`
					} `json:"nodes"`
				} `json:"closingIssuesReferences"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing pull request data: %w", err)
	}

	pr := result.Repository.PullRequest

	prLabels := make([]string, 0, len(pr.Labels.Nodes))
	for _, l := range pr.Labels.Nodes {
		prLabels = append(prLabels, l.Name)
	}

	linkedIssues := make([]IssueInfo, 0, len(pr.ClosingIssuesReferences.Nodes))
	for _, iss := range pr.ClosingIssuesReferences.Nodes {
		labels := make([]string, 0, len(iss.Labels.Nodes))
		for _, l := range iss.Labels.Nodes {
			labels = append(labels, l.Name)
		}
		linkedIssues = append(linkedIssues, IssueInfo{
			Owner:  owner,
			Repo:   repo,
			Number: iss.Number,
			Title:  iss.Title,
			URL:    iss.URL,
			Labels: labels,
		})
	}

	return &PRInfo{
		Owner:  owner,
		Repo:   repo,
		Number: number,
		Title:  pr.Title,
		URL:    pr.URL,
		State:  pr.State,
		Labels: prLabels,
		Author: GitHubAuthor{
			Login: pr.Author.Login,
			Name:  pr.Author.Name,
		},
		LinkedIssues: linkedIssues,
	}, nil
}
