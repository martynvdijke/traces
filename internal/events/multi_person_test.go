package events

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"traces/internal/models"
)

func newMultiPersonDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE persons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		avatar_url TEXT DEFAULT '',
		bio TEXT DEFAULT '',
		birth_date TEXT DEFAULT '',
		color TEXT DEFAULT '#B5452B',
		created_at TEXT DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE event_people (
		event_id INTEGER NOT NULL,
		person_id INTEGER NOT NULL,
		PRIMARY KEY (event_id, person_id)
	)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Alice", "Bob", "Carol"} {
		if _, err := db.Exec(`INSERT INTO persons (name) VALUES (?)`, name); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestAttachPeople(t *testing.T) {
	db := newMultiPersonDB(t)

	if _, err := db.Exec(`INSERT INTO event_people (event_id, person_id) VALUES (10, 2), (10, 1)`); err != nil {
		t.Fatal(err)
	}

	two := 2
	three := 3
	evs := []models.TimelineEvent{
		{ID: 10, PersonID: &two, Person: &models.Person{ID: 2, Name: "Bob"}},
		{ID: 11, PersonID: &three, Person: &models.Person{ID: 3, Name: "Carol"}},
	}

	AttachPeople(db, evs)

	if len(evs[0].People) != 2 {
		t.Fatalf("event 10 people = %d, want 2", len(evs[0].People))
	}
	// Primary person (Bob, id 2) is moved to the front.
	if got := evs[0].People[0].ID; got != 2 {
		t.Errorf("event 10 People[0].ID = %d, want 2", got)
	}
	if got, want := evs[0].PersonIDs, []int{2, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("event 10 PersonIDs = %v, want %v", got, want)
	}
	if evs[0].Person == nil || evs[0].Person.ID != 2 {
		t.Errorf("event 10 Person = %+v, want id 2", evs[0].Person)
	}
	if evs[0].PersonID == nil || *evs[0].PersonID != 2 {
		t.Errorf("event 10 PersonID = %v, want 2", evs[0].PersonID)
	}

	// No join rows: legacy single person is mirrored into the multi fields.
	if got, want := evs[1].PersonIDs, []int{3}; !reflect.DeepEqual(got, want) {
		t.Errorf("event 11 PersonIDs = %v, want %v", got, want)
	}
	if len(evs[1].People) != 1 || evs[1].People[0].ID != 3 {
		t.Errorf("event 11 People = %+v, want single id 3", evs[1].People)
	}
}

func TestEventPersonIDs(t *testing.T) {
	one, two := 2, 0
	cases := []struct {
		name string
		ev   models.TimelineEvent
		want []int
	}{
		{"prefers PersonIDs and dedupes", models.TimelineEvent{PersonIDs: []int{2, 2, 0, 1}, PersonID: &one}, []int{2, 1}},
		{"falls back to PersonID", models.TimelineEvent{PersonID: &one}, []int{2}},
		{"drops non-positive PersonID", models.TimelineEvent{PersonID: &two}, []int{}},
		{"falls back to People", models.TimelineEvent{People: []models.Person{{ID: 3}, {ID: 3}, {ID: 1}}}, []int{3, 1}},
		{"empty", models.TimelineEvent{}, []int{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EventPersonIDs(c.ev)
			if len(got) != len(c.want) {
				t.Fatalf("EventPersonIDs = %v, want %v", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("EventPersonIDs = %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestBuildEventQueryPersonMembership(t *testing.T) {
	t.Run("person id uses event_people membership", func(t *testing.T) {
		query, args := BuildEventQuery(EventFilters{PersonID: "2"})
		if !strings.Contains(query, "EXISTS (SELECT 1 FROM event_people ep") {
			t.Errorf("query missing event_people membership: %s", query)
		}
		if len(args) != 2 || args[0] != "2" || args[1] != "2" {
			t.Errorf("args = %v, want [\"2\" \"2\"]", args)
		}
	})

	t.Run("person name uses event_people membership", func(t *testing.T) {
		query, args := BuildEventQuery(EventFilters{Person: "Alice"})
		if !strings.Contains(query, "EXISTS (SELECT 1 FROM event_people ep2 JOIN persons p2") {
			t.Errorf("query missing event_people person membership: %s", query)
		}
		if len(args) != 2 || args[0] != "%Alice%" || args[1] != "%Alice%" {
			t.Errorf("args = %v, want [%%Alice%% %%Alice%%]", args)
		}
	})
}
