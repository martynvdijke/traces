package auth

import (
	"database/sql"
	"sync"
)

// SessionInfo carries the identity and expiry of an authenticated session.
// UserID 0 means the admin (legacy behaviour); any other value is a users.id.
type SessionInfo struct {
	UserID    int64
	ExpiresAt int64
}

// SessionStore encapsulates session + CSRF state. It keeps a hot in-memory
// cache but, when a database is attached, persists every session so logins
// survive a restart.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]SessionInfo
	csrf     map[string]string
	db       *sql.DB
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]SessionInfo),
		csrf:     make(map[string]string),
	}
}

// SetDB attaches persistent storage. Sessions are written through and lazily
// read back on a cache miss.
func (s *SessionStore) SetDB(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
}

func (s *SessionStore) Get(sessionID string) (SessionInfo, bool) {
	s.mu.RLock()
	v, ok := s.sessions[sessionID]
	db := s.db
	s.mu.RUnlock()
	if ok {
		return v, true
	}
	if db == nil {
		return SessionInfo{}, false
	}
	var info SessionInfo
	var csrf string
	err := db.QueryRow("SELECT user_id, expires_at, COALESCE(csrf, '') FROM sessions WHERE id = ?", sessionID).Scan(&info.UserID, &info.ExpiresAt, &csrf)
	if err != nil {
		return SessionInfo{}, false
	}
	s.mu.Lock()
	s.sessions[sessionID] = info
	s.csrf[sessionID] = csrf
	s.mu.Unlock()
	return info, true
}

func (s *SessionStore) Set(sessionID string, info SessionInfo) {
	s.mu.Lock()
	s.sessions[sessionID] = info
	db := s.db
	csrf := s.csrf[sessionID]
	s.mu.Unlock()
	if db == nil {
		return
	}
	_, _ = db.Exec(`INSERT INTO sessions (id, user_id, expires_at, csrf) VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET user_id=excluded.user_id, expires_at=excluded.expires_at`,
		sessionID, info.UserID, info.ExpiresAt, csrf)
}

func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	delete(s.csrf, sessionID)
	db := s.db
	s.mu.Unlock()
	if db != nil {
		_, _ = db.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	}
}

func (s *SessionStore) DeleteWithCSRF(sessionID string) {
	s.Delete(sessionID)
}

func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

func (s *SessionStore) All() map[string]SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]SessionInfo, len(s.sessions))
	for k, v := range s.sessions {
		cp[k] = v
	}
	return cp
}

func (s *SessionStore) GetCSRF(sessionID string) (string, bool) {
	s.mu.RLock()
	v, ok := s.csrf[sessionID]
	db := s.db
	s.mu.RUnlock()
	if ok {
		return v, true
	}
	if db == nil {
		return "", false
	}
	var csrf string
	err := db.QueryRow("SELECT COALESCE(csrf, '') FROM sessions WHERE id = ?", sessionID).Scan(&csrf)
	if err != nil || csrf == "" {
		return "", false
	}
	s.mu.Lock()
	s.csrf[sessionID] = csrf
	s.mu.Unlock()
	return csrf, true
}

func (s *SessionStore) SetCSRF(sessionID, token string) {
	s.mu.Lock()
	s.csrf[sessionID] = token
	info, exists := s.sessions[sessionID]
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return
	}
	if exists {
		_, _ = db.Exec(`INSERT INTO sessions (id, user_id, expires_at, csrf) VALUES (?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET csrf=excluded.csrf`,
			sessionID, info.UserID, info.ExpiresAt, token)
		return
	}
	// A CSRF token may be requested before the session row was written (for
	// example when upgrading from the in-memory-only store). Keep it in the
	// cache; Set will persist it with the session.
}

func (s *SessionStore) DeleteCSRF(sessionID string) {
	s.mu.Lock()
	delete(s.csrf, sessionID)
	db := s.db
	s.mu.Unlock()
	if db != nil {
		_, _ = db.Exec("UPDATE sessions SET csrf = '' WHERE id = ?", sessionID)
	}
}

// Put is a test seam: directly set session + optional CSRF.
func (s *SessionStore) Put(sessionID string, info SessionInfo) {
	s.Set(sessionID, info)
}

// CleanupExpired removes expired sessions + associated CSRF tokens.
func (s *SessionStore) CleanupExpired(now int64) {
	s.mu.Lock()
	for k, v := range s.sessions {
		if now > v.ExpiresAt {
			delete(s.sessions, k)
			delete(s.csrf, k)
		}
	}
	db := s.db
	s.mu.Unlock()
	if db != nil {
		_, _ = db.Exec("DELETE FROM sessions WHERE expires_at < ?", now)
	}
}

func (s *SessionStore) Clear() {
	s.mu.Lock()
	s.sessions = make(map[string]SessionInfo)
	s.csrf = make(map[string]string)
	db := s.db
	s.mu.Unlock()
	if db != nil {
		_, _ = db.Exec("DELETE FROM sessions")
	}
}
