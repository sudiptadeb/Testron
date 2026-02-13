package config

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level load test configuration.
type Config struct {
	// Global settings
	Users       int           `yaml:"users"`
	Duration    time.Duration `yaml:"duration"`
	RampUp      time.Duration `yaml:"ramp_up"`
	Proxy       string        `yaml:"proxy"`
	Insecure    bool          `yaml:"insecure"`
	ThinkTime   ThinkTime     `yaml:"think_time"`
	Timeout     time.Duration `yaml:"timeout"`
	MaxIdleConn int           `yaml:"max_idle_conns_per_host"`

	// Domains to test
	Domains []Domain `yaml:"domains"`
}

// ThinkTime configures the pause between requests for each simulated user.
type ThinkTime struct {
	Min time.Duration `yaml:"min"`
	Max time.Duration `yaml:"max"`
}

// Rand returns a random duration in [Min, Max].
func (t ThinkTime) Rand() time.Duration {
	if t.Max <= t.Min {
		return t.Min
	}
	delta := t.Max - t.Min
	return t.Min + time.Duration(rand.Int63n(int64(delta)))
}

// Domain represents a target domain with its endpoints.
type Domain struct {
	Name      string     `yaml:"name"`
	BaseURL   string     `yaml:"base_url"`
	Weight    int        `yaml:"weight"`
	Headers   map[string]string `yaml:"headers"`
	Endpoints []Endpoint `yaml:"endpoints"`
}

// Endpoint is a single HTTP endpoint to hit.
type Endpoint struct {
	Name    string            `yaml:"name"`
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
	Body    string            `yaml:"body"`
	Weight  int               `yaml:"weight"`
}

// Load reads and parses a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		// Defaults
		Users:       10,
		Duration:    30 * time.Second,
		RampUp:      0,
		Timeout:     10 * time.Second,
		MaxIdleConn: 100,
		ThinkTime: ThinkTime{
			Min: 0,
			Max: 0,
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	cfg.applyDefaults()

	return cfg, nil
}

func (c *Config) validate() error {
	if len(c.Domains) == 0 {
		return fmt.Errorf("config: at least one domain is required")
	}
	for i, d := range c.Domains {
		if d.BaseURL == "" {
			return fmt.Errorf("config: domain[%d] (%s) missing base_url", i, d.Name)
		}
		if len(d.Endpoints) == 0 {
			return fmt.Errorf("config: domain[%d] (%s) has no endpoints", i, d.Name)
		}
		for j, e := range d.Endpoints {
			if e.Path == "" {
				return fmt.Errorf("config: domain[%d].endpoint[%d] missing path", i, j)
			}
		}
	}
	if c.Users < 1 {
		return fmt.Errorf("config: users must be >= 1")
	}
	if c.Duration < 1*time.Second {
		return fmt.Errorf("config: duration must be >= 1s")
	}
	return nil
}

func (c *Config) applyDefaults() {
	for i := range c.Domains {
		if c.Domains[i].Weight == 0 {
			c.Domains[i].Weight = 1
		}
		if c.Domains[i].Name == "" {
			c.Domains[i].Name = c.Domains[i].BaseURL
		}
		for j := range c.Domains[i].Endpoints {
			if c.Domains[i].Endpoints[j].Weight == 0 {
				c.Domains[i].Endpoints[j].Weight = 1
			}
			if c.Domains[i].Endpoints[j].Method == "" {
				c.Domains[i].Endpoints[j].Method = "GET"
			}
			if c.Domains[i].Endpoints[j].Name == "" {
				c.Domains[i].Endpoints[j].Name = c.Domains[i].Endpoints[j].Path
			}
		}
	}
}

// WeightedPick holds precomputed cumulative weights for fast random selection.
type WeightedPick[T any] struct {
	items      []T
	cumWeights []int
	total      int
}

// NewWeightedPick builds a picker from items and their weights.
func NewWeightedPick[T any](items []T, weightFn func(T) int) *WeightedPick[T] {
	wp := &WeightedPick[T]{
		items:      items,
		cumWeights: make([]int, len(items)),
	}
	sum := 0
	for i, item := range items {
		sum += weightFn(item)
		wp.cumWeights[i] = sum
	}
	wp.total = sum
	return wp
}

// Pick returns a random item based on weights.
func (wp *WeightedPick[T]) Pick() T {
	r := rand.Intn(wp.total)
	for i, cw := range wp.cumWeights {
		if r < cw {
			return wp.items[i]
		}
	}
	return wp.items[len(wp.items)-1]
}
