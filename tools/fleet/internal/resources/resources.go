// Package resources samples disk, load and memory pressure and classifies them as ok, warn or
// critical.
package resources

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/herdr"
)

type Level int

const (
	OK Level = iota
	Warn
	Critical
)

func (l Level) String() string {
	switch l {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Critical:
		return "critical"
	}
	return fmt.Sprintf("level(%d)", int(l))
}

// Default swap thresholds, as a percentage of total swap in use. Swap files live on the data
// volume, so heavy swapping also eats the disk the fleet needs.
const (
	DefaultSwapWarnPct     = 75
	DefaultSwapCriticalPct = 90
)

// Thresholds classify a sample. Only the disk figures are configurable (fleet.json); the load,
// memory and swap figures are fixed defaults, since the plan sets no config keys for them.
type Thresholds struct {
	DiskWarnGB, DiskCriticalGB int
	// Load is the 1-minute load average divided by the core count.
	LoadWarnPerCore, LoadCriticalPerCore float64
	// MemFreePct is memory_pressure's system-wide free percentage; lower is worse.
	MemFreeWarnPct, MemFreeCriticalPct int
	// SwapUsedPct is the share of total swap in use; higher is worse.
	SwapWarnPct, SwapCriticalPct int
}

// DefaultThresholds takes the disk limits from config. The fleet normally runs at 1.2 to 1.8 load
// per core, so warn starts at 2.5 and critical at 4.
func DefaultThresholds(c config.Thresholds) Thresholds {
	return Thresholds{DiskWarnGB: c.DiskWarnGB, DiskCriticalGB: c.DiskCriticalGB,
		LoadWarnPerCore: 2.5, LoadCriticalPerCore: 4, MemFreeWarnPct: 20, MemFreeCriticalPct: 10,
		SwapWarnPct: DefaultSwapWarnPct, SwapCriticalPct: DefaultSwapCriticalPct}
}

// Snapshot is one sample and its classification.
type Snapshot struct {
	DiskFreeGB  float64
	Load1       float64
	Cores       int
	MemFreePct  int
	SwapUsedPct float64
	Disk, Load  Level
	Memory      Level
	Swap        Level
	Overall     Level
	LoadPerCore float64
}

// Sampler reads the machine. Its fields are injectable so tests never touch the real system.
type Sampler struct {
	Statfs func(path string, st *syscall.Statfs_t) error
	Run    herdr.Runner
	Cores  int
}

// NewSampler returns a sampler for the running machine.
func NewSampler() Sampler {
	return Sampler{Statfs: syscall.Statfs, Run: herdr.ExecRunner, Cores: runtime.NumCPU()}
}

// Sample reads disk free space on the volume holding dataDir, the load average (`sysctl -n
// vm.loadavg`) and memory pressure and swap usage (`sysctl -n vm.swapusage`). Memory comes from `memory_pressure -Q`, whose single "free
// percentage" is the kernel's own figure; vm_stat's free pages ignore compressed and purgeable
// memory and read as critical on a healthy Mac. Any failed or unparsable read is an error, never a
// default level.
func (s Sampler) Sample(ctx context.Context, dataDir string, th Thresholds) (Snapshot, error) {
	snap := Snapshot{Cores: s.Cores}
	var st syscall.Statfs_t
	if err := s.Statfs(dataDir, &st); err != nil {
		return snap, fmt.Errorf("statfs %s: %w", dataDir, err)
	}
	snap.DiskFreeGB = float64(st.Bavail) * float64(st.Bsize) / (1 << 30)

	out, err := s.Run(ctx, "sysctl", "-n", "vm.loadavg")
	if err != nil {
		return snap, fmt.Errorf("load average: %w", err)
	}
	if snap.Load1, err = ParseLoadAvg(string(out)); err != nil {
		return snap, err
	}
	out, err = s.Run(ctx, "memory_pressure", "-Q")
	if err != nil {
		return snap, fmt.Errorf("memory pressure: %w", err)
	}
	if snap.MemFreePct, err = ParseMemoryFree(string(out)); err != nil {
		return snap, err
	}
	out, err = s.Run(ctx, "sysctl", "-n", "vm.swapusage")
	if err != nil {
		return snap, fmt.Errorf("swap usage: %w", err)
	}
	if snap.SwapUsedPct, err = ParseSwapUsedPct(string(out)); err != nil {
		return snap, err
	}
	return snap.classify(th)
}

func (s Snapshot) classify(th Thresholds) (Snapshot, error) {
	if s.Cores <= 0 {
		return s, fmt.Errorf("core count must be positive, got %d", s.Cores)
	}
	s.LoadPerCore = s.Load1 / float64(s.Cores)
	s.Disk = below(s.DiskFreeGB, float64(th.DiskWarnGB), float64(th.DiskCriticalGB))
	s.Load = above(s.LoadPerCore, th.LoadWarnPerCore, th.LoadCriticalPerCore)
	s.Memory = below(float64(s.MemFreePct), float64(th.MemFreeWarnPct), float64(th.MemFreeCriticalPct))
	s.Swap = above(s.SwapUsedPct, float64(th.SwapWarnPct), float64(th.SwapCriticalPct))
	s.Overall = max(s.Disk, s.Load, s.Memory, s.Swap)
	return s, nil
}

func below(v, warn, critical float64) Level {
	switch {
	case v < critical:
		return Critical
	case v < warn:
		return Warn
	}
	return OK
}

func above(v, warn, critical float64) Level {
	switch {
	case v >= critical:
		return Critical
	case v >= warn:
		return Warn
	}
	return OK
}

var (
	loadRE = regexp.MustCompile(`^\{?\s*(\d+(?:[.,]\d+)?)\s`)
	memRE  = regexp.MustCompile(`free percentage:\s*(\d{1,3})%`)
	swapRE = regexp.MustCompile(`total = (\d+(?:[.,]\d+)?)M\s+used = (\d+(?:[.,]\d+)?)M`)
)

// ParseSwapUsedPct returns the percentage of swap in use from `sysctl -n vm.swapusage`
// ("total = 16384,00M  used = 14793,62M  free = 1590,38M  (encrypted)"). The decimal separator
// follows the locale. No swap configured (total 0) is 0% used.
func ParseSwapUsedPct(out string) (float64, error) {
	m := swapRE.FindStringSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("unparsable vm.swapusage %q", strings.TrimSpace(out))
	}
	total, err := strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64)
	if err != nil {
		return 0, err
	}
	used, err := strconv.ParseFloat(strings.Replace(m[2], ",", ".", 1), 64)
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	return used / total * 100, nil
}

// ParseLoadAvg returns the 1-minute average from `sysctl -n vm.loadavg` ("{ 1.23 4.56 7.89 }").
// The decimal separator follows the locale, so a comma is accepted.
func ParseLoadAvg(out string) (float64, error) {
	m := loadRE.FindStringSubmatch(strings.TrimSpace(out) + " ")
	if m == nil {
		return 0, fmt.Errorf("unparsable vm.loadavg %q", strings.TrimSpace(out))
	}
	return strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64)
}

// ParseMemoryFree returns the percentage from `memory_pressure -Q`
// ("System-wide memory free percentage: 52%").
func ParseMemoryFree(out string) (int, error) {
	m := memRE.FindStringSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("unparsable memory_pressure output %q", strings.TrimSpace(out))
	}
	v, _ := strconv.Atoi(m[1]) // the pattern admits digits only
	if v > 100 {
		return 0, fmt.Errorf("memory free percentage %d out of range", v)
	}
	return v, nil
}
