// Package issuetype detects the appropriate JIRA issue type from GitHub
// metadata using a priority-based heuristic.
package issuetype

import "strings"

// JiraIssueType represents a JIRA issue type name.
type JiraIssueType string

const (
	// Bug maps to the JIRA "Bug" issue type.
	Bug JiraIssueType = "Bug"
	// Story maps to the JIRA "Story" issue type.
	Story JiraIssueType = "Story"
	// Housekeeping maps to the JIRA "Housekeeping" issue type.
	Housekeeping JiraIssueType = "Housekeeping"
)

var housekeepingPrefixes = []string{
	"chore(", "chore:",
	"test(", "test:",
	"ci(", "ci:",
	"docs(", "docs:",
	"refactor(", "refactor:",
	"build(", "build:",
	"perf(", "perf:",
}

// Detect determines the JIRA issue type using the following priority:
//  1. GitHub labels: "bug" -> Bug, "enhancement" or "feature" -> Story
//  2. PR title conventional commit prefix: fix -> Bug, feat -> Story,
//     chore/test/ci/docs/refactor/build/perf -> Housekeeping
//  3. Default: Housekeeping
func Detect(labels []string, prTitle string) JiraIssueType {
	// Priority 1: GitHub labels
	for _, label := range labels {
		lower := strings.ToLower(label)
		if lower == "bug" {
			return Bug
		}
		if lower == "enhancement" || lower == "feature" {
			return Story
		}
	}

	// Priority 2: PR title conventional commit prefix
	if prTitle != "" {
		lower := strings.ToLower(prTitle)
		if strings.HasPrefix(lower, "fix(") || strings.HasPrefix(lower, "fix:") {
			return Bug
		}
		if strings.HasPrefix(lower, "feat(") || strings.HasPrefix(lower, "feat:") {
			return Story
		}
		for _, prefix := range housekeepingPrefixes {
			if strings.HasPrefix(lower, prefix) {
				return Housekeeping
			}
		}
	}

	// Default
	return Housekeeping
}
