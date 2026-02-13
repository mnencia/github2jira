package issuetype

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIssueType(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IssueType Suite")
}

var _ = Describe("Detect", func() {
	Context("labels", func() {
		It("bug -> Bug", func() {
			Expect(Detect([]string{"bug"}, "")).To(Equal(Bug))
		})

		It("enhancement -> Story", func() {
			Expect(Detect([]string{"enhancement"}, "")).To(Equal(Story))
		})

		It("feature -> Story", func() {
			Expect(Detect([]string{"feature"}, "")).To(Equal(Story))
		})

		It("case-insensitive", func() {
			Expect(Detect([]string{"BUG"}, "")).To(Equal(Bug))
			Expect(Detect([]string{"Enhancement"}, "")).To(Equal(Story))
		})

		It("take precedence over PR title", func() {
			Expect(Detect([]string{"bug"}, "feat(core): add feature")).To(Equal(Bug))
		})

		It("unrecognized labels fall through", func() {
			Expect(Detect([]string{"good first issue", "help wanted"}, "")).To(Equal(Housekeeping))
		})
	})

	Context("PR title prefix", func() {
		DescribeTable("fix -> Bug",
			func(title string) { Expect(Detect(nil, title)).To(Equal(Bug)) },
			Entry("scoped", "fix(auth): correct validation"),
			Entry("scopeless", "fix: broken login"),
		)

		DescribeTable("feat -> Story",
			func(title string) { Expect(Detect(nil, title)).To(Equal(Story)) },
			Entry("scoped", "feat(api): add endpoint"),
			Entry("scopeless", "feat: new dashboard"),
		)

		DescribeTable("housekeeping prefixes",
			func(title string) { Expect(Detect(nil, title)).To(Equal(Housekeeping)) },
			Entry("chore(…)", "chore(deps): update dependencies"),
			Entry("chore:", "chore: cleanup"),
			Entry("test(…)", "test(auth): add login tests"),
			Entry("test:", "test: fix flaky spec"),
			Entry("ci(…)", "ci(github): add workflow"),
			Entry("ci:", "ci: fix pipeline"),
			Entry("docs(…)", "docs(api): add examples"),
			Entry("docs:", "docs: update readme"),
			Entry("refactor(…)", "refactor(core): simplify logic"),
			Entry("refactor:", "refactor: extract helper"),
			Entry("build(…)", "build(docker): multi-stage"),
			Entry("build:", "build: update Makefile"),
			Entry("perf(…)", "perf(query): optimize SQL"),
			Entry("perf:", "perf: cache results"),
		)
	})

	Context("defaults", func() {
		It("empty inputs -> Housekeeping", func() {
			Expect(Detect(nil, "")).To(Equal(Housekeeping))
		})

		It("unrecognized title -> Housekeeping", func() {
			Expect(Detect(nil, "do something")).To(Equal(Housekeeping))
		})
	})
})
