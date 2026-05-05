package observability

import (
	"sync"
	"time"
)

type Record struct {
	RequestTime           time.Time
	Model                 string
	RouteMode             string
	SelectedNode          string
	LoadBalancingStrategy string
	RetryCount            int
	Duration              time.Duration
	FinalStatus           string
	Error                 string
}

type Recorder struct {
	mu      sync.Mutex
	limit   int
	records []Record
}

func NewRecorder(limit int) *Recorder {
	if limit <= 0 {
		limit = 100
	}
	return &Recorder{limit: limit}
}

func (r *Recorder) Record(record Record) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.records) >= r.limit {
		copy(r.records, r.records[1:])
		r.records[len(r.records)-1] = record
		return
	}
	r.records = append(r.records, record)
}

func (r *Recorder) Records() []Record {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	cloned := make([]Record, len(r.records))
	copy(cloned, r.records)
	return cloned
}
