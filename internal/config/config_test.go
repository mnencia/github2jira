package config

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}

var _ = Describe("validate", func() {
	validConfig := func() *Config {
		return &Config{
			GitHub: GitHubConfig{Token: "ghp_test"},
			Jira: JiraConfig{
				URL:       "https://company.atlassian.net",
				User:      "user@company.com",
				Token:     "jira_token",
				Project:   "PROJ",
				Component: "COMP",
			},
		}
	}

	It("accepts a complete config", func() {
		Expect(validConfig().validate()).To(Succeed())
	})

	DescribeTable("rejects missing required field",
		func(clear func(*Config), field string) {
			cfg := validConfig()
			clear(cfg)
			Expect(cfg.validate()).To(MatchError(ContainSubstring(field)))
		},
		Entry("github.token", func(c *Config) { c.GitHub.Token = "" }, "github.token"),
		Entry("jira.url", func(c *Config) { c.Jira.URL = "" }, "jira.url"),
		Entry("jira.user", func(c *Config) { c.Jira.User = "" }, "jira.user"),
		Entry("jira.token", func(c *Config) { c.Jira.Token = "" }, "jira.token"),
		Entry("jira.project", func(c *Config) { c.Jira.Project = "" }, "jira.project"),
		Entry("jira.component", func(c *Config) { c.Jira.Component = "" }, "jira.component"),
	)
})
