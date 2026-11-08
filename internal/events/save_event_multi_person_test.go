package events

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/auth"
	"traces/internal/database"
	"traces/internal/models"
)

func newSaveEventHarness(t *testing.T) (*Service, *sql.DB, []int) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ids := make([]int, 0, 2)
	for _, name := range []string{"Alice", "Bob"} {
		res, err := db.Exec(`INSERT INTO persons (name, color) VALUES (?, ?)`, name, "#B5452B")
		if err != nil {
			t.Fatalf("insert person %s: %v", name, err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, int(id))
	}

	return New(Deps{DB: db}), db, ids
}

func postSaveEvent(t *testing.T, svc *Service, body string) (int, models.TimelineEvent) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(auth.CtxKeyUserID, int64(1))
	c.Set(auth.CtxKeyUserName, "tester")
	c.Set(auth.CtxKeyUserIsAdmin, true)

	svc.SaveEvent(c)

	var got models.TimelineEvent
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
	}
	return rec.Code, got
}

func eventPeopleCount(t *testing.T, db *sql.DB, eventID int) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_people WHERE event_id = ?`, eventID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSaveEventWritesMultiplePeople(t *testing.T) {
	svc, db, ids := newSaveEventHarness(t)

	code, ev := postSaveEvent(t, svc, fmt.Sprintf(
		`{"title":"Trip","date":"2026-01-02","person_ids":[%d,%d]}`, ids[0], ids[1]))
	if code != http.StatusOK {
		t.Fatalf("create status = %d", code)
	}
	if ev.ID == 0 {
		t.Fatal("created event has no id")
	}
	if want := []int{ids[0], ids[1]}; !reflect.DeepEqual(ev.PersonIDs, want) {
		t.Errorf("create PersonIDs = %v, want %v", ev.PersonIDs, want)
	}
	if len(ev.People) != 2 || ev.People[0].ID != ids[0] {
		t.Errorf("create People = %+v, want primary first", ev.People)
	}
	if ev.PersonID == nil || *ev.PersonID != ids[0] {
		t.Errorf("create PersonID = %v, want %d", ev.PersonID, ids[0])
	}
	if got := eventPeopleCount(t, db, ev.ID); got != 2 {
		t.Errorf("create event_people rows = %d, want 2", got)
	}

	// Replace the association set: only Bob should remain, and he becomes primary.
	code, ev = postSaveEvent(t, svc, fmt.Sprintf(
		`{"id":%d,"title":"Trip edited","date":"2026-01-02","person_ids":[%d]}`, ev.ID, ids[1]))
	if code != http.StatusOK {
		t.Fatalf("update status = %d", code)
	}
	if want := []int{ids[1]}; !reflect.DeepEqual(ev.PersonIDs, want) {
		t.Errorf("update PersonIDs = %v, want %v", ev.PersonIDs, want)
	}
	if ev.PersonID == nil || *ev.PersonID != ids[1] {
		t.Errorf("update PersonID = %v, want %d", ev.PersonID, ids[1])
	}
	if got := eventPeopleCount(t, db, ev.ID); got != 1 {
		t.Errorf("update event_people rows = %d, want 1", got)
	}

	// Explicit empty list clears all associations.
	code, ev = postSaveEvent(t, svc, fmt.Sprintf(
		`{"id":%d,"title":"Trip alone","date":"2026-01-02","person_ids":[]}`, ev.ID))
	if code != http.StatusOK {
		t.Fatalf("clear status = %d", code)
	}
	if len(ev.PersonIDs) != 0 || ev.PersonID != nil {
		t.Errorf("clear PersonIDs = %v PersonID = %v, want none", ev.PersonIDs, ev.PersonID)
	}
	if got := eventPeopleCount(t, db, ev.ID); got != 0 {
		t.Errorf("clear event_people rows = %d, want 0", got)
	}
}
