package chunkdiscoverer

import (
	"container/list"
	"crypto/sha256"
	"sync"
)

// boundedSet keeps hot deduplication keys without retaining every historical URL.
type boundedSet struct {
	mu       sync.Mutex
	capacity int
	entries  map[[32]byte]*list.Element
	order    *list.List
}

func newBoundedSet(capacity int) *boundedSet {
	return &boundedSet{capacity: max(1, capacity), entries: make(map[[32]byte]*list.Element), order: list.New()}
}
func (s *boundedSet) add(key string) (duplicate bool) {
	digest := sha256.Sum256([]byte(key))
	s.mu.Lock()
	defer s.mu.Unlock()
	if element, exists := s.entries[digest]; exists {
		s.order.MoveToFront(element)
		return true
	}
	s.entries[digest] = s.order.PushFront(digest)
	if s.order.Len() > s.capacity {
		old := s.order.Back()
		delete(s.entries, old.Value.([32]byte))
		s.order.Remove(old)
	}
	return false
}
func (s *boundedSet) remove(key string) {
	digest := sha256.Sum256([]byte(key))
	s.mu.Lock()
	defer s.mu.Unlock()
	if element, exists := s.entries[digest]; exists {
		delete(s.entries, digest)
		s.order.Remove(element)
	}
}
