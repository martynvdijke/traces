package events

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"traces/internal/auth"
	"traces/internal/models"
)

func newCapsuleTestService(t *testing.T) (*sql.DB, *Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	stmts := []string{
		`CREATE TABLE timeline_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT,
			description TEXT,
			event_date TEXT,
			location TEXT,
			media_type TEXT,
			media_url TEXT,
			thumbnail TEXT,
			media_caption TEXT,
			tags TEXT,
			sort_order INTEGER DEFAULT 0,
			is_public INTEGER DEFAULT 0,
			is_favorite INTEGER DEFAULT 0,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			person_id INTEGER,
			latitude REAL,
			longitude REAL,
			recurring TEXT,
			weather_data TEXT,
			event_start_time TEXT,
			event_end_time TEXT,
			user_id INTEGER,
			deleted_at TEXT DEFAULT '',
			reveal_at TEXT DEFAULT '',
			revealed_at TEXT DEFAULT ''
		)`,
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			display_name TEXT,
			email TEXT DEFAULT ''
		)`,
		`CREATE TABLE email_settings (
			id INTEGER PRIMARY KEY,
			smtp_host TEXT DEFAULT '',
			smtp_port INTEGER DEFAULT 587,
			smtp_user TEXT DEFAULT '',
			smtp_pass TEXT DEFAULT '',
			from_addr TEXT DEFAULT '',
			to_addr TEXT DEFAULT ''
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}

	return db, &Service{Deps: Deps{DB: db}}
}

func insertCapsuleEvent(t *testing.T, db *sql.DB, title string, userID int, revealAt string, opts ...func(*[]any, *[]string)) int {
	t.Helper()

	cols := []string{"title", "description", "event_date", "location", "media_type", "media_url", "tags", "user_id", "reveal_at", "revealed_at", "deleted_at"}
	vals := []any{title, "secret body of " + title, "2027-01-01", "Secret Location", "photo", "https://example.test/secret.jpg", "secret-tag", userID, revealAt, "", ""}
	for _, opt := range opts {
		opt(&vals, &cols)
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
	res, err := db.Exec(`INSERT INTO timeline_events (`+strings.Join(cols, ",")+`) VALUES (`+placeholders+`)`, vals...)
	if err != nil {
		t.Fatalf("insert %s: %v", title, err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func futureReveal(t *testing.T) string {
	t.Helper()
	return models.FormatRevealAt(time.Now().AddDate(0, 0, 30))
}

func capsuleContext(userID int64, admin bool) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/capsules", nil)
	c.Set(auth.CtxKeyUserID, userID)
	c.Set(auth.CtxKeyUserName, "user")
	c.Set(auth.CtxKeyUserColor, models.DefaultColor)
	c.Set(auth.CtxKeyUserIsAdmin, admin)
	return c, w
}

func TestGetCapsulesListsOnlySealedAndWithholdsContent(t *testing.T) {
	db, svc := newCapsuleTestService(t)

	sealedID := insertCapsuleEvent(t, db, "Letter to 2027", 1, futureReveal(t))
	insertCapsuleEvent(t, db, "Already open", 1, models.FormatRevealAt(time.Now().AddDate(0, 0, -1)))
	insertCapsuleEvent(t, db, "Plain event", 1, "")
	deletedID := insertCapsuleEvent(t, db, "Deleted capsule", 1, futureReveal(t), func(vals *[]any, cols *[]string) {
		for i, col := range *cols {
			if col == "deleted_at" {
				(*vals)[i] = "2027-01-02T00:00:00Z"
			}
		}
	})

	c, w := capsuleContext(1, false)
	svc.GetCapsules(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Capsules []CapsuleSummary `json:"capsules"`
		Total    int              `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}

	if resp.Total != 1 || len(resp.Capsules) != 1 {
		t.Fatalf("got %d capsules (total %d), want only the sealed one: %s", len(resp.Capsules), resp.Total, w.Body.String())
	}
	got := resp.Capsules[0]
	if got.ID != sealedID || got.Title != "Letter to 2027" {
		t.Fatalf("unexpected capsule %+v", got)
	}
	if got.SecondsUntilReveal <= 0 {
		t.Fatalf("seconds_until_reveal = %d, want positive", got.SecondsUntilReveal)
	}
	if got.MediaType != "photo" {
		t.Fatalf("media_type = %q, want photo", got.MediaType)
	}

	body := w.Body.String()
	for _, leak := range []string{"secret body", "Secret Location", "secret.jpg", "secret-tag"} {
		if strings.Contains(body, leak) {
			t.Fatalf("capsule listing leaked %q: %s", leak, body)
		}
	}
	if strings.Contains(body, "Deleted capsule") {
		t.Fatalf("capsule listing leaked a deleted capsule: %s", body)
	}
	_ = deletedID
}

func TestGetCapsulesScopesToOwnerAndAdmin(t *testing.T) {
	db, svc := newCapsuleTestService(t)
	insertCapsuleEvent(t, db, "Mine", 1, futureReveal(t))
	insertCapsuleEvent(t, db, "Theirs", 2, futureReveal(t))

	c, w := capsuleContext(1, false)
	svc.GetCapsules(c)
	if !strings.Contains(w.Body.String(), "Mine") || strings.Contains(w.Body.String(), "Theirs") {
		t.Fatalf("family member must only see their own capsules: %s", w.Body.String())
	}

	cAdmin, wAdmin := capsuleContext(9, true)
	svc.GetCapsules(cAdmin)
	if !strings.Contains(wAdmin.Body.String(), "Mine") || !strings.Contains(wAdmin.Body.String(), "Theirs") {
		t.Fatalf("admin must see every capsule: %s", wAdmin.Body.String())
	}
}

func TestUnsealCapsule(t *testing.T) {
	db, svc := newCapsuleTestService(t)
	sealedID := insertCapsuleEvent(t, db, "Open me", 1, futureReveal(t))
	otherID := insertCapsuleEvent(t, db, "Not mine", 2, futureReveal(t))
	plainID := insertCapsuleEvent(t, db, "Plain", 1, "")

	post := func(userID int64, admin bool, id int) (*httptest.ResponseRecorder, *gin.Context) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body, _ := json.Marshal(map[string]int{"id": id})
		c.Request = httptest.NewRequest(http.MethodPost, "/api/capsules/unseal", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(auth.CtxKeyUserID, userID)
		c.Set(auth.CtxKeyUserIsAdmin, admin)
		return w, c
	}

	// Another family member may not open it.
	w, c := post(2, false, sealedID)
	svc.UnsealCapsule(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-owner status = %d, want 403 (%s)", w.Code, w.Body.String())
	}

	// A plain event is not a capsule.
	w, c = post(1, false, plainID)
	svc.UnsealCapsule(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("plain event status = %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// Missing event.
	w, c = post(1, false, 99999)
	svc.UnsealCapsule(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing event status = %d, want 404 (%s)", w.Code, w.Body.String())
	}

	// Owner opens their own capsule.
	w, c = post(1, false, sealedID)
	svc.UnsealCapsule(c)
	if w.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200 (%s)", w.Code, w.Body.String())
	}

	var revealAt, revealedAt string
	if err := db.QueryRow(`SELECT COALESCE(reveal_at,''), COALESCE(revealed_at,'') FROM timeline_events WHERE id = ?`, sealedID).Scan(&revealAt, &revealedAt); err != nil {
		t.Fatal(err)
	}
	if revealAt != "" {
		t.Fatalf("reveal_at = %q, want cleared", revealAt)
	}
	if revealedAt == "" {
		t.Fatal("revealed_at must be stamped on unseal")
	}

	// Admin may open someone else's sealed capsule.
	w, c = post(9, true, otherID)
	svc.UnsealCapsule(c)
	if w.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
}

func TestRevealDueCapsulesClaimsEachCapsuleOnce(t *testing.T) {
	db, svc := newCapsuleTestService(t)

	dueID := insertCapsuleEvent(t, db, "Due", 1, models.FormatRevealAt(time.Now().Add(-2*time.Minute)))
	futureID := insertCapsuleEvent(t, db, "Later", 1, futureReveal(t))
	openID := insertCapsuleEvent(t, db, "Already revealed", 1, models.FormatRevealAt(time.Now().Add(-time.Hour)), func(vals *[]any, cols *[]string) {
		for i, col := range *cols {
			if col == "revealed_at" {
				(*vals)[i] = models.FormatRevealAt(time.Now().Add(-time.Hour))
			}
		}
	})

	opened, err := svc.RevealDueCapsules()
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if opened != 1 {
		t.Fatalf("first sweep opened %d, want 1", opened)
	}

	again, err := svc.RevealDueCapsules()
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if again != 0 {
		t.Fatalf("second sweep opened %d, want 0 (must be at most once)", again)
	}

	var revealedAt string
	db.QueryRow(`SELECT COALESCE(revealed_at,'') FROM timeline_events WHERE id = ?`, dueID).Scan(&revealedAt)
	if revealedAt == "" {
		t.Fatal("due capsule must be stamped as revealed")
	}

	db.QueryRow(`SELECT COALESCE(revealed_at,'') FROM timeline_events WHERE id = ?`, futureID).Scan(&revealedAt)
	if revealedAt != "" {
		t.Fatal("future capsule must stay sealed")
	}

	// The already-revealed row keeps its original stamp.
	db.QueryRow(`SELECT COALESCE(revealed_at,'') FROM timeline_events WHERE id = ?`, openID).Scan(&revealedAt)
	if revealedAt == "" {
		t.Fatal("previously revealed capsule must keep its revealed_at")
	}
}
