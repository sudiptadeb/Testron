package metrics

import (
	"sort"
	"sync"
	"time"
)

// RequestResult captures the outcome of a single HTTP request.
type RequestResult struct {
	Timestamp  time.Time
	Domain     string
	Endpoint   string
	StatusCode int
	Latency    time.Duration
	BytesRead  int64
	Error      string
}

// Collector accumulates request results from all simulated users.
type Collector struct {
	mu      sync.Mutex
	results []RequestResult
	start   time.Time
}

// NewCollector creates a new metrics collector.
func NewCollector() *Collector {
	return &Collector{
		start: time.Now(),
	}
}

// Record adds a request result.
func (c *Collector) Record(r RequestResult) {
	c.mu.Lock()
	c.results = append(c.results, r)
	c.mu.Unlock()
}

// Snapshot returns a copy of all results collected so far.
func (c *Collector) Snapshot() []RequestResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]RequestResult, len(c.results))
	copy(out, c.results)
	return out
}

// DomainStats holds aggregate stats for one domain.
type DomainStats struct {
	Domain       string
	Requests     int
	Errors       int
	BytesTotal   int64
	Latencies    []time.Duration
	StatusCounts map[int]int
}

// EndpointStats holds aggregate stats for one endpoint within a domain.
type EndpointStats struct {
	Domain       string
	Endpoint     string
	Requests     int
	Errors       int
	BytesTotal   int64
	Latencies    []time.Duration
	StatusCounts map[int]int
}

// Summary is the final report data.
type Summary struct {
	TotalRequests  int
	TotalErrors    int
	TotalBytes     int64
	Duration       time.Duration
	RequestsPerSec float64
	Domains        []DomainStats
	Endpoints      []EndpointStats
	AllLatencies   []time.Duration
}

// Summarize computes aggregate statistics from all collected results.
func (c *Collector) Summarize() Summary {
	results := c.Snapshot()
	elapsed := time.Since(c.start)

	s := Summary{
		Duration: elapsed,
	}

	domainMap := make(map[string]*DomainStats)
	epMap := make(map[string]*EndpointStats) // key: "domain|endpoint"

	for _, r := range results {
		s.TotalRequests++
		s.TotalBytes += r.BytesRead
		s.AllLatencies = append(s.AllLatencies, r.Latency)

		if r.Error != "" {
			s.TotalErrors++
		}

		// Domain stats
		ds, ok := domainMap[r.Domain]
		if !ok {
			ds = &DomainStats{
				Domain:       r.Domain,
				StatusCounts: make(map[int]int),
			}
			domainMap[r.Domain] = ds
		}
		ds.Requests++
		ds.BytesTotal += r.BytesRead
		ds.Latencies = append(ds.Latencies, r.Latency)
		if r.StatusCode > 0 {
			ds.StatusCounts[r.StatusCode]++
		}
		if r.Error != "" {
			ds.Errors++
		}

		// Endpoint stats
		epKey := r.Domain + "|" + r.Endpoint
		es, ok := epMap[epKey]
		if !ok {
			es = &EndpointStats{
				Domain:       r.Domain,
				Endpoint:     r.Endpoint,
				StatusCounts: make(map[int]int),
			}
			epMap[epKey] = es
		}
		es.Requests++
		es.BytesTotal += r.BytesRead
		es.Latencies = append(es.Latencies, r.Latency)
		if r.StatusCode > 0 {
			es.StatusCounts[r.StatusCode]++
		}
		if r.Error != "" {
			es.Errors++
		}
	}

	if elapsed > 0 {
		s.RequestsPerSec = float64(s.TotalRequests) / elapsed.Seconds()
	}

	for _, ds := range domainMap {
		s.Domains = append(s.Domains, *ds)
	}
	sort.Slice(s.Domains, func(i, j int) bool {
		return s.Domains[i].Domain < s.Domains[j].Domain
	})

	for _, es := range epMap {
		s.Endpoints = append(s.Endpoints, *es)
	}
	sort.Slice(s.Endpoints, func(i, j int) bool {
		if s.Endpoints[i].Domain != s.Endpoints[j].Domain {
			return s.Endpoints[i].Domain < s.Endpoints[j].Domain
		}
		return s.Endpoints[i].Endpoint < s.Endpoints[j].Endpoint
	})

	return s
}

// Percentile computes the p-th percentile from a slice of durations (0-100).
func Percentile(latencies []time.Duration, p float64) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := int(float64(len(sorted)-1) * p / 100.0)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// Mean computes the mean latency.
func Mean(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	return total / time.Duration(len(latencies))
}
