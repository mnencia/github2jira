package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newGraphQLTestClient(t *testing.T, responseBody string) *Client {
	t.Helper()

	return &Client{
		token: "gh-token",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(responseBody)),
				}, nil
			}),
		},
	}
}

func TestFetchIssueReturnsErrorWhenIssueIsMissing(t *testing.T) {
	client := newGraphQLTestClient(t, `{"data":{"repository":{"issue":null}}}`)

	_, err := client.FetchIssue(context.Background(), "acme", "widget", 123)
	if err == nil {
		t.Fatal("expected an error for a missing issue")
	}
	if !strings.Contains(err.Error(), `issue #123 not found in acme/widget`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchPullRequestReturnsErrorWhenPullRequestIsMissing(t *testing.T) {
	client := newGraphQLTestClient(t, `{"data":{"repository":{"pullRequest":null}}}`)

	_, err := client.FetchPullRequest(context.Background(), "acme", "widget", 456)
	if err == nil {
		t.Fatal("expected an error for a missing pull request")
	}
	if !strings.Contains(err.Error(), `pull request #456 not found in acme/widget`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
