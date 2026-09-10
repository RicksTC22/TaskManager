package store

import "time"

// SeedDemo creates a demo account with a handful of entries the first time the
// database is opened. It returns true if it created the account.
func SeedDemo(db *DB) (bool, error) {
	n, err := db.UserCount()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}

	u, err := db.CreateUser("demo@cairn.local", "Demo", "demodemo")
	if err != nil {
		return false, err
	}

	today := time.Now().Format("2006-01-02")
	soon := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	past := time.Now().AddDate(0, 0, -2).Format("2006-01-02")

	entries := []EntryInput{
		{
			Title:  "Welcome to Cairn",
			Status: "",
			Body: `Cairn is a **notebook and a task board** in one. Every item here is an *entry*.

Give an entry a status and it shows up on the board. Leave the status off and it is just a note, like this one.

- Link entries with double brackets: [[Trip planning]]
- Tag anything with #hashtags
- Use the Idea dump to capture fast and sort later

Switch between **minimal** and **dense** display with the toggle in the header — minimal for writing, dense for working through tasks.`,
		},
		{
			Title:    "Draft the project brief",
			Status:   "doing",
			Priority: 3,
			Due:      soon,
			Project:  "Website relaunch",
			Body:     "Two pages max. Cover goals, audience, and the #content model. Link back to [[Welcome to Cairn]] for the pitch.",
		},
		{
			Title:    "Send kickoff invite",
			Status:   "next",
			Priority: 2,
			Due:      today,
			Project:  "Website relaunch",
			Body:     "Thursday 10am. #meeting",
		},
		{
			Title:   "Audit current analytics",
			Status:  "inbox",
			Project: "Website relaunch",
			Body:    "What are we actually tracking? #content",
		},
		{
			Title:    "Renew domain",
			Status:   "inbox",
			Priority: 1,
			Due:      past,
			Body:     "Overdue — the card on file expired. #admin",
		},
		{
			Title:  "Trip planning",
			Status: "",
			Body: `Ideas for the autumn trip. Nothing booked yet.

- Cabin near the lake for 3 nights
- Day hike to the ridge if the weather holds
- Check whether [[Renew domain]] clears before we leave (unrelated, but it has been nagging)`,
		},
		{
			Title:  "Book the cabin",
			Status: "next",
			Due:    soon,
			Body:   "For [[Trip planning]]. Call, do not email — they are slow. #travel",
		},
		{
			Title:  "Pick up boots from repair",
			Status: "done",
			Body:   "#travel",
		},
	}

	var briefID int64
	for _, in := range entries {
		e, err := db.CreateEntry(u.ID, in)
		if err != nil {
			return false, err
		}
		if in.Title == "Draft the project brief" {
			briefID = e.ID
		}
	}

	// A couple of subtasks under the brief.
	for _, sub := range []EntryInput{
		{Title: "Outline the three goals", Status: "done", ParentID: briefID},
		{Title: "Get sign-off from Sam", Status: "next", ParentID: briefID, Due: soon},
	} {
		if _, err := db.CreateEntry(u.ID, sub); err != nil {
			return false, err
		}
	}

	// Default calendars, plus a few demo events.
	if err := db.EnsureDefaultCalendars(u.ID); err != nil {
		return false, err
	}
	cals, err := db.Calendars(u.ID)
	if err != nil {
		return false, err
	}
	var personal, work *Calendar
	for i := range cals {
		if cals[i].Slug == "personal" {
			c := cals[i]
			personal = &c
		}
	}
	if w, err := db.CreateCalendar(u.ID, "Work", "#417a86", "events", ""); err == nil {
		work = w
	}

	at := func(dayOffset, hour, min int) time.Time {
		n := time.Now()
		return time.Date(n.Year(), n.Month(), n.Day(), hour, min, 0, 0, time.Local).AddDate(0, 0, dayOffset)
	}
	if personal != nil {
		db.CreateEvent(u.ID, EventInput{CalendarID: personal.ID, Title: "Morning run", Location: "River loop",
			Start: at(1, 7, 0), End: at(1, 8, 0), RRule: "FREQ=WEEKLY;BYDAY=MO,WE,FR"})
		db.CreateEvent(u.ID, EventInput{CalendarID: personal.ID, Title: "Dinner with Jordan",
			Start: at(2, 19, 0), End: at(2, 21, 0)})
	}
	if work != nil {
		db.CreateEvent(u.ID, EventInput{CalendarID: work.ID, Title: "Website kickoff", Location: "Room 2 / call",
			Start: at(0, 10, 0), End: at(0, 11, 0)})
		db.CreateEvent(u.ID, EventInput{CalendarID: work.ID, Title: "Team standup",
			Start: at(1, 9, 30), End: at(1, 9, 45), RRule: "FREQ=DAILY;COUNT=10"})
		db.CreateEvent(u.ID, EventInput{CalendarID: work.ID, Title: "Design review",
			Start: at(4, 14, 0), End: at(4, 15, 0)})
	}

	return true, nil
}
