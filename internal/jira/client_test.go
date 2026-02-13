package jira

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestJira(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Jira Suite")
}

// roundTripFunc adapts a function to the http.RoundTripper interface.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// newTestClient creates a Client whose HTTP layer is replaced by a
// RoundTripper that returns the given users for any request.  This avoids
// httptest.Server, which rejects the un-encoded query strings produced by
// go-jira's User.Find.
func newTestClient(users []jira.User) *Client {
	body, err := json.Marshal(users)
	Expect(err).NotTo(HaveOccurred())

	httpClient := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		}),
	}

	api, err := jira.NewClient("http://jira.test", httpClient)
	Expect(err).NotTo(HaveOccurred())

	return &Client{api: api, baseURL: "http://jira.test"}
}

var _ = Describe("jqlQuote", func() {
	It("wraps a plain string in double quotes", func() {
		Expect(jqlQuote("PROJ")).To(Equal(`"PROJ"`))
	})

	It("escapes backslashes", func() {
		Expect(jqlQuote(`a\b`)).To(Equal(`"a\\b"`))
	})

	It("escapes double quotes", func() {
		Expect(jqlQuote(`say "hello"`)).To(Equal(`"say \"hello\""`))
	})

	It("escapes both backslashes and double quotes", func() {
		Expect(jqlQuote(`a\"b`)).To(Equal(`"a\\\"b"`))
	})

	It("handles empty string", func() {
		Expect(jqlQuote("")).To(Equal(`""`))
	})
})

var _ = Describe("ResolveUser", func() {
	It("returns the exact display name match among partial matches", func() {
		c := newTestClient([]jira.User{
			{AccountID: "aaa", DisplayName: "Alice Johnson", Active: true},
			{AccountID: "bbb", DisplayName: "Alice Jones", Active: true},
			{AccountID: "ccc", DisplayName: "Alice Jordan", Active: true},
		})

		user, err := c.ResolveUser("Alice Johnson")
		Expect(err).NotTo(HaveOccurred())
		Expect(user.AccountID).To(Equal("aaa"))
		Expect(user.DisplayName).To(Equal("Alice Johnson"))
	})

	It("matches by email address", func() {
		c := newTestClient([]jira.User{
			{AccountID: "aaa", DisplayName: "Alice Johnson", EmailAddress: "aj@example.com", Active: true},
			{AccountID: "bbb", DisplayName: "Other User", EmailAddress: "other@example.com", Active: true},
		})

		user, err := c.ResolveUser("aj@example.com")
		Expect(err).NotTo(HaveOccurred())
		Expect(user.AccountID).To(Equal("aaa"))
		Expect(user.DisplayName).To(Equal("Alice Johnson"))
	})

	It("returns an error when no users match", func() {
		c := newTestClient([]jira.User{})

		_, err := c.ResolveUser("Nobody")
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("no active JIRA user found")))
	})

	It("returns an error when only partial matches exist", func() {
		c := newTestClient([]jira.User{
			{AccountID: "bbb", DisplayName: "Alice Jones", Active: true},
			{AccountID: "ccc", DisplayName: "Alice Jordan", Active: true},
		})

		_, err := c.ResolveUser("Alice Johnson")
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("no active JIRA user found")))
	})

	It("excludes inactive users even if the name matches exactly", func() {
		c := newTestClient([]jira.User{
			{AccountID: "aaa", DisplayName: "Alice Johnson", Active: false},
		})

		_, err := c.ResolveUser("Alice Johnson")
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("no active JIRA user found")))
	})

	It("returns an error when multiple active users match exactly", func() {
		c := newTestClient([]jira.User{
			{AccountID: "aaa", DisplayName: "John Smith", Active: true},
			{AccountID: "bbb", DisplayName: "John Smith", Active: true},
		})

		_, err := c.ResolveUser("John Smith")
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("multiple JIRA users match")))
	})

	It("picks the active user when duplicates include inactive accounts", func() {
		c := newTestClient([]jira.User{
			{AccountID: "old", DisplayName: "Alice Johnson", Active: false},
			{AccountID: "current", DisplayName: "Alice Johnson", Active: true},
		})

		user, err := c.ResolveUser("Alice Johnson")
		Expect(err).NotTo(HaveOccurred())
		Expect(user.AccountID).To(Equal("current"))
		Expect(user.DisplayName).To(Equal("Alice Johnson"))
	})
})
