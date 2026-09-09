package parse

import (
	"testing"
	"time"
)

var ref = time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC) // a Wednesday

func TestParseDate(t *testing.T) {
	cases := map[string]string{
		"today":      "2026-09-02",
		"tomorrow":   "2026-09-03",
		"fri":        "2026-09-04",
		"mon":        "2026-09-07",
		"in3d":       "2026-09-05",
		"2026-12-25": "2026-12-25",
		"9/10":       "2026-09-10",
		"sep 10":     "2026-09-10",
		"10 sep":     "2026-09-10",
		"jan 5":      "2027-01-05", // already past this year -> next year
	}
	for in, want := range cases {
		got, ok := ParseDate(in, ref)
		if !ok || got != want {
			t.Errorf("ParseDate(%q) = %q,%v; want %q", in, got, ok, want)
		}
	}
	if _, ok := ParseDate("banana", ref); ok {
		t.Error("ParseDate(banana) should fail")
	}
}

func TestDumpBlocksAndSubtasks(t *testing.T) {
	in := `call the dentist tomorrow !high

buy hiking boots @travel

Trip planning
still nothing booked yet
- cabin near the lake
- [[Book the cabin]] !high

random musing about the weather and how it never cooperates on weekends #life`

	items := Dump(in, ref)
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4: %+v", len(items), items)
	}

	if !items[0].IsTask || items[0].Priority != 3 || items[0].Due != "2026-09-03" {
		t.Errorf("dentist item wrong: %+v", items[0])
	}
	if items[1].Project != "travel" || !items[1].IsTask {
		t.Errorf("boots item wrong: %+v", items[1])
	}

	trip := items[2]
	if trip.Title != "Trip planning" || trip.Body != "still nothing booked yet" {
		t.Errorf("trip planning head wrong: %+v", trip)
	}
	if !trip.IsTask {
		t.Errorf("an item with subtasks should be a task: %+v", trip)
	}
	if len(trip.Sub) != 2 {
		t.Fatalf("want 2 subtasks, got %d: %+v", len(trip.Sub), trip.Sub)
	}
	if trip.Sub[0].Title != "cabin near the lake" {
		t.Errorf("subtask 0 wrong: %+v", trip.Sub[0])
	}
	if trip.Sub[1].Priority != 3 || len(trip.Sub[1].Links) != 1 || trip.Sub[1].Links[0] != "Book the cabin" {
		t.Errorf("subtask 1 should keep its link and priority: %+v", trip.Sub[1])
	}

	last := items[3]
	if last.IsTask || len(last.Tags) != 1 || last.Tags[0] != "life" {
		t.Errorf("musing should be a tagged note: %+v", last)
	}
}

func TestDumpNonDashLineIsBody(t *testing.T) {
	items := Dump("Plan the party\n- send invites\ndecide on a date first\n- book the room", ref)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d: %+v", len(items), items)
	}
	if items[0].Body != "decide on a date first" {
		t.Errorf("non-dash line should be the head's body: %q", items[0].Body)
	}
	if len(items[0].Sub) != 2 {
		t.Errorf("want 2 subtasks: %+v", items[0].Sub)
	}
}

func TestDumpConsecutiveLinesAreOneNote(t *testing.T) {
	items := Dump("Meeting notes\nwe talked about the roadmap\nnext steps unclear", ref)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d: %+v", len(items), items)
	}
	if items[0].Title != "Meeting notes" || items[0].IsTask {
		t.Errorf("should be one note: %+v", items[0])
	}
	if items[0].Body != "we talked about the roadmap\nnext steps unclear" {
		t.Errorf("body should keep both continuation lines: %q", items[0].Body)
	}
}

func TestDumpBlankLineSeparates(t *testing.T) {
	items := Dump("first thought\n\nsecond thought", ref)
	if len(items) != 2 {
		t.Fatalf("blank line should split: got %d: %+v", len(items), items)
	}
}

func TestDumpCheckbox(t *testing.T) {
	items := Dump("- [x] mow the lawn\n- [ ] water plants", ref)
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if !items[0].Done || !items[0].IsTask {
		t.Errorf("checked item should be a done task: %+v", items[0])
	}
	if items[1].Done || !items[1].IsTask {
		t.Errorf("unchecked item should be an open task: %+v", items[1])
	}
}

func TestRenderHTMLEscapesAndLinks(t *testing.T) {
	resolver := func(target string) (string, bool) {
		return "/e/" + target, target == "Known"
	}
	out := RenderHTML("<script>alert(1)</script> see [[Known]] and [[Missing]] #tag", resolver)
	if contains(out, "<script>") {
		t.Errorf("script tag not escaped: %s", out)
	}
	if !contains(out, `class="wikilink"`) || !contains(out, `class="wikilink new"`) {
		t.Errorf("wiki links not rendered: %s", out)
	}
	if !contains(out, `href="/tags/tag"`) {
		t.Errorf("hashtag not linked: %s", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
