package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fast(s Semaphore) Semaphore {
	s.Poll = 5 * time.Millisecond
	return s
}

func TestNPlusOneHoldersOneWaits(t *testing.T) {
	t.Parallel()
	const n = 3
	dir := t.TempDir()
	var holding, peak atomic.Int32
	hold := make(chan struct{})
	var wg sync.WaitGroup
	acquired := make(chan struct{}, n+1)
	for i := 0; i < n+1; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel, err := fast(Heavy(dir, n, "")).Acquire(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			cur := holding.Add(1)
			for {
				p := peak.Load()
				if cur <= p || peak.CompareAndSwap(p, cur) {
					break
				}
			}
			acquired <- struct{}{}
			<-hold
			holding.Add(-1)
			rel()
		}()
	}
	for i := 0; i < n; i++ {
		<-acquired
	}
	select {
	case <-acquired:
		t.Fatal("the N+1th holder got a slot while N were held")
	case <-time.After(150 * time.Millisecond):
	}
	hold <- struct{}{} // one releases
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter never got the freed slot")
	}
	close(hold)
	wg.Wait()
	if peak.Load() != n {
		t.Fatalf("peak concurrent holders %d, want %d", peak.Load(), n)
	}
}

func TestMarkerWhileWaitingOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "heavy-wait", "cc-impl")
	rel, err := fast(Heavy(dir, 1, marker)).Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("an uncontended acquire must not leave a marker")
	}
	done := make(chan error, 1)
	go func() {
		r, err := fast(Heavy(dir, 1, marker)).Acquire(context.Background())
		if err == nil {
			if _, serr := os.Stat(marker); !os.IsNotExist(serr) {
				err = errors.New("marker still present after acquiring")
			}
			r()
		}
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("marker never appeared while waiting")
		}
		time.Sleep(5 * time.Millisecond)
	}
	rel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTimeoutRemovesMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "heavy-wait", "slot")
	rel, err := fast(Heavy(dir, 1, marker)).Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rel()
	w := fast(Heavy(dir, 1, marker))
	w.Timeout = 80 * time.Millisecond
	start := time.Now()
	_, err = w.Acquire(context.Background())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if time.Since(start) < 70*time.Millisecond {
		t.Fatal("returned before the timeout")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("marker left behind after timeout")
	}
}

func TestContextCancel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rel, err := fast(Named(dir, "x")).Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fast(Named(dir, "x")).Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
}

func TestReleaseIsIdempotentAndFrees(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rel, err := Named(dir, "x").Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rel()
	rel()
	rel2, err := Named(dir, "x").Acquire(context.Background())
	if err != nil {
		t.Fatalf("lock not free after release: %v", err)
	}
	rel2()
}

func TestNamedLocksAreIndependent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, err := Named(dir, "push-repo").Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer a()
	b, err := fast(Named(dir, "other")).Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b()
	w := fast(Named(dir, "push-repo"))
	w.Timeout = 50 * time.Millisecond
	if _, err := w.Acquire(context.Background()); !errors.Is(err, ErrTimeout) {
		t.Fatalf("same name must exclude: %v", err)
	}
}

func TestValidNameAndSlot(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"a", "push-repo-1", strings.Repeat("a", 64), "0"} {
		if err := ValidName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "A", "a.b", "../x", "a/b", "a b", "a_b", strings.Repeat("a", 65), "heavy.0", "a\n"} {
		if err := ValidName(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if s, err := SlotName(""); err != nil || s != UnknownSlot {
		t.Errorf("empty slot: %q %v", s, err)
	}
	if s, err := SlotName("cc-impl"); err != nil || s != "cc-impl" {
		t.Errorf("slot: %q %v", s, err)
	}
	for _, bad := range []string{"../x", "A", "-a", "a/b"} {
		if _, err := SlotName(bad); err == nil {
			t.Errorf("slot %q must be rejected", bad)
		}
	}
}

func TestNoPaths(t *testing.T) {
	t.Parallel()
	if _, err := (Semaphore{}).Acquire(context.Background()); err == nil {
		t.Fatal("empty semaphore must error")
	}
}
