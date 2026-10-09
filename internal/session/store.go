package session

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

type Store struct {
	mu   sync.Mutex
	ttl  time.Duration
	data map[string]time.Time
}

func NewStore(ttl time.Duration) *Store {
	s := &Store{ttl: ttl, data: map[string]time.Time{}}
	go func() {
		for range time.Tick(time.Hour) {
			s.gc()
		}
	}()
	return s
}

func (s *Store) Check(password, candidate string) bool {
	a := []byte(password)
	b := []byte(candidate)
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

func (s *Store) Create() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("rand: " + err.Error())
	}
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.data[tok] = time.Now().Add(s.ttl)
	s.mu.Unlock()
	return tok
}

func (s *Store) Valid(tok string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.data[tok]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.data, tok)
		return false
	}
	return true
}

func (s *Store) Revoke(tok string) {
	s.mu.Lock()
	delete(s.data, tok)
	s.mu.Unlock()
}

func (s *Store) gc() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.data {
		if now.After(exp) {
			delete(s.data, k)
		}
	}
}
