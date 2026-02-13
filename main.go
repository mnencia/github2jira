// github2jira creates JIRA tracking tickets from GitHub issue and pull request URLs.
//
// It fetches metadata from GitHub via the GraphQL API, auto-detects the JIRA
// issue type from labels and conventional commit prefixes, creates the ticket
// in JIRA, and transitions it to the appropriate workflow status.
package main

import "github.com/mnencia/github2jira/cmd"

func main() {
	cmd.Execute()
}
