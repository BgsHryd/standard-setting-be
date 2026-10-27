// Package testutil contains in-memory fixtures used only by test packages.
package testutil

import (
	"context"
	"standard-setting/backend/internal/session"
	"sync"
	"time"
)

type BrowserSessions struct {
	mu       sync.Mutex
	records  map[string]session.BrowserSession
	active   map[string]string
	versions map[string]int
	watchers map[string]map[chan struct{}]bool
	Err      error
}

func (s *BrowserSessions) CreateBrowserSession(_ context.Context, rec session.BrowserSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	if s.records == nil {
		s.records = map[string]session.BrowserSession{}
		s.active = map[string]string{}
		s.versions = map[string]int{}
	}
	if (rec.ExpectedID != "" && s.active[rec.UserID] != rec.ExpectedID) || rec.AuthVersion < s.versions[rec.UserID] {
		return session.ErrBrowserSessionReplaced
	}
	s.versions[rec.UserID] = rec.AuthVersion
	if old, ok := s.records[s.active[rec.UserID]]; ok {
		old.Reason = "replaced"
		s.records[old.ID] = old
	}
	s.records[rec.ID] = rec
	s.active[rec.UserID] = rec.ID
	s.notify(rec.UserID)
	return nil
}
func (s *BrowserSessions) GetBrowserSession(_ context.Context, id string) (session.BrowserSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return session.BrowserSession{}, s.Err
	}
	rec, ok := s.records[id]
	if !ok || rec.ExpiresAt <= time.Now().UnixMilli() {
		return rec, session.ErrBrowserSessionInvalid
	}
	if rec.Reason == "replaced" {
		return rec, session.ErrBrowserSessionReplaced
	}
	if rec.Reason != "" {
		return rec, session.ErrBrowserSessionInvalid
	}
	if s.active[rec.UserID] != id {
		return rec, session.ErrBrowserSessionReplaced
	}
	return rec, nil
}
func (s *BrowserSessions) RevokeBrowserSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	rec, ok := s.records[id]
	if ok && s.active[rec.UserID] == id {
		delete(s.active, rec.UserID)
		rec.Reason = "logout"
		s.records[id] = rec
		s.notify(rec.UserID)
	}
	return nil
}
func (s *BrowserSessions) notify(user string) {
	for ch := range s.watchers[user] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (s *BrowserSessions) SubscribeBrowserSessions(_ context.Context, user string) (<-chan struct{}, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, nil, s.Err
	}
	if s.watchers == nil {
		s.watchers = map[string]map[chan struct{}]bool{}
	}
	if s.watchers[user] == nil {
		s.watchers[user] = map[chan struct{}]bool{}
	}
	ch := make(chan struct{}, 1)
	s.watchers[user][ch] = true
	var once sync.Once
	return ch, func() { once.Do(func() { s.mu.Lock(); defer s.mu.Unlock(); delete(s.watchers[user], ch); close(ch) }) }, nil
}
