package durable

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

func openTest(t *testing.T, path string, hook Hook) (*Log, Recovery) {
	t.Helper()
	l, recovery, err := Open(path, hook)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, recovery
}

func TestRecoveryAndIdempotency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.wal")
	l, _ := openTest(t, path, nil)
	for i := 1; i <= 2; i++ {
		if token, err := l.Reserve(); err != nil || token != i {
			t.Fatalf("reserve %d: %d %v", i, token, err)
		}
	}
	if err := l.Fence(2); err != nil {
		t.Fatal(err)
	}
	first, err := l.Write(2, "invoice", sim.Idempotent)
	if err != nil || first.Status != "committed" {
		t.Fatal(first, err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, report := openTest(t, path, nil)
	if report.Records != 4 || report.TruncatedBytes != 0 {
		t.Fatal(report)
	}
	if token, err := recovered.Reserve(); err != nil || token != 3 {
		t.Fatal(token, err)
	}
	if err := recovered.Fence(1); err != nil || recovered.Snapshot().Fence != 2 {
		t.Fatal(err)
	}
	old, err := recovered.Write(1, "invoice", sim.Idempotent)
	if err != nil || old.Status != "rejected" {
		t.Fatal(old, err)
	}
	again, err := recovered.Write(3, "invoice", sim.Idempotent)
	if err != nil || again.Status != "deduplicated" || again.Effect != first.Effect {
		t.Fatal(again, err)
	}
	snapshot := recovered.Snapshot()
	snapshot.Effects[0].Key = "mutated"
	if recovered.Snapshot().Effects[0].Key != "invoice" {
		t.Fatal("snapshot aliases state")
	}
}

func TestEveryTornTailAndCorruptByte(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.wal")
	l, _ := openTest(t, path, nil)
	if _, err := l.Reserve(); err != nil {
		t.Fatal(err)
	}
	prefix := l.bytes
	if _, err := l.Reserve(); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for end := int(prefix); end < len(data); end++ {
		testPath := filepath.Join(t.TempDir(), "torn.wal")
		if err := os.WriteFile(testPath, data[:end], 0600); err != nil {
			t.Fatal(err)
		}
		recovered, report := openTest(t, testPath, nil)
		if recovered.Snapshot().Epoch != 1 || report.TruncatedBytes != int64(end)-prefix {
			t.Fatalf("cut %d: %+v %+v", end, report, recovered.Snapshot())
		}
		if token, err := recovered.Reserve(); err != nil || token != 2 {
			t.Fatalf("append after cut %d: %d %v", end, token, err)
		}
		if err := recovered.Close(); err != nil {
			t.Fatal(err)
		}
		final, _ := openTest(t, testPath, nil)
		if final.Snapshot().Epoch != 2 {
			t.Fatal("repaired WAL cannot be recovered")
		}
	}
	for at := range data {
		corrupt := append([]byte{}, data...)
		corrupt[at] ^= 1
		testPath := filepath.Join(t.TempDir(), "corrupt.wal")
		if err := os.WriteFile(testPath, corrupt, 0600); err != nil {
			t.Fatal(err)
		}
		if bad, _, err := Open(testPath, nil); err == nil {
			_ = bad.Close()
			t.Fatalf("accepted corrupt byte %d", at)
		}
	}
}

func TestCrashBoundariesAndPoisoning(t *testing.T) {
	for _, point := range []string{"before-append", "after-header", "after-append", "after-sync", "after-apply"} {
		t.Run(point, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "crash.wal")
			injected := errors.New("injected failure")
			l, _ := openTest(t, path, func(p string) error {
				if p == point {
					return injected
				}
				return nil
			})
			if _, err := l.Reserve(); !errors.Is(err, injected) {
				t.Fatal(err)
			}
			if _, err := l.Reserve(); err == nil {
				t.Fatal("poisoned append succeeded")
			}
			if err := l.Fence(1); err == nil {
				t.Fatal("poisoned fence succeeded")
			}
			if _, err := l.Write(1, "key", sim.Idempotent); err == nil {
				t.Fatal("poisoned write succeeded")
			}
			if err := l.Close(); err != nil {
				t.Fatal(err)
			}
			recovered, _ := openTest(t, path, nil)
			expected := 1
			if point == "before-append" || point == "after-header" {
				expected = 0
			}
			if recovered.Snapshot().Epoch != expected {
				t.Fatal(recovered.Snapshot())
			}
		})
	}
}

func TestExclusiveOwnerAndConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned.wal")
	l, _ := openTest(t, path, nil)
	if other, _, err := Open(path, nil); err == nil {
		_ = other.Close()
		t.Fatal("second writer obtained ownership")
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := l.Reserve(); err != nil {
				t.Error(err)
			}
			if _, err := l.Write(1, "same-key", sim.Idempotent); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	state := l.Snapshot()
	if state.Epoch != 20 || len(state.Effects) != 1 || state.Sequence != 21 {
		t.Fatal(state)
	}
}

func TestInvalidRequestsAndDeliberatelyUnsafePolicies(t *testing.T) {
	l, _ := openTest(t, filepath.Join(t.TempDir(), "policies.wal"), nil)
	for _, token := range []int{0, -1, MaxToken + 1} {
		if err := l.Fence(token); err == nil {
			t.Fatal("accepted invalid fence")
		}
		if _, err := l.Write(token, "key", sim.Idempotent); err == nil {
			t.Fatal("accepted invalid write")
		}
	}
	if _, err := l.Write(1, "", sim.Idempotent); err == nil {
		t.Fatal("accepted empty key")
	}
	if _, err := l.Write(1, "key", "unknown"); err == nil {
		t.Fatal("accepted unknown policy")
	}
	if err := l.Fence(2); err != nil {
		t.Fatal(err)
	}
	for _, p := range []sim.Policy{sim.LeaseOnly, sim.LeaseOnly, sim.Fenced} {
		token := 1
		if p == sim.Fenced {
			token = 2
		}
		if result, err := l.Write(token, "key", p); err != nil || result.Status != "committed" {
			t.Fatal(result, err)
		}
	}
	if len(l.Snapshot().Effects) != 3 {
		t.Fatal("unsafe controls were silently protected")
	}
}
