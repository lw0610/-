package acquire

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureSpec() CacheSpec {
	return CacheSpec{SourceID: "source.fixture", SourceVersion: "1", Category: "transport.railway", BBox: []float64{100, 20, 120, 40}, Params: map[string]string{"year": "2024"}, AdapterVersion: "1"}
}
func fixtureBuild(t *testing.T, contents string) func(string) (Manifest, error) {
	t.Helper()
	return func(dir string) (Manifest, error) {
		for _, path := range []string{"raw/source.geojson", "out/rail.geojson"} {
			if err := os.WriteFile(filepath.Join(dir, path), []byte(contents), 0600); err != nil {
				return Manifest{}, err
			}
		}
		return Manifest{RawFiles: []string{"raw/source.geojson"}, OutputFiles: []string{"out/rail.geojson"}, Attribution: "synthetic fixture"}, nil
	}
}
func TestStoreReuseExpiryIntegrityAndContainment(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := NewStore(t.TempDir(), 4096)
	store.Now = func() time.Time { return now }
	spec := fixtureSpec()
	build := fixtureBuild(t, "fixture")
	first, reused, err := store.Materialize(ctx, spec, 1, build)
	if err != nil || reused {
		t.Fatalf("%v %v", reused, err)
	}
	second, reused, err := store.Materialize(ctx, spec, 1, func(string) (Manifest, error) { t.Fatal("cache hit fetched again"); return Manifest{}, nil })
	if err != nil || !reused || second.Key != first.Key {
		t.Fatalf("%v %v", reused, err)
	}
	child := spec
	child.BBox = []float64{110, 30, 111, 31}
	if _, ok, err := store.FindContaining(ctx, child); err != nil || !ok {
		t.Fatalf("containment %v %v", ok, err)
	}
	now = now.Add(25 * time.Hour)
	if _, ok, err := store.Lookup(ctx, spec.Key()); err != nil || ok {
		t.Fatalf("expiry %v %v", ok, err)
	}
	renewed, reused, err := store.Materialize(ctx, spec, 1, build)
	if err != nil || reused {
		t.Fatalf("renew %v %v", reused, err)
	}
	if err = os.WriteFile(renewed.Outputs()[0], []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Lookup(ctx, spec.Key()); err != nil || ok {
		t.Fatalf("corrupt cache %v %v", ok, err)
	}
	if err = store.Delete(ctx, spec.Key()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Lookup(ctx, spec.Key()); err != nil || ok {
		t.Fatalf("deleted %v %v", ok, err)
	}
	// A run may retain this value after deletion; the source record is independent of files.
	if renewed.Attribution != "synthetic fixture" {
		t.Fatal("provenance lost")
	}
}
func TestStoreConcurrentBuildersAndCapacity(t *testing.T) {
	root := t.TempDir()
	var count atomic.Int32
	ctx := context.Background()
	spec := fixtureSpec()
	build := fixtureBuild(t, "fixture")
	wrapped := func(dir string) (Manifest, error) { count.Add(1); time.Sleep(20 * time.Millisecond); return build(dir) }
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := NewStore(root, 1024).Materialize(ctx, spec, 0, wrapped)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count.Load() != 1 {
		t.Fatal(count.Load())
	}
	store := NewStore(root, 15)
	other := spec
	other.Category = "hydro.river_line"
	if _, _, err := store.Materialize(ctx, other, 0, build); err == nil {
		t.Fatal("capacity not enforced")
	}
	if _, ok, err := store.Lookup(ctx, spec.Key()); err != nil || !ok {
		t.Fatal("capacity failure deleted existing data")
	}
}
func TestStoreLockCancellationAndPathGuards(t *testing.T) {
	store := NewStore(t.TempDir(), 1024)
	unlock, err := store.lock(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err = store.lock(ctx, "fixture"); err == nil {
		t.Fatal("lock ignored cancellation")
	}
	spec := fixtureSpec()
	spec.SourceID = "../escape"
	if _, _, err = store.Materialize(context.Background(), spec, 0, fixtureBuild(t, "x")); err == nil {
		t.Fatal("source traversal accepted")
	}
	spec = fixtureSpec()
	if _, _, err = store.Materialize(context.Background(), spec, 0, func(dir string) (Manifest, error) {
		if err := os.Symlink(t.TempDir(), filepath.Join(dir, "out", "link")); err != nil {
			return Manifest{}, err
		}
		return Manifest{OutputFiles: []string{"out/link"}}, nil
	}); err == nil {
		t.Fatal("symlink accepted")
	}
}
