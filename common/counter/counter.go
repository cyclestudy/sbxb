package counter

import (
	"strings"
	"sync"
	"sync/atomic"
)

// TrafficCounter provides atomic per-user upload/download byte counting.
type TrafficCounter struct {
	Upload   atomic.Int64
	Download atomic.Int64
}

// AddUpload adds bytes to upload counter.
func (c *TrafficCounter) AddUpload(n int64) { c.Upload.Add(n) }

// AddDownload adds bytes to download counter.
func (c *TrafficCounter) AddDownload(n int64) { c.Download.Add(n) }

// Reset reads and resets both counters, returns [upload, download].
func (c *TrafficCounter) Reset() [2]int64 {
	return [2]int64{c.Upload.Swap(0), c.Download.Swap(0)}
}

// TrafficStorage is a concurrent map of user name to TrafficCounter.
type TrafficStorage struct {
	counters sync.Map // key: user name (string), value: *TrafficCounter
}

// NewTrafficStorage creates a new TrafficStorage.
func NewTrafficStorage() *TrafficStorage {
	return &TrafficStorage{}
}

// GetOrCreate returns the TrafficCounter for the given user, creating one if it
// does not already exist.
func (s *TrafficStorage) GetOrCreate(user string) *TrafficCounter {
	if v, ok := s.counters.Load(user); ok {
		return v.(*TrafficCounter)
	}
	actual, _ := s.counters.LoadOrStore(user, &TrafficCounter{})
	return actual.(*TrafficCounter)
}

// ResetAll iterates over every counter, resets it, and returns a map of
// user name to [upload, download] byte totals. Entries with zero traffic
// are included so the caller can decide whether to filter them.
func (s *TrafficStorage) ResetAll() map[string][2]int64 {
	result := make(map[string][2]int64)
	s.counters.Range(func(key, value any) bool {
		user := key.(string)
		tc := value.(*TrafficCounter)
		data := tc.Reset()
		result[user] = data
		return true
	})
	return result
}

// ResetByPrefix resets only counters whose key starts with the given prefix
// and returns their values. Non-matching entries are left untouched.
func (s *TrafficStorage) ResetByPrefix(prefix string) map[string][2]int64 {
	result := make(map[string][2]int64)
	s.counters.Range(func(key, value any) bool {
		k := key.(string)
		if !strings.HasPrefix(k, prefix) {
			return true
		}
		tc := value.(*TrafficCounter)
		data := tc.Reset()
		result[k] = data
		return true
	})
	return result
}
