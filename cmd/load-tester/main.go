package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/sudiptadeb/testron/internal/config"
	"github.com/sudiptadeb/testron/internal/report"
	"github.com/sudiptadeb/testron/internal/runner"
)

func main() {
	var (
		configFile = flag.String("config", "", "path to YAML config file (required)")
		users      = flag.Int("users", 0, "override number of concurrent users")
		duration   = flag.Duration("duration", 0, "override test duration (e.g. 60s, 5m)")
		proxy      = flag.String("proxy", "", "HTTP(S) proxy URL for SASE connector (e.g. http://proxy:8080)")
		insecure   = flag.Bool("insecure", false, "skip TLS certificate verification")
		jsonOutput = flag.Bool("json", false, "output results as JSON instead of table")
		rampUp     = flag.Duration("ramp-up", 0, "override ramp-up period to stagger user starts")
		quiet      = flag.Bool("quiet", false, "suppress live progress output")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `load-tester - Multi-user, multi-domain SASE connector load simulator

Simulates concurrent users making weighted-random requests across multiple
domains and endpoints. Designed to test SASE connector capacity.

Usage:
  load-tester -config <file.yaml> [flags]

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Example:
  load-tester -config test.yaml -users 50 -duration 2m -proxy http://sase:8080
`)
	}
	flag.Parse()

	if *configFile == "" {
		fmt.Fprintln(os.Stderr, "error: -config flag is required")
		flag.Usage()
		os.Exit(1)
	}

	cfg, err := config.Load(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// CLI overrides
	if *users > 0 {
		cfg.Users = *users
	}
	if *duration > 0 {
		cfg.Duration = *duration
	}
	if *proxy != "" {
		cfg.Proxy = *proxy
	}
	if *insecure {
		cfg.Insecure = true
	}
	if *rampUp > 0 {
		cfg.RampUp = *rampUp
	}

	// Print test plan
	fmt.Fprintf(os.Stderr, "load-tester: %d users, %s duration, %d domains\n",
		cfg.Users, cfg.Duration, len(cfg.Domains))
	if cfg.Proxy != "" {
		fmt.Fprintf(os.Stderr, "load-tester: proxy=%s\n", cfg.Proxy)
	}
	if cfg.RampUp > 0 {
		fmt.Fprintf(os.Stderr, "load-tester: ramp_up=%s\n", cfg.RampUp)
	}
	for _, d := range cfg.Domains {
		fmt.Fprintf(os.Stderr, "  domain: %s (%s) weight=%d endpoints=%d\n",
			d.Name, d.BaseURL, d.Weight, len(d.Endpoints))
	}
	fmt.Fprintln(os.Stderr)

	// Setup runner
	r := runner.New(cfg)

	// Live progress
	var ticker *report.LiveTicker
	if !*quiet && !*jsonOutput {
		ticker = report.NewLiveTicker(r, 1*time.Second)
		ticker.Start()
	}

	// Handle ctrl+c gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nload-tester: stopping (ctrl+c)...")
		cancel()
	}()

	// Run
	fmt.Fprintf(os.Stderr, "load-tester: starting...\n")
	if err := r.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}

	if ticker != nil {
		ticker.Stop()
	}

	// Results
	summary := r.Collector().Summarize()

	if *jsonOutput {
		if err := report.PrintJSON(summary); err != nil {
			fmt.Fprintf(os.Stderr, "error writing JSON: %v\n", err)
			os.Exit(1)
		}
	} else {
		report.PrintSummary(summary)
	}
}
