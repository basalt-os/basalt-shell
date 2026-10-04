package skills

import (
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"Find the PDF the bank sent last month.":                  SkillFiles,
		"Show me the invoice from the electricity company.":       SkillFiles,
		"Where is my passport scan?":                              SkillFiles,
		"Find the spreadsheet with the travel budget.":            SkillFiles,
		"Open the second result.":                                 SkillOpen,
		"open result 3":                                           SkillOpen,
		"What did Ana say in her last email?":                     SkillMail,
		"Summarize my unread emails from this week.":              SkillMail,
		"Do I have any email from the landlord about the rent?":   SkillMail,
		"Search my mail for the flight confirmation.":             SkillMail,
		"Summarize the news page.":                                SkillWeb,
		"What does the weather page say about Friday?":            SkillWeb,
		"summarize news.lab.test":                                 SkillWeb,
		"Allow access to my Documents folder for one hour.":       SkillGrant,
		"allow reading my mail for 10 minutes":                    SkillGrant,
		"revoke access":                                           SkillRevoke,
		"Make the text bigger.":                                   "",
		"Open the text editor.":                                   "",
		"Why did nginx stop?":                                     "",
		"Arrange the windows side by side.":                       "",
		"Find the contract I signed in March.":                    SkillFiles,
		"Which emails mention the dentist appointment?":           SkillMail,
		"Summarize the page about the new library opening hours.": SkillWeb,
		"Find the receipt for order 4572.":                        SkillFiles,
	}
	for in, want := range cases {
		if got := Classify(in).Skill; got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	r := Classify("Allow access to my Documents folder for 20 seconds")
	if r.Kind != GrantFolder || r.Duration != 20*time.Second || len(r.Targets) != 1 {
		t.Errorf("grant: %+v", r)
	}
	if r := Classify("Open the second result."); r.N != 2 {
		t.Errorf("open: %+v", r)
	}
}

func TestTimeRange(t *testing.T) {
	now := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
	a, b, _ := TimeRange("the PDF the bank sent last month", now)
	if !a.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) || !b.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("last month: %v %v", a, b)
	}
	a, b, _ = TimeRange("the contract I signed in March", now)
	if a.Month() != time.March || a.Year() != 2026 || b.Month() != time.April {
		t.Errorf("march: %v %v", a, b)
	}
	a, _, _ = TimeRange("in november", now)
	if a.Year() != 2025 {
		t.Errorf("november: %v", a)
	}
}

func TestAllowEntry(t *testing.T) {
	if AllowEntry("news.lab.test", 80, 443) != "news.lab.test:80,443 private" {
		t.Error(AllowEntry("news.lab.test", 80, 443))
	}
	if AllowEntry("example.org", 443) != "example.org:443" {
		t.Error(AllowEntry("example.org", 443))
	}
}
