package services

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"market-mate/models"
)

func TestMemoryCache(t *testing.T) {
	ctx := context.Background()
	c := NewMemoryCache(time.Minute)

	t.Run("a miss leaves the destination alone", func(t *testing.T) {
		var got []models.Store
		if c.Get(ctx, "mm:v1:stores:absent", &got) {
			t.Fatal("reported a hit for a key that was never written")
		}
		if got != nil {
			t.Errorf("destination was modified on a miss: %+v", got)
		}
	})

	t.Run("a round trip preserves the value", func(t *testing.T) {
		want := []models.Store{{Name: "Trader Joe's", Address: "1 Union Avenue", Distance: "0.8 km"}}
		c.Set(ctx, "k", want, time.Minute)

		var got []models.Store
		if !c.Get(ctx, "k", &got) {
			t.Fatal("value written to the cache was not found")
		}
		if len(got) != 1 || got[0].Name != want[0].Name || got[0].Distance != want[0].Distance {
			t.Errorf("round trip returned %+v, want %+v", got, want)
		}
	})

	t.Run("entries are copies, not aliases", func(t *testing.T) {
		// Redis cannot hand back a pointer to the caller's slice, so neither may
		// the in-memory implementation: otherwise a mutation would be invisible
		// locally and a bug in production.
		original := []models.Store{{Name: "Safeway"}}
		c.Set(ctx, "aliasing", original, time.Minute)
		original[0].Name = "mutated after Set"

		var got []models.Store
		if !c.Get(ctx, "aliasing", &got) {
			t.Fatal("entry missing")
		}
		if got[0].Name != "Safeway" {
			t.Errorf("cached entry changed with the caller's slice: %q", got[0].Name)
		}
	})

	t.Run("expiry is honoured", func(t *testing.T) {
		c.Set(ctx, "brief", []models.Store{{Name: "gone"}}, time.Millisecond)
		time.Sleep(10 * time.Millisecond)

		var got []models.Store
		if c.Get(ctx, "brief", &got) {
			t.Error("an expired entry was served")
		}
	})

	t.Run("stats count hits and misses", func(t *testing.T) {
		fresh := NewMemoryCache(time.Minute)
		var sink []models.Store
		fresh.Set(ctx, "a", []models.Store{}, time.Minute)
		fresh.Get(ctx, "a", &sink)
		fresh.Get(ctx, "b", &sink)

		stats := fresh.Stats()
		if stats.Hits != 1 || stats.Misses != 1 {
			t.Errorf("stats = %+v, want 1 hit and 1 miss", stats)
		}
		if stats.Kind != "memory" {
			t.Errorf("kind = %q, want memory", stats.Kind)
		}
	})

	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// --- store lookup --------------------------------------------------------------

type stubStoreProvider struct {
	calls   int32
	err     error
	release chan struct{}
}

func (s *stubStoreProvider) FindNearbyStores(_ context.Context, lat, lng float64) ([]models.Store, error) {
	atomic.AddInt32(&s.calls, 1)
	if s.release != nil {
		<-s.release
	}
	if s.err != nil {
		return nil, s.err
	}
	return []models.Store{{Name: "Whole Foods Market", Distance: "0.5 km"}}, nil
}

func TestStoreLookupCachesByCell(t *testing.T) {
	ctx := context.Background()
	provider := &stubStoreProvider{}
	lookup := NewStoreLookup(provider, NewMemoryCache(time.Minute), time.Minute)

	if _, cached, err := lookup.Stores(ctx, "vid", 37.7749, -122.4194); err != nil || cached {
		t.Fatalf("first lookup: cached=%v err=%v, want a miss", cached, err)
	}
	if _, cached, err := lookup.Stores(ctx, "vid", 37.7751, -122.4196); err != nil || !cached {
		t.Fatalf("second lookup 25m away: cached=%v err=%v, want a hit", cached, err)
	}
	if got := atomic.LoadInt32(&provider.calls); got != 1 {
		t.Errorf("provider called %d times, want 1: the second point is in the same cell", got)
	}

	if _, _, err := lookup.Stores(ctx, "vid", 40.7128, -74.0060); err != nil {
		t.Fatalf("third lookup: %v", err)
	}
	if got := atomic.LoadInt32(&provider.calls); got != 2 {
		t.Errorf("provider called %d times, want 2: a different city is a different cell", got)
	}
}

// TestStoreLookupCollapsesConcurrentCalls is what keeps a GraphQL query for N
// ingredients from becoming N billed Places calls.
func TestStoreLookupCollapsesConcurrentCalls(t *testing.T) {
	provider := &stubStoreProvider{release: make(chan struct{})}
	lookup := NewStoreLookup(provider, NewMemoryCache(time.Minute), time.Minute)

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = lookup.Stores(context.Background(), "", 51.5074, -0.1278)
		}(i)
	}

	// Let every caller reach the provider before it returns, so this exercises
	// the in-flight collapse rather than the cache.
	time.Sleep(50 * time.Millisecond)
	close(provider.release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&provider.calls); got != 1 {
		t.Errorf("provider called %d times for %d concurrent callers, want 1", got, callers)
	}
}

func TestStoreLookupDoesNotCacheFailures(t *testing.T) {
	provider := &stubStoreProvider{err: errors.New("places is down")}
	lookup := NewStoreLookup(provider, NewMemoryCache(time.Minute), time.Minute)

	for i := 0; i < 2; i++ {
		if _, _, err := lookup.Stores(context.Background(), "vid", 0, 0); err == nil {
			t.Fatal("expected the provider error to reach the caller")
		}
	}
	if got := atomic.LoadInt32(&provider.calls); got != 2 {
		t.Errorf("provider called %d times, want 2: a failure must not be cached", got)
	}
}

func TestWithinRadius(t *testing.T) {
	stores := []models.Store{
		{Name: "near", Distance: "0.4 km"},
		{Name: "middle", Distance: "1.9 km"},
		{Name: "far", Distance: "4.6 km"},
		{Name: "unparseable", Distance: "just around the corner"},
	}

	cases := []struct {
		name   string
		radius int
		want   []string
	}{
		{"zero means no filter", 0, []string{"near", "middle", "far", "unparseable"}},
		{"negative means no filter", -1, []string{"near", "middle", "far", "unparseable"}},
		{"beyond the provider radius means no filter", 10000, []string{"near", "middle", "far", "unparseable"}},
		{"one kilometre", 1000, []string{"near"}},
		{"two kilometres", 2000, []string{"near", "middle"}},
		// A distance that cannot be parsed sorts to the end and is dropped by
		// any filter, which is the safe direction: better an omitted shop than
		// one claimed to be closer than it is.
		{"unparseable distances are excluded", 4900, []string{"near", "middle", "far"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WithinRadius(stores, tc.radius)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d stores, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, name := range tc.want {
				if got[i].Name != name {
					t.Errorf("store %d = %q, want %q", i, got[i].Name, name)
				}
			}
		})
	}
}
