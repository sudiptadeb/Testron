package report

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sudiptadeb/testron/internal/metrics"
	"github.com/sudiptadeb/testron/internal/runner"
)

// LiveTicker prints periodic status updates during the test.
type LiveTicker struct {
	runner   *runner.Runner
	interval time.Duration
	done     chan struct{}
}

// NewLiveTicker creates a live reporter that prints stats every interval.
func NewLiveTicker(r *runner.Runner, interval time.Duration) *LiveTicker {
	return &LiveTicker{
		runner:   r,
		interval: interval,
		done:     make(chan struct{}),
	}
}

// Start begins printing live updates. Call Stop() to end.
func (lt *LiveTicker) Start() {
	go func() {
		ticker := time.NewTicker(lt.interval)
		defer ticker.Stop()
		for {
			select {
			case <-lt.done:
				return
			case <-ticker.C:
				reqs := lt.runner.TotalRequests.Load()
				errs := lt.runner.TotalErrors.Load()
				users := lt.runner.ActiveUsers.Load()
				bytes := lt.runner.TotalBytes.Load()
				fmt.Fprintf(os.Stderr, "\r  active_users=%d  requests=%d  errors=%d  bytes=%s",
					users, reqs, errs, humanBytes(bytes))
			}
		}
	}()
}

// Stop ends live updates.
func (lt *LiveTicker) Stop() {
	close(lt.done)
	fmt.Fprintln(os.Stderr) // newline after last update
}

// PrintSummary writes a formatted summary table to stdout.
func PrintSummary(s metrics.Summary) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("  LOAD TEST RESULTS")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println()

	fmt.Printf("  Duration:       %s\n", s.Duration.Round(time.Millisecond))
	fmt.Printf("  Total Requests: %d\n", s.TotalRequests)
	fmt.Printf("  Total Errors:   %d (%.1f%%)\n", s.TotalErrors, errorRate(s.TotalErrors, s.TotalRequests))
	fmt.Printf("  Throughput:     %.1f req/s\n", s.RequestsPerSec)
	fmt.Printf("  Data Transfer:  %s\n", humanBytes(s.TotalBytes))
	fmt.Println()

	if len(s.AllLatencies) > 0 {
		fmt.Println("  Latency Distribution (all requests):")
		printLatencyTable(s.AllLatencies, "    ")
		fmt.Println()
	}

	// Per-domain breakdown
	fmt.Println(strings.Repeat("-", 80))
	fmt.Println("  PER-DOMAIN BREAKDOWN")
	fmt.Println(strings.Repeat("-", 80))

	for _, ds := range s.Domains {
		fmt.Println()
		fmt.Printf("  [%s]\n", ds.Domain)
		fmt.Printf("    Requests: %d   Errors: %d   Data: %s\n",
			ds.Requests, ds.Errors, humanBytes(ds.BytesTotal))
		if len(ds.StatusCounts) > 0 {
			fmt.Printf("    Status codes: ")
			first := true
			for code, count := range ds.StatusCounts {
				if !first {
					fmt.Printf(", ")
				}
				fmt.Printf("%d=%d", code, count)
				first = false
			}
			fmt.Println()
		}
		if len(ds.Latencies) > 0 {
			printLatencyTable(ds.Latencies, "    ")
		}
	}

	// Per-endpoint breakdown
	fmt.Println()
	fmt.Println(strings.Repeat("-", 80))
	fmt.Println("  PER-ENDPOINT BREAKDOWN")
	fmt.Println(strings.Repeat("-", 80))

	for _, es := range s.Endpoints {
		fmt.Printf("\n  [%s] %s\n", es.Domain, es.Endpoint)
		fmt.Printf("    Requests: %d   Errors: %d   Data: %s\n",
			es.Requests, es.Errors, humanBytes(es.BytesTotal))
		if len(es.Latencies) > 0 {
			printLatencyTable(es.Latencies, "    ")
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
}

// PrintJSON writes the summary as JSON to stdout.
func PrintJSON(s metrics.Summary) error {
	type latencyStats struct {
		Mean string `json:"mean"`
		P50  string `json:"p50"`
		P90  string `json:"p90"`
		P95  string `json:"p95"`
		P99  string `json:"p99"`
	}

	type domainJSON struct {
		Domain       string         `json:"domain"`
		Requests     int            `json:"requests"`
		Errors       int            `json:"errors"`
		Bytes        int64          `json:"bytes"`
		StatusCounts map[int]int    `json:"status_counts"`
		Latency      latencyStats   `json:"latency"`
	}

	type endpointJSON struct {
		Domain   string       `json:"domain"`
		Endpoint string       `json:"endpoint"`
		Requests int          `json:"requests"`
		Errors   int          `json:"errors"`
		Bytes    int64        `json:"bytes"`
		Latency  latencyStats `json:"latency"`
	}

	type summaryJSON struct {
		Duration       string         `json:"duration"`
		TotalRequests  int            `json:"total_requests"`
		TotalErrors    int            `json:"total_errors"`
		TotalBytes     int64          `json:"total_bytes"`
		RequestsPerSec float64        `json:"requests_per_sec"`
		Latency        latencyStats   `json:"latency"`
		Domains        []domainJSON   `json:"domains"`
		Endpoints      []endpointJSON `json:"endpoints"`
	}

	mkLatency := func(lats []time.Duration) latencyStats {
		return latencyStats{
			Mean: metrics.Mean(lats).String(),
			P50:  metrics.Percentile(lats, 50).String(),
			P90:  metrics.Percentile(lats, 90).String(),
			P95:  metrics.Percentile(lats, 95).String(),
			P99:  metrics.Percentile(lats, 99).String(),
		}
	}

	out := summaryJSON{
		Duration:       s.Duration.Round(time.Millisecond).String(),
		TotalRequests:  s.TotalRequests,
		TotalErrors:    s.TotalErrors,
		TotalBytes:     s.TotalBytes,
		RequestsPerSec: s.RequestsPerSec,
		Latency:        mkLatency(s.AllLatencies),
	}

	for _, ds := range s.Domains {
		out.Domains = append(out.Domains, domainJSON{
			Domain:       ds.Domain,
			Requests:     ds.Requests,
			Errors:       ds.Errors,
			Bytes:        ds.BytesTotal,
			StatusCounts: ds.StatusCounts,
			Latency:      mkLatency(ds.Latencies),
		})
	}

	for _, es := range s.Endpoints {
		out.Endpoints = append(out.Endpoints, endpointJSON{
			Domain:   es.Domain,
			Endpoint: es.Endpoint,
			Requests: es.Requests,
			Errors:   es.Errors,
			Bytes:    es.BytesTotal,
			Latency:  mkLatency(es.Latencies),
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func printLatencyTable(lats []time.Duration, prefix string) {
	fmt.Printf("%sMean: %-12s  P50: %-12s  P90: %-12s  P95: %-12s  P99: %s\n",
		prefix,
		metrics.Mean(lats).Round(time.Microsecond),
		metrics.Percentile(lats, 50).Round(time.Microsecond),
		metrics.Percentile(lats, 90).Round(time.Microsecond),
		metrics.Percentile(lats, 95).Round(time.Microsecond),
		metrics.Percentile(lats, 99).Round(time.Microsecond),
	)
}

func humanBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func errorRate(errors, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(errors) / float64(total) * 100
}
