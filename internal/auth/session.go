package auth

import "sync"

// SessionInfo carries the identity and expiry of an authenticated session.
// UserID 0 means the admin (legacy behaviour); any other value is a users.id.
type SessionInfo struct {
	UserID    int64
	ExpiresAt int64
}

// SessionStore encapsulates in-memory session + CSRF state.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]SessionInfo
	csrf     map[string]string
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]SessionInfo),
		csrf:     make(map[string]string),
	}
}

func (s *SessionStore) Get(sessionID string) (SessionInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.sessions[sessionID]
	return v, ok
}

func (s *SessionStore) Set(sessionID string, info SessionInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = info
}

func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *SessionStore) DeleteWithCSRF(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	delete(s.csrf, sessionID)
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
	defer s.mu.RUnlock()
	v, ok := s.csrf[sessionID]
	return v, ok
}

func (s *SessionStore) SetCSRF(sessionID, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.csrf[sessionID] = token
}

func (s *SessionStore) DeleteCSRF(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.csrf, sessionID)
}

// Put is a test seam: directly set session + optional CSRF.
func (s *SessionStore) Put(sessionID string, info SessionInfo) {
	s.Set(sessionID, info)
}

// CleanupExpired removes expired sessions + associated CSRF tokens.
func (s *SessionStore) CleanupExpired(now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if now > v.ExpiresAt {
			delete(s.sessions, k)
			delete(s.csrf, k)
		}
	}
}

func (s *SessionStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = make(map[string]SessionInfo)
	s.csrf = make(map[string]string)
}
