package channels

import (
	"fmt"
	"sync"
)

type SessionMap[K comparable] struct {
	mu      sync.RWMutex
	forward map[K]string
	reverse map[string]K
	prefix  string
	toKey   func(K) string
}

func NewSessionMap[K comparable](prefix string, toKey func(K) string) *SessionMap[K] {
	return &SessionMap[K]{
		forward: make(map[K]string),
		reverse: make(map[string]K),
		prefix:  prefix,
		toKey:   toKey,
	}
}

func (sm *SessionMap[K]) GetOrCreate(channelID K) string {
	sm.mu.RLock()
	sid, ok := sm.forward[channelID]
	sm.mu.RUnlock()

	if ok {
		return sid
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sid, ok := sm.forward[channelID]; ok {
		return sid
	}

	sid = fmt.Sprintf("%s-%s", sm.prefix, sm.toKey(channelID))
	sm.forward[channelID] = sid
	sm.reverse[sid] = channelID
	return sid
}

func (sm *SessionMap[K]) Lookup(channelID K) (string, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	sid, ok := sm.forward[channelID]
	return sid, ok
}

func (sm *SessionMap[K]) Reverse(sessionID string) (K, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	cid, ok := sm.reverse[sessionID]
	return cid, ok
}
