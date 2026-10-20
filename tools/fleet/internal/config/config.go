// Package config loads and validates ~/.claude/fleet/fleet.json.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

type Harness string

const (
	Claude   Harness = "claude"
	Codex    Harness = "codex"
	Kimi     Harness = "kimi"
	OpenCode Harness = "opencode"
)

type Role string

const (
	Coordinator Role = "coordinator"
	Impl        Role = "impl"
	Merge       Role = "merge"
	Reviewer    Role = "reviewer"
)

type Tier string

const (
	TierLow    Tier = "low"
	TierMedium Tier = "medium"
	TierHigh   Tier = "high"
)

var (
	harnesses = []Harness{Claude, Codex, Kimi, OpenCode}
	roles     = []Role{Coordinator, Impl, Merge, Reviewer}
	tiers     = []Tier{TierLow, TierMedium, TierHigh}
)

// Repo is one repository the fleet works on. Slots maps a role to its target slot count.
type Repo struct {
	Name     string       `json:"name"`
	Owner    string       `json:"owner"`
	Checkout string       `json:"checkout"`
	Slots    map[Role]int `json:"slots"`
}

// RolePolicy is the harness preference list (best first) and the lowest tier allowed to fill the role.
type RolePolicy struct {
	Prefer  []Harness `json:"prefer"`
	MinTier Tier      `json:"min_tier"`
}

// Duration is a time.Duration that decodes from a Go duration string such as "2h".
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"2h\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

type Thresholds struct {
	HangMinutes    int      `json:"hang_minutes"`
	MigrateAfter   Duration `json:"migrate_after"`
	DiskWarnGB     int      `json:"disk_warn_gb"`
	DiskCriticalGB int      `json:"disk_critical_gb"`
	HeavySlots     int      `json:"heavy_slots"`
}

type Config struct {
	Repos      []Repo              `json:"repos"`
	Roles      map[Role]RolePolicy `json:"roles"`
	Thresholds Thresholds          `json:"thresholds"`
}

// DefaultPath is where fleetd looks for the config.
func DefaultPath(home string) string {
	return filepath.Join(home, ".claude", "fleet", "fleet.json")
}

// ExpandHome replaces a leading "~" with home.
func ExpandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

func defaults() Config {
	return Config{Thresholds: Thresholds{
		HangMinutes:    45,
		MigrateAfter:   Duration(2 * time.Hour),
		DiskWarnGB:     30,
		DiskCriticalGB: 15,
		HeavySlots:     4,
	}}
}

// Load reads and validates the config at path. home resolves "~" in checkout paths.
// Unknown keys, missing checkouts and invalid values are errors.
func Load(path, home string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := cfg.ValidateCheckouts(home); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes and validates data without touching the filesystem.
func Parse(data []byte) (*Config, error) {
	cfg := defaults()
	if err := checkKeys(data, reflect.TypeOf(cfg), "config"); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode: trailing data after config object")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Valid reports whether h is a known harness.
func (h Harness) Valid() bool {
	for _, v := range harnesses {
		if h == v {
			return true
		}
	}
	return false
}

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	for _, v := range roles {
		if r == v {
			return true
		}
	}
	return false
}

func (t Tier) valid() bool {
	for _, v := range tiers {
		if t == v {
			return true
		}
	}
	return false
}

// Validate checks everything that needs no filesystem.
func (c *Config) Validate() error {
	if len(c.Repos) == 0 {
		return errors.New("repos: at least one repo is required")
	}
	seen := map[string]bool{}
	for i, r := range c.Repos {
		if r.Name == "" || r.Owner == "" || r.Checkout == "" {
			return fmt.Errorf("repos[%d]: name, owner and checkout are required", i)
		}
		if seen[r.Name] {
			return fmt.Errorf("repos[%d]: duplicate repo name %q", i, r.Name)
		}
		seen[r.Name] = true
		for _, role := range sortedRoles(r.Slots) {
			if !role.Valid() {
				return fmt.Errorf("repos[%s].slots: unknown role %q", r.Name, role)
			}
			if r.Slots[role] < 0 {
				return fmt.Errorf("repos[%s].slots.%s: must not be negative, got %d", r.Name, role, r.Slots[role])
			}
		}
	}
	for _, role := range sortedRoles(c.Roles) {
		if !role.Valid() {
			return fmt.Errorf("roles: unknown role %q", role)
		}
		p := c.Roles[role]
		if len(p.Prefer) == 0 {
			return fmt.Errorf("roles.%s.prefer: must list at least one harness", role)
		}
		dup := map[Harness]bool{}
		for _, h := range p.Prefer {
			if !h.Valid() {
				return fmt.Errorf("roles.%s.prefer: unknown harness %q", role, h)
			}
			if dup[h] {
				return fmt.Errorf("roles.%s.prefer: duplicate harness %q", role, h)
			}
			dup[h] = true
			if role == Merge && h != Claude {
				return fmt.Errorf("roles.merge.prefer: merge gates are claude only, got %q", h)
			}
		}
		if !p.MinTier.valid() {
			return fmt.Errorf("roles.%s.min_tier: unknown tier %q", role, p.MinTier)
		}
	}
	for _, r := range roles {
		if _, ok := c.Roles[r]; !ok {
			return fmt.Errorf("roles.%s: missing", r)
		}
	}
	return c.Thresholds.validate()
}

func (t Thresholds) validate() error {
	switch {
	case t.HangMinutes <= 0:
		return fmt.Errorf("thresholds.hang_minutes: must be positive, got %d", t.HangMinutes)
	case time.Duration(t.MigrateAfter) <= 0:
		return fmt.Errorf("thresholds.migrate_after: must be positive, got %s", time.Duration(t.MigrateAfter))
	case t.DiskWarnGB <= 0:
		return fmt.Errorf("thresholds.disk_warn_gb: must be positive, got %d", t.DiskWarnGB)
	case t.DiskCriticalGB <= 0:
		return fmt.Errorf("thresholds.disk_critical_gb: must be positive, got %d", t.DiskCriticalGB)
	case t.DiskCriticalGB >= t.DiskWarnGB:
		return fmt.Errorf("thresholds.disk_critical_gb (%d) must be below disk_warn_gb (%d)", t.DiskCriticalGB, t.DiskWarnGB)
	case t.HeavySlots <= 0:
		return fmt.Errorf("thresholds.heavy_slots: must be positive, got %d", t.HeavySlots)
	}
	return nil
}

// ValidateCheckouts requires every repo checkout to be an existing directory.
func (c *Config) ValidateCheckouts(home string) error {
	for _, r := range c.Repos {
		p := ExpandHome(r.Checkout, home)
		fi, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("repos[%s].checkout: %w", r.Name, err)
		}
		if !fi.IsDir() {
			return fmt.Errorf("repos[%s].checkout: %s is not a directory", r.Name, p)
		}
	}
	return nil
}

func sortedRoles[V any](m map[Role]V) []Role {
	out := make([]Role, 0, len(m))
	for r := range m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// checkKeys rejects object keys that are not exactly a json tag of the target struct. encoding/json
// matches keys case-insensitively, so "Repos" would otherwise be accepted. Syntax errors are left to
// the real decoder.
func checkKeys(raw []byte, t reflect.Type, path string) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return nil
		}
		for i, it := range items {
			if err := checkKeys(it, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return nil
		}
		for _, k := range sortedKeys(m) {
			if err := checkKeys(m[k], t.Elem(), path+"."+k); err != nil {
				return err
			}
		}
	case reflect.Struct:
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return nil
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			if name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ","); name != "" && name != "-" {
				fields[name] = t.Field(i).Type
			}
		}
		for _, k := range sortedKeys(m) {
			ft, ok := fields[k]
			if !ok {
				return fmt.Errorf("%s: unknown field %q (keys are case-sensitive)", path, k)
			}
			if err := checkKeys(m[k], ft, path+"."+k); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
