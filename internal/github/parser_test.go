package github

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGitHub(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GitHub Suite")
}

var _ = Describe("ParseURL", func() {
	It("parses an issue URL", func() {
		parsed, err := ParseURL("https://github.com/my-org/my-repo/issues/42")
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Owner).To(Equal("my-org"))
		Expect(parsed.Repo).To(Equal("my-repo"))
		Expect(parsed.Number).To(Equal(42))
		Expect(parsed.Kind).To(Equal(KindIssue))
	})

	It("parses a pull request URL", func() {
		parsed, err := ParseURL("https://github.com/my-org/my-repo/pull/99")
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Owner).To(Equal("my-org"))
		Expect(parsed.Repo).To(Equal("my-repo"))
		Expect(parsed.Number).To(Equal(99))
		Expect(parsed.Kind).To(Equal(KindPullRequest))
	})

	It("rejects non-GitHub hosts", func() {
		_, err := ParseURL("https://gitlab.com/owner/repo/issues/1")
		Expect(err).To(MatchError(ContainSubstring("not a github.com URL")))
	})

	It("rejects missing path segments", func() {
		_, err := ParseURL("https://github.com/owner/repo")
		Expect(err).To(MatchError(ContainSubstring("expected URL format")))
	})

	It("rejects non-numeric number", func() {
		_, err := ParseURL("https://github.com/owner/repo/issues/abc")
		Expect(err).To(MatchError(ContainSubstring("invalid issue/PR number")))
	})

	It("rejects unsupported path kind", func() {
		_, err := ParseURL("https://github.com/owner/repo/wiki/123")
		Expect(err).To(MatchError(ContainSubstring("unsupported URL type")))
	})

	It("ignores extra path segments", func() {
		parsed, err := ParseURL("https://github.com/owner/repo/issues/7/extra")
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Number).To(Equal(7))
		Expect(parsed.Kind).To(Equal(KindIssue))
	})
})
