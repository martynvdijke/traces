package logging

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// LogEntry represents a single structured log entry stored in the database.
type LogEntry struct {
	ID        int            `json:"id"`
	Timestamp string         `json:"timestamp"`
	Severity  string         `json:"severity"`
	Source    string         `json:"source"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

var severityOrder = map[string]int{
	"debug": 0,
	"info":  1,
	"warn":  2,
	"error": 3,
}

// LogService provides SQLite-backed structured logging with severity filtering.
type LogService struct {
	db              *sql.DB
	mu              sync.RWMutex
	minSeverity     string
	otelLogsEnabled func() bool
}

// New creates a new LogService backed by db. otelLogsEnabled is called live
// on each Log call to decide whether to mirror into the OTel log pipeline.
func New(db *sql.DB, otelLogsEnabled func() bool) *LogService {
	return &LogService{db: db, otelLogsEnabled: otelLogsEnabled}
}

// Init ensures the log_settings row exists and loads the current min_severity.
func (ls *LogService) Init() error {
	var count int
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.db.QueryRow("SELECT COUNT(*) FROM log_settings").Scan(&count)
	if count == 0 {
		_, err := ls.db.Exec("INSERT INTO log_settings (id, min_severity) VALUES (1, 'warn')")
		if err != nil {
			return err
		}
	}
	var sev string
	ls.db.QueryRow("SELECT min_severity FROM log_settings WHERE id=1").Scan(&sev)
	if sev == "" {
		sev = "warn"
	}
	ls.minSeverity = sev
	return nil
}

// Log inserts a new log entry if its severity meets the configured threshold,
// then prunes the table to at most 10,000 rows.
func (ls *LogService) Log(severity, source, message string, metadata map[string]any) {
	ls.mu.RLock()
	minSev := ls.minSeverity
	ls.mu.RUnlock()

	if severityOrder[severity] < severityOrder[minSev] {
		return
	}

	var metaJSON string
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err == nil {
			metaJSON = string(b)
		}
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	_, err := ls.db.Exec(
		"INSERT INTO app_logs (timestamp, severity, source, message, metadata) VALUES (?, ?, ?, ?, ?)",
		timestamp, severity, source, message, metaJSON,
	)
	if err != nil {
		log.Printf("[LogService] Failed to insert log: %v", err)
		return
	}

	// Mirror the entry into the OTel log pipeline. Without this the slog bridge
	// configured in initTelemetry has no producers and the OTLP logs exporter
	// stays empty. Gated on the OTel logs setting to avoid noise when disabled.
	if ls.otelLogsEnabled != nil && ls.otelLogsEnabled() {
		slog.Default().Log(context.Background(), severityToSlog(severity), message,
			"source", source, "metadata", metaJSON)
	}

	// Prune to 10K rows
	ls.db.Exec("DELETE FROM app_logs WHERE id NOT IN (SELECT id FROM app_logs ORDER BY id DESC LIMIT 10000)")
}

// severityToSlog maps the app's severity strings to slog levels.
func severityToSlog(severity string) slog.Level {
	switch severity {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// SetMinSeverity updates the minimum severity threshold and persists it.
func (ls *LogService) SetMinSeverity(severity string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.minSeverity = severity
	ls.db.Exec("UPDATE log_settings SET min_severity=? WHERE id=1", severity)
}

// GetMinSeverity returns the current minimum severity threshold.
func (ls *LogService) GetMinSeverity() string {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return ls.minSeverity
}

// Query returns log entries matching the given filters, ordered by id DESC.
func (ls *LogService) Query(severity, source, q string, limit, offset int, since string) ([]LogEntry, error) {
	query := "SELECT id, timestamp, severity, source, message, COALESCE(metadata,'') FROM app_logs WHERE 1=1"
	args := []any{}

	if severity != "" {
		minOrd := severityOrder[severity]
		var sevs []string
		for s, ord := range severityOrder {
			if ord >= minOrd {
				sevs = append(sevs, s)
			}
		}
		if len(sevs) > 0 {
			placeholders := make([]string, len(sevs))
			for i, s := range sevs {
				placeholders[i] = "?"
				args = append(args, s)
			}
			query += " AND severity IN (" + strings.Join(placeholders, ",") + ")"
		}
	}
	if source != "" {
		query += " AND source = ?"
		args = append(args, source)
	}
	if q != "" {
		query += " AND message LIKE ?"
		args = append(args, "%"+q+"%")
	}
	if since != "" {
		query += " AND timestamp >= ?"
		args = append(args, since)
	}

	query += " ORDER BY id DESC"

	if limit <= 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}
	query += " LIMIT ?"
	args = append(args, limit)

	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}

	rows, err := ls.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var metaStr string
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Severity, &e.Source, &e.Message, &metaStr); err != nil {
			continue
		}
		if metaStr != "" {
			json.Unmarshal([]byte(metaStr), &e.Metadata)
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []LogEntry{}
	}
	return entries, nil
}

// Count returns the total number of log entries.
func (ls *LogService) Count() (int, error) {
	var count int
	err := ls.db.QueryRow("SELECT COUNT(*) FROM app_logs").Scan(&count)
	return count, err
}

// Clear deletes all log entries from the database.
func (ls *LogService) Clear() error {
	_, err := ls.db.Exec("DELETE FROM app_logs")
	return err
}

// GetDistinctSources returns a list of unique source names in the logs.
func (ls *LogService) GetDistinctSources() ([]string, error) {
	rows, err := ls.db.Query("SELECT DISTINCT source FROM app_logs ORDER BY source")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			sources = append(sources, s)
		}
	}
	if sources == nil {
		sources = []string{}
	}
	return sources, nil
}

// GetLogSeverityOrder returns the severity order map (for API reference).
func GetLogSeverityOrder() map[string]int {
	cp := make(map[string]int, len(severityOrder))
	for k, v := range severityOrder {
		cp[k] = v
	}
	return cp
}

// --- API Handlers ---

// HandleGetLogs returns log entries with optional filtering and pagination.
func (ls *LogService) HandleGetLogs(c *gin.Context) {
	severity := c.Query("severity")
	source := c.Query("source")
	q := c.Query("q")
	since := c.Query("since")

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
		limit = l
	}

	offset := 0
	if o, err := strconv.Atoi(c.Query("offset")); err == nil && o >= 0 {
		offset = o
	}

	entries, err := ls.Query(severity, source, q, limit, offset, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query logs"})
		return
	}
	c.JSON(http.StatusOK, entries)
}

// HandleGetLogCount returns the total number of log entries.
func (ls *LogService) HandleGetLogCount(c *gin.Context) {
	count, err := ls.Count()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": count})
}

// HandleClearLogs deletes all log entries.
func (ls *LogService) HandleClearLogs(c *gin.Context) {
	if err := ls.Clear(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleGetLogSettings returns the current log settings (min_severity).
func (ls *LogService) HandleGetLogSettings(c *gin.Context) {
	sev := ls.GetMinSeverity()
	c.JSON(http.StatusOK, gin.H{"min_severity": sev})
}

// HandleUpdateLogSettings updates the minimum severity level.
func (ls *LogService) HandleUpdateLogSettings(c *gin.Context) {
	var input struct {
		MinSeverity string `json:"min_severity"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, ok := severityOrder[input.MinSeverity]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid severity level"})
		return
	}
	ls.SetMinSeverity(input.MinSeverity)
	ls.Log("info", "system", "Log verbosity changed to "+input.MinSeverity, nil)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleGetLogSources returns a list of distinct source names from logs.
func (ls *LogService) HandleGetLogSources(c *gin.Context) {
	sources, err := ls.GetDistinctSources()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get sources"})
		return
	}
	c.JSON(http.StatusOK, sources)
}
