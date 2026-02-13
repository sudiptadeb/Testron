package runner

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sudiptadeb/testron/internal/config"
	"github.com/sudiptadeb/testron/internal/metrics"
)

// Runner orchestrates the load test.
type Runner struct {
	cfg       *config.Config
	collector *metrics.Collector
	client    *http.Client

	// Live counters for the reporter
	ActiveUsers   atomic.Int64
	TotalRequests atomic.Int64
	TotalErrors   atomic.Int64
	TotalBytes    atomic.Int64
}

// New creates a runner from the given config.
func New(cfg *config.Config) *Runner {
	transport := &http.Transport{
		MaxIdleConnsPerHost: cfg.MaxIdleConn,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.Insecure,
		},
	}

	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}

	return &Runner{
		cfg:       cfg,
		collector: metrics.NewCollector(),
		client:    client,
	}
}

// Collector returns the metrics collector for reporting.
func (r *Runner) Collector() *metrics.Collector {
	return r.collector
}

// Run executes the load test. It blocks until the test duration elapses or ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.Duration)
	defer cancel()

	// Build weighted pickers
	domainPicker := config.NewWeightedPick(r.cfg.Domains, func(d config.Domain) int {
		return d.Weight
	})

	endpointPickers := make(map[string]*config.WeightedPick[config.Endpoint])
	for _, d := range r.cfg.Domains {
		endpointPickers[d.BaseURL] = config.NewWeightedPick(d.Endpoints, func(e config.Endpoint) int {
			return e.Weight
		})
	}

	var wg sync.WaitGroup

	// Ramp-up: spread user starts over ramp_up duration
	rampDelay := time.Duration(0)
	if r.cfg.RampUp > 0 && r.cfg.Users > 1 {
		rampDelay = r.cfg.RampUp / time.Duration(r.cfg.Users-1)
	}

	for i := 0; i < r.cfg.Users; i++ {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()

			// Wait for ramp-up
			if rampDelay > 0 && userID > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(rampDelay * time.Duration(userID)):
				}
			}

			r.ActiveUsers.Add(1)
			defer r.ActiveUsers.Add(-1)

			r.userLoop(ctx, userID, domainPicker, endpointPickers)
		}(i)
	}

	wg.Wait()
	return nil
}

func (r *Runner) userLoop(
	ctx context.Context,
	userID int,
	domainPicker *config.WeightedPick[config.Domain],
	endpointPickers map[string]*config.WeightedPick[config.Endpoint],
) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		domain := domainPicker.Pick()
		epPicker := endpointPickers[domain.BaseURL]
		endpoint := epPicker.Pick()

		result := r.doRequest(ctx, domain, endpoint)
		r.collector.Record(result)

		r.TotalRequests.Add(1)
		r.TotalBytes.Add(result.BytesRead)
		if result.Error != "" {
			r.TotalErrors.Add(1)
		}

		// Think time
		if r.cfg.ThinkTime.Max > 0 {
			pause := r.cfg.ThinkTime.Rand()
			select {
			case <-ctx.Done():
				return
			case <-time.After(pause):
			}
		}
	}
}

func (r *Runner) doRequest(ctx context.Context, domain config.Domain, endpoint config.Endpoint) metrics.RequestResult {
	targetURL := domain.BaseURL + endpoint.Path

	var body io.Reader
	if endpoint.Body != "" {
		body = strings.NewReader(endpoint.Body)
	}

	req, err := http.NewRequestWithContext(ctx, endpoint.Method, targetURL, body)
	if err != nil {
		return metrics.RequestResult{
			Timestamp: time.Now(),
			Domain:    domain.Name,
			Endpoint:  endpoint.Name,
			Error:     fmt.Sprintf("build request: %v", err),
		}
	}

	// Apply domain-level headers
	for k, v := range domain.Headers {
		req.Header.Set(k, v)
	}
	// Apply endpoint-level headers (override domain)
	for k, v := range endpoint.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := r.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return metrics.RequestResult{
			Timestamp: time.Now(),
			Domain:    domain.Name,
			Endpoint:  endpoint.Name,
			Latency:   latency,
			Error:     fmt.Sprintf("request failed: %v", err),
		}
	}

	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	return metrics.RequestResult{
		Timestamp:  time.Now(),
		Domain:     domain.Name,
		Endpoint:   endpoint.Name,
		StatusCode: resp.StatusCode,
		Latency:    latency,
		BytesRead:  n,
	}
}
