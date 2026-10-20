package resources

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/config"
)

var th = DefaultThresholds(config.Thresholds{DiskWarnGB: 30, DiskCriticalGB: 15})

func sampler(freeGB int64, load, mem string) Sampler { return swapSampler(freeGB, load, mem, swapOK) }

func swapSampler(freeGB int64, load, mem, swap string) Sampler {
	return Sampler{
		Statfs: func(_ string, st *syscall.Statfs_t) error {
			st.Bsize, st.Bavail = 4096, uint64(freeGB<<30/4096)
			return nil
		},
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			switch {
			case name == "sysctl" && len(args) > 1 && args[1] == "vm.swapusage":
				return []byte(swap), nil
			case name == "sysctl":
				return []byte(load), nil
			}
			return []byte(mem), nil
		},
		Cores: 10,
	}
}

const (
	loadOK   = "{ 12.00 13.00 14.00 }\n"
	memOK    = "The system has 34359738368 (2097152 pages with a page size of 16384).\nSystem-wide memory free percentage: 52%\n"
	memWarn  = "System-wide memory free percentage: 15%\n"
	memCrit  = "System-wide memory free percentage: 9%\n"
	loadWarn = "{ 26.5 20.0 20.0 }\n"
	loadCrit = "{ 40,0 20,0 20,0 }\n"
	swapOK   = "total = 16384,00M  used = 1024,00M  free = 15360,00M  (encrypted)\n"
	swapWarn = "total = 16384.00M  used = 12500.00M  free = 3884.00M  (encrypted)\n"
	swapCrit = "total = 20480,00M  used = 18793,62M  free = 1686,38M  (encrypted)\n"
)

func TestClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		free              int64
		load, mem         string
		disk, ld, mm, all Level
	}{
		{"all ok", 100, loadOK, memOK, OK, OK, OK, OK},
		{"disk warn", 29, loadOK, memOK, Warn, OK, OK, Warn},
		{"disk at warn is ok", 30, loadOK, memOK, OK, OK, OK, OK},
		{"disk critical", 14, loadOK, memOK, Critical, OK, OK, Critical},
		{"disk at critical is warn", 15, loadOK, memOK, Warn, OK, OK, Warn},
		{"load warn", 100, loadWarn, memOK, OK, Warn, OK, Warn},
		{"load critical with locale comma", 100, loadCrit, memOK, OK, Critical, OK, Critical},
		{"memory warn", 100, loadOK, memWarn, OK, OK, Warn, Warn},
		{"memory critical", 100, loadOK, memCrit, OK, OK, Critical, Critical},
		{"worst wins", 29, loadCrit, memWarn, Warn, Critical, Warn, Critical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, err := sampler(tt.free, tt.load, tt.mem).Sample(context.Background(), "/data", th)
			if err != nil {
				t.Fatal(err)
			}
			if s.Disk != tt.disk || s.Load != tt.ld || s.Memory != tt.mm || s.Overall != tt.all {
				t.Fatalf("got disk=%s load=%s mem=%s overall=%s (%+v)", s.Disk, s.Load, s.Memory, s.Overall, s)
			}
		})
	}
}

func TestSwapClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, swap string
		want       Level
	}{
		{"ok", swapOK, OK},
		{"warn", swapWarn, Warn},
		{"critical with comma decimals", swapCrit, Critical},
		{"no swap configured", "total = 0,00M  used = 0,00M  free = 0,00M  (encrypted)\n", OK},
		{"a grown total at 94% used with little swapped is ok", "total = 2048,00M  used = 1920,00M  free = 128,00M  (encrypted)\n", OK},
		{"exactly the warn threshold warns", "total = 20480,00M  used = 8192,00M  free = 12288,00M\n", Warn},
		{"exactly the critical threshold is critical", "total = 20480,00M  used = 16384,00M  free = 4096,00M\n", Critical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, err := swapSampler(100, loadOK, memOK, tt.swap).Sample(context.Background(), "/data", th)
			if err != nil {
				t.Fatal(err)
			}
			if s.Swap != tt.want || s.Overall != tt.want || s.Disk != OK || s.Load != OK || s.Memory != OK {
				t.Fatalf("swap=%s overall=%s (%+v)", s.Swap, s.Overall, s)
			}
		})
	}
	if _, err := swapSampler(100, loadOK, memOK, "nope").Sample(context.Background(), "/data", th); err == nil || !strings.Contains(err.Error(), "vm.swapusage") {
		t.Fatalf("garbage swap must error, got %v", err)
	}
	if th.SwapWarnMB != 8192 || th.SwapCriticalMB != 16384 {
		t.Fatalf("defaults: %+v", th)
	}
}

func TestLevelStringOutOfRange(t *testing.T) {
	t.Parallel()
	for _, l := range []Level{-1, 3, 99} {
		if got := l.String(); !strings.Contains(got, "level(") {
			t.Errorf("Level(%d).String() = %q", int(l), got)
		}
	}
	if OK.String() != "ok" || Warn.String() != "warn" || Critical.String() != "critical" {
		t.Fatal("named levels changed")
	}
}

func TestSampleValues(t *testing.T) {
	t.Parallel()
	s, err := sampler(50, loadOK, memOK).Sample(context.Background(), "/data", th)
	if err != nil {
		t.Fatal(err)
	}
	if s.DiskFreeGB != 50 || s.Load1 != 12 || s.MemFreePct != 52 || s.LoadPerCore != 1.2 {
		t.Fatalf("%+v", s)
	}
}

func TestSampleErrorsAreNotDefaulted(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name string
		mod  func(*Sampler)
		want string
	}{
		{"statfs", func(s *Sampler) { s.Statfs = func(string, *syscall.Statfs_t) error { return boom } }, "statfs"},
		{"sysctl", func(s *Sampler) {
			s.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, boom }
		}, "load average"},
		{"memory_pressure", func(s *Sampler) {
			s.Run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
				if name == "sysctl" {
					return []byte(loadOK), nil
				}
				return nil, boom
			}
		}, "memory pressure"},
		{"garbage load", func(s *Sampler) {
			s.Run = func(context.Context, string, ...string) ([]byte, error) { return []byte("nope"), nil }
		}, "vm.loadavg"},
		{"zero cores", func(s *Sampler) { s.Cores = 0 }, "core count"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := sampler(100, loadOK, memOK)
			tt.mod(&s)
			if _, err := s.Sample(context.Background(), "/data", th); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
}

func TestParsers(t *testing.T) {
	t.Parallel()
	if v, err := ParseLoadAvg("{ 83,07 118,51 115,73 }"); err != nil || v != 83.07 {
		t.Fatalf("%v %v", v, err)
	}
	if _, err := ParseLoadAvg(""); err == nil {
		t.Fatal("empty load must error")
	}
	if _, err := ParseMemoryFree("System-wide memory free percentage: 140%"); err == nil {
		t.Fatal("out of range must error")
	}
	if _, err := ParseMemoryFree("no number"); err == nil {
		t.Fatal("garbage must error")
	}
}

func TestRealSampleSmoke(t *testing.T) {
	t.Parallel()
	s, err := NewSampler().Sample(context.Background(), t.TempDir(), th)
	if err != nil {
		t.Skipf("system probes unavailable: %v", err)
	}
	if s.DiskFreeGB <= 0 || s.Load1 < 0 || s.MemFreePct < 0 {
		t.Fatalf("implausible sample %+v", s)
	}
}
