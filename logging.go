package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// LogEntry represents a single structured log entry stored in the database.
type LogEntry struct {
	ID        int                    `json:"id"`
	Timestamp string                 `json:"timestamp"`
	Severity  string                 `json:"severity"`
	Source    string                 `json:"source"`
	Message   string                 `json:"message"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

var severityOrder = map[string]int{
	"debug": 0,
	"info":  1,
	"warn":  2,
	"error": 3,
}

// LogService provides SQLite-backed structured logging with severity filtering.
type LogService struct {
	db          *sql.DB
	mu          sync.RWMutex
	minSeverity string
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
func (ls *LogService) Log(severity, source, message string, metadata map[string]interface{}) {
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

	// Prune to 10K rows
	ls.db.Exec("DELETE FROM app_logs WHERE id NOT IN (SELECT id FROM app_logs ORDER BY id DESC LIMIT 10000)")
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
//   - severity: minimum severity level (debug/info/warn/error)
//   - source: exact source name match
//   - q: text search in message (LIKE)
//   - limit: max results (default 50, max 200)
//   - offset: pagination offset
//   - since: ISO 8601 timestamp, only entries after this time
func (ls *LogService) Query(severity, source, q string, limit, offset int, since string) ([]LogEntry, error) {
	query := "SELECT id, timestamp, severity, source, message, COALESCE(metadata,'') FROM app_logs WHERE 1=1"
	args := []interface{}{}

	if severity != "" {
		// Filter by minimum severity level
		minOrd := severityOrder[severity]
		// Build list of severities at or above the minimum
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
			query += " AND severity IN (" + joinStrings(placeholders, ",") + ")"
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
	return severityOrder
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// --- API Handlers ---

// handleGetLogs returns log entries with optional filtering and pagination.
func handleGetLogs(c *gin.Context) {
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

	entries, err := logService.Query(severity, source, q, limit, offset, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query logs"})
		return
	}
	c.JSON(http.StatusOK, entries)
}

// handleGetLogCount returns the total number of log entries.
func handleGetLogCount(c *gin.Context) {
	count, err := logService.Count()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": count})
}

// handleClearLogs deletes all log entries.
func handleClearLogs(c *gin.Context) {
	if err := logService.Clear(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// handleGetLogSettings returns the current log settings (min_severity).
func handleGetLogSettings(c *gin.Context) {
	sev := logService.GetMinSeverity()
	c.JSON(http.StatusOK, gin.H{"min_severity": sev})
}

// handleUpdateLogSettings updates the minimum severity level.
func handleUpdateLogSettings(c *gin.Context) {
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
	logService.SetMinSeverity(input.MinSeverity)
	logService.Log("info", "system", "Log verbosity changed to "+input.MinSeverity, nil)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// handleGetLogSources returns a list of distinct source names from logs.
func handleGetLogSources(c *gin.Context) {
	sources, err := logService.GetDistinctSources()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get sources"})
		return
	}
	c.JSON(http.StatusOK, sources)
}
