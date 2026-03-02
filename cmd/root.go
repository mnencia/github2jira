// Package cmd implements the CLI command for github2jira.
package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mnencia/github2jira/internal/config"
	"github.com/mnencia/github2jira/internal/github"
	"github.com/mnencia/github2jira/internal/issuetype"
	"github.com/mnencia/github2jira/internal/jira"
	"github.com/spf13/cobra"
)

var version = "dev"

var (
	dryRun bool
	debug  bool
)

func init() {
	rootCmd.Version = version
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false,
		"perform read operations only, printing what would be done without writing to JIRA")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false,
		"print detailed debug trace to stderr")
}

// debugf prints a formatted debug message to stderr when debug mode is enabled.
func debugf(format string, args ...any) {
	if debug {
		log.Printf("debug: "+format, args...)
	}
}

var rootCmd = &cobra.Command{
	Use:   "github2jira <github-url>",
	Short: "Create a JIRA ticket from a GitHub issue or PR",
	Long: `github2jira creates a JIRA tracking ticket from a GitHub issue or pull request URL.

It fetches the issue/PR metadata from GitHub, resolves linked items (PRs linked
to issues and vice versa), auto-detects the appropriate JIRA issue type, creates
the ticket, and transitions it to the correct workflow status. Existing JIRA
tickets are detected before creation to avoid duplicates.

When a PR URL is provided and the PR links to an issue, the issue is used as the
canonical work item for the JIRA ticket title.

Use --dry-run (-n) to preview what would be done without writing to JIRA.
Use --debug (-d) to print a detailed trace of each step to stderr.`,
	Args: cobra.ExactArgs(1),
	RunE: run,
}

// Execute runs the root command and exits with code 1 on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// smartLink formats a URL as a JIRA wiki-markup inline smart-link.
func smartLink(url string) string {
	return fmt.Sprintf("[%s|%s|smart-link]", url, url)
}

// ghLink pairs a GitHub URL with its kind so that the correct label can be used
// when appending missing links to an existing JIRA description.
type ghLink struct {
	URL  string
	Kind github.URLKind
}

// formatLink returns the JIRA description line for a GitHub link.
func formatLink(l ghLink) string {
	if l.Kind == github.KindPullRequest {
		return fmt.Sprintf("PR: %s", smartLink(l.URL))
	}
	return fmt.Sprintf("Issue: %s", smartLink(l.URL))
}

func run(cmd *cobra.Command, args []string) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("finding config directory: %w", err)
	}

	cfg, err := config.Load(configDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	parsed, err := github.ParseURL(args[0])
	if err != nil {
		return fmt.Errorf("parsing URL: %w", err)
	}

	debugf("parsed URL: %s/%s %s #%d", parsed.Owner, parsed.Repo, parsed.Kind, parsed.Number)

	ghClient := github.NewClient(cfg.GitHub.Token)

	var (
		summary       string
		description   string
		labels        []string
		prTitle       string
		prState       string
		canonicalNum  int
		allGitHubURLs []ghLink
		author        github.GitHubAuthor
	)

	ctx := cmd.Context()

	switch parsed.Kind {
	case github.KindIssue:
		issue, err := ghClient.FetchIssue(ctx, parsed.Owner, parsed.Repo, parsed.Number)
		if err != nil {
			return fmt.Errorf("fetching issue: %w", err)
		}

		debugf("fetched issue #%d: %q by %s (%s), labels=%v",
			issue.Number, issue.Title, issue.Author.Login, issue.Author.Name, issue.Labels)

		summary = fmt.Sprintf("%s#%d - %s", parsed.Repo, issue.Number, issue.Title)
		labels = issue.Labels
		canonicalNum = issue.Number
		author = issue.Author

		// Derive aggregate PR state. Precedence: MERGED > OPEN > first PR's state.
		for _, pr := range issue.LinkedPRs {
			debugf("linked PR #%d: %q state=%s", pr.Number, pr.Title, pr.State)
			switch {
			case pr.State == "MERGED":
				prState = "MERGED"
			case prState != "MERGED" && pr.State == "OPEN":
				prState = "OPEN"
			case prState == "":
				prState = pr.State
			}
		}

		debugf("derived prState: %q", prState)

		allGitHubURLs = append(allGitHubURLs, ghLink{URL: issue.URL, Kind: github.KindIssue})

		var bodyParts []string
		bodyParts = append(bodyParts, fmt.Sprintf("Issue: %s", smartLink(issue.URL)))
		for _, pr := range issue.LinkedPRs {
			bodyParts = append(bodyParts, fmt.Sprintf("PR: %s", smartLink(pr.URL)))
			allGitHubURLs = append(allGitHubURLs, ghLink{URL: pr.URL, Kind: github.KindPullRequest})
			if prTitle == "" {
				prTitle = pr.Title
			}
		}
		description = strings.Join(bodyParts, "\n")

	case github.KindPullRequest:
		pr, err := ghClient.FetchPullRequest(ctx, parsed.Owner, parsed.Repo, parsed.Number)
		if err != nil {
			return fmt.Errorf("fetching pull request: %w", err)
		}

		debugf("fetched PR #%d: %q by %s (%s), state=%s, labels=%v",
			pr.Number, pr.Title, pr.Author.Login, pr.Author.Name, pr.State, pr.Labels)

		prState = pr.State
		prTitle = pr.Title
		author = pr.Author

		if len(pr.LinkedIssues) > 0 {
			// Use the first linked issue as the canonical item
			issue := pr.LinkedIssues[0]

			for _, li := range pr.LinkedIssues {
				debugf("linked issue #%d: %q", li.Number, li.Title)
			}

			summary = fmt.Sprintf("%s#%d - %s", parsed.Repo, issue.Number, issue.Title)
			labels = issue.Labels
			canonicalNum = issue.Number

			allGitHubURLs = append(allGitHubURLs, ghLink{URL: issue.URL, Kind: github.KindIssue})
			allGitHubURLs = append(allGitHubURLs, ghLink{URL: pr.URL, Kind: github.KindPullRequest})

			var bodyParts []string
			bodyParts = append(bodyParts, fmt.Sprintf("Issue: %s", smartLink(issue.URL)))
			bodyParts = append(bodyParts, fmt.Sprintf("PR: %s", smartLink(pr.URL)))
			description = strings.Join(bodyParts, "\n")
		} else {
			// No linked issue, use the PR itself
			summary = fmt.Sprintf("%s#%d - %s", parsed.Repo, pr.Number, pr.Title)
			labels = pr.Labels
			canonicalNum = pr.Number

			allGitHubURLs = append(allGitHubURLs, ghLink{URL: pr.URL, Kind: github.KindPullRequest})
			description = "PR: " + smartLink(pr.URL)
		}

		debugf("derived prState: %q", prState)
	}

	issueType := issuetype.Detect(labels, prTitle)
	debugf("detected issue type: %s", issueType)

	jiraClient, err := jira.NewClient(cfg.Jira.URL, cfg.Jira.User, cfg.Jira.Token)
	if err != nil {
		return fmt.Errorf("creating JIRA client: %w", err)
	}

	// Resolve PR/issue author to a JIRA user for assignment
	var assignee jira.ResolvedUser
	if author.Login != "" {
		userQuery := author.Name
		if mapped, ok := cfg.Jira.Users[author.Login]; ok {
			userQuery = mapped
		}
		debugf("resolving JIRA user: query=%q (login=%s)", userQuery, author.Login)
		if userQuery != "" {
			resolved, resolveErr := jiraClient.ResolveUser(userQuery)
			if resolveErr != nil {
				debugf("JIRA user resolution failed: %v", resolveErr)
				log.Printf("warning: could not resolve JIRA user for %s: %v", author.Login, resolveErr)
			} else {
				debugf("resolved JIRA user: %s (%s)", resolved.DisplayName, resolved.AccountID)
				assignee = resolved
			}
		}
	}

	// Check for existing JIRA issues before creating a new one
	urlStrings := make([]string, len(allGitHubURLs))
	for i, l := range allGitHubURLs {
		urlStrings[i] = l.URL
	}
	debugf("FindExisting: repo=%s number=%d", parsed.Repo, canonicalNum)
	existing, err := jiraClient.FindExisting(cfg.Jira.Project, parsed.Repo, canonicalNum, urlStrings)
	if err != nil {
		return fmt.Errorf("checking for existing issue: %w", err)
	}

	debugf("FindExisting returned %d result(s)", len(existing))
	for _, e := range existing {
		debugf("  %s status=%s", e.Key, e.Status)
	}

	if len(existing) > 0 {
		// Partition matches into active and abandoned
		var active, abandoned []jira.FindResult
		for _, e := range existing {
			if e.Status == cfg.Jira.Statuses.Abandoned {
				abandoned = append(abandoned, e)
			} else {
				active = append(active, e)
			}
		}

		debugf("active=%d abandoned=%d", len(active), len(abandoned))

		// Prefer active matches; fall back to abandoned for reporting only
		workingSet := active
		if len(active) == 0 {
			workingSet = abandoned
		}

		if dryRun {
			fmt.Println("mode: dry-run")
		} else {
			fmt.Println("mode: update")
		}

		for _, e := range workingSet {
			fmt.Printf("existing: %s  %s\n", e.Key, e.URL)
			fmt.Printf("summary: %s (unchanged)\n", e.Summary)
			fmt.Printf("status: %s (unchanged)\n", e.Status)
			if e.Assignee != "" {
				fmt.Printf("assignee: %s (unchanged)\n", e.Assignee)
			}
		}

		// Only update if there is exactly one active match
		if len(active) == 1 {
			e := active[0]

			var missingLinks []ghLink
			for _, l := range allGitHubURLs {
				if !strings.Contains(e.Description, l.URL) {
					missingLinks = append(missingLinks, l)
				}
			}

			debugf("missing links: %d", len(missingLinks))

			if len(missingLinks) > 0 {
				var newParts []string
				for _, l := range missingLinks {
					newParts = append(newParts, formatLink(l))
				}
				updatedDesc := e.Description + "\n" + strings.Join(newParts, "\n")

				fmt.Println("adding missing links:")
				for _, l := range missingLinks {
					fmt.Println(" ", l.URL)
				}
				if !dryRun {
					if err := jiraClient.UpdateDescription(e.Key, updatedDesc); err != nil {
						return fmt.Errorf("updating existing issue description: %w", err)
					}
				}
			}
		}
		return nil
	}

	// No existing issue found — create a new one
	var targetStatus string
	switch prState {
	case "MERGED":
		targetStatus = cfg.Jira.Statuses.MergedPR
	case "OPEN":
		targetStatus = cfg.Jira.Statuses.WithPR
	default:
		targetStatus = cfg.Jira.Statuses.WithoutPR
	}

	debugf("target status: %s (prState=%s)", targetStatus, prState)

	if dryRun {
		fmt.Println("mode: dry-run")
	} else {
		fmt.Println("mode: create")
	}

	fmt.Printf("project: %s\n", cfg.Jira.Project)
	fmt.Printf("type: %s\n", issueType)
	fmt.Printf("summary: %s\n", summary)
	fmt.Printf("description: %s\n", description)
	if assignee.AccountID != "" {
		fmt.Printf("assignee: %s\n", assignee.DisplayName)
	}
	fmt.Printf("transition to: %s\n", targetStatus)

	if dryRun {
		return nil
	}

	created, err := jiraClient.CreateIssue(jira.CreateParams{
		Project:      cfg.Jira.Project,
		Summary:      summary,
		Description:  description,
		IssueType:    string(issueType),
		Component:    cfg.Jira.Component,
		TargetStatus: targetStatus,
		Assignee:     assignee.AccountID,
	})
	if err != nil {
		return fmt.Errorf("creating JIRA issue: %w", err)
	}

	fmt.Printf("created: %s  %s\n", created.Key, created.URL)

	return nil
}
