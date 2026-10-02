package lcsc

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testCategoryTreePath = "/ftps/wm/product/category/tree"
	testOneLevelPath     = "/ftps/wm/product/catalog/menu/onelevel"
)

// recordingCache is a Cache that records the TTL of each Set call.
type recordingCache struct {
	mu      sync.Mutex
	entries map[string][]byte
	ttls    map[string]time.Duration
}

func newRecordingCache() *recordingCache {
	return &recordingCache{entries: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (c *recordingCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *recordingCache) Set(key string, value []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = value
	c.ttls[key] = ttl
}

func (c *recordingCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
	delete(c.ttls, key)
}

func newCachedTestClient(cache Cache, fn roundTripFunc) *Client {
	return NewClient(
		WithBaseURL("https://wmsc.lcsc.com/ftps/wm"),
		WithHTTPClient(newTestHTTPClient(fn)),
		WithCache(cache),
		WithCacheConfig(CacheConfig{Enabled: true, SearchTTL: time.Minute, DetailsTTL: time.Minute}),
		WithoutRetry(),
	)
}

// newTreeTestClient returns a client without cache that answers the tree
// request with the trimmed live tree. It counts the requests.
func newTreeTestClient(t *testing.T, calls *int32) *Client {
	t.Helper()
	tree := mustReadFixture(t, "category_tree.json")
	return newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(calls, 1)
		if req.Method != http.MethodGet || req.URL.Path != testCategoryTreePath {
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		return jsonResponse(http.StatusOK, tree), nil
	})
}

func categoryIDs(nodes []Category) []int {
	ids := make([]int, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	return ids
}

func TestCatalogTreeDropsMaintenanceRoot(t *testing.T) {
	var calls int32
	client := newTreeTestClient(t, &calls)
	defer func() { _ = client.Close() }()

	tree, err := client.Catalog.Tree(context.Background())
	if err != nil {
		t.Fatalf("tree failed: %v", err)
	}

	// The fixture has the roots 12, 15, 30, 1569 and 1729.
	if got, want := categoryIDs(tree), []int{12, 15, 30, 1569}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected roots %v, got %v", want, got)
	}

	passives := tree[2]
	if passives.Name != "Passives" || passives.ParentID != 0 || passives.Level != 1 || passives.IsLeaf() {
		t.Fatalf("unexpected root: %+v", passives)
	}
	resistors := passives.Children[1]
	if resistors.ID != 501 || resistors.ParentID != 30 || resistors.Level != 2 {
		t.Fatalf("unexpected parent category: id %d, parent %d, level %d", resistors.ID, resistors.ParentID, resistors.Level)
	}
	chip := resistors.Children[0]
	if chip.ID != 1199 || chip.Name != "Chip Resistor - Surface Mount" || chip.ParentID != 501 || chip.Level != 3 || !chip.IsLeaf() {
		t.Fatalf("unexpected leaf category: %+v", chip)
	}

	mosfets := tree[0].Children[0].Children[0].Children[0]
	if mosfets.ID != 1436 || mosfets.Level != 4 || mosfets.ParentID != 1433 {
		t.Fatalf("unexpected level 4 category: id %d, level %d, parent %d", mosfets.ID, mosfets.Level, mosfets.ParentID)
	}
}

func TestCatalogTreeIsCachedFor24Hours(t *testing.T) {
	tree := mustReadFixture(t, "category_tree.json")
	cache := newRecordingCache()

	var calls int32
	client := newCachedTestClient(cache, func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, tree), nil
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	first, err := client.Catalog.Tree(ctx)
	if err != nil {
		t.Fatalf("first tree failed: %v", err)
	}
	// The caller can change the result without an effect on the cache.
	first[0].Name = "changed"
	first[2].Children = nil

	second, err := client.Catalog.Tree(ctx)
	if err != nil {
		t.Fatalf("second tree failed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one request, got %d", got)
	}
	if got := cache.ttls[catalogTreeCacheKey]; got != 24*time.Hour {
		t.Fatalf("expected tree TTL 24h, got %v", got)
	}
	if second[0].Name != "Discrete Semiconductors" || len(second[2].Children) != 2 {
		t.Fatalf("expected an unchanged cached tree, got %q with %d children", second[0].Name, len(second[2].Children))
	}
	if second[2].Children[1].Children[0].ParentID != 501 || second[2].Children[1].Children[0].Level != 3 {
		t.Fatalf("expected parent and level in the cached tree, got %+v", second[2].Children[1].Children[0])
	}
}

func TestCatalogTreeEmptyIsNotFound(t *testing.T) {
	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":[{"categoryId":1729,"categoryNameEn":"Maintenance, Repair & Operations","childrenList":[]}],"ok":true}`), nil
	})
	defer func() { _ = client.Close() }()

	if _, err := client.Catalog.Tree(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an empty tree, got %v", err)
	}
}

func TestCatalogPath(t *testing.T) {
	var calls int32
	client := newTreeTestClient(t, &calls)
	defer func() { _ = client.Close() }()

	tests := []struct {
		name string
		id   int
		want []CategoryRef
	}{
		{"leaf", 1199, []CategoryRef{{30, "Passives"}, {501, "Resistors"}, {1199, "Chip Resistor - Surface Mount"}}},
		{"parent", 495, []CategoryRef{{30, "Passives"}, {495, "Capacitors"}}},
		{"root", 30, []CategoryRef{{30, "Passives"}}},
		{"level 4", 1436, []CategoryRef{{12, "Discrete Semiconductors"}, {1420, "Transistors"}, {1433, "FETs, MOSFETs"}, {1436, "Single FETs, MOSFETs"}}},
		// 1585 occurs under 15 and under 1569. Path uses the first one.
		{"repeated id", 1585, []CategoryRef{{15, "Hardware, Fasteners, Accessories"}, {2580, "Washers"}, {1585, "Bushing, Shoulder Washers"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.Catalog.Path(context.Background(), tt.id)
			if err != nil {
				t.Fatalf("path failed: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Path(%d) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestCatalogPathErrors(t *testing.T) {
	var calls int32
	client := newTreeTestClient(t, &calls)
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if _, err := client.Catalog.Path(ctx, 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for id 0, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no request for an invalid id, got %d", got)
	}
	if _, err := client.Catalog.Path(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown id, got %v", err)
	}
	// 1730 is under the removed root 1729.
	if _, err := client.Catalog.Path(ctx, 1730); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an id under root 1729, got %v", err)
	}
}

func TestCatalogLeaves(t *testing.T) {
	var calls int32
	client := newTreeTestClient(t, &calls)
	defer func() { _ = client.Close() }()

	tests := []struct {
		name string
		id   int
		want []int
	}{
		{"root", 30, []int{1141, 1142, 1199, 1200}},
		{"parent", 495, []int{1141, 1142}},
		{"leaf returns itself", 1142, []int{1142}},
		{"deep parent", 12, []int{1436}},
		{"root with repeated ids", 1569, []int{1585, 1586}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.Catalog.Leaves(context.Background(), tt.id)
			if err != nil {
				t.Fatalf("leaves failed: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Leaves(%d) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}

	if _, err := client.Catalog.Leaves(context.Background(), 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown id, got %v", err)
	}
	if _, err := client.Catalog.Leaves(context.Background(), -1); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for id -1, got %v", err)
	}
}

func TestCategoryLeafIDs(t *testing.T) {
	node := &Category{ID: 1, Children: []Category{
		{ID: 2, Children: []Category{{ID: 4}, {ID: 5}}},
		{ID: 3},
		{ID: 6, Children: []Category{{ID: 4}}},
	}}
	if got, want := node.LeafIDs(), []int{4, 5, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("LeafIDs() = %v, want %v", got, want)
	}

	var nilNode *Category
	if nilNode.LeafIDs() != nil || nilNode.IsLeaf() {
		t.Fatal("expected no leaves for a nil category")
	}
}

func TestCatalogChildCounts(t *testing.T) {
	parent := mustReadFixture(t, "catalog_onelevel_495.json")
	leaf := mustReadFixture(t, "catalog_onelevel_1199.json")

	client := newSearchTestClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != testOneLevelPath {
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		switch req.URL.Query().Get("catalogId") {
		case "495":
			return jsonResponse(http.StatusOK, parent), nil
		case "1199":
			return jsonResponse(http.StatusOK, leaf), nil
		default:
			// Live response for an unknown id.
			return jsonResponse(http.StatusOK, `{"code":200,"msg":null,"result":null,"ok":true}`), nil
		}
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	counts, err := client.Catalog.ChildCounts(ctx, 495)
	if err != nil {
		t.Fatalf("child counts failed: %v", err)
	}
	want := []CategoryCount{
		{ID: 1139, Name: "Aluminum - Polymer Capacitors", ProductCount: 11280},
		{ID: 1140, Name: "Aluminum Electrolytic Capacitors", ProductCount: 101445},
		{ID: 1141, Name: "Capacitor Networks, Arrays", ProductCount: 556},
	}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("ChildCounts(495) = %+v, want %+v", counts, want)
	}

	counts, err = client.Catalog.ChildCounts(ctx, 1199)
	if err != nil {
		t.Fatalf("child counts for a leaf failed: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected no child categories for a leaf, got %+v", counts)
	}

	if _, err := client.Catalog.ChildCounts(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown id, got %v", err)
	}
	if _, err := client.Catalog.ChildCounts(ctx, 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for id 0, got %v", err)
	}
}

func TestCatalogChildCountsIsCached(t *testing.T) {
	parent := mustReadFixture(t, "catalog_onelevel_495.json")
	cache := newRecordingCache()

	var calls int32
	client := newCachedTestClient(cache, func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(http.StatusOK, parent), nil
	})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		counts, err := client.Catalog.ChildCounts(ctx, 495)
		if err != nil {
			t.Fatalf("child counts failed: %v", err)
		}
		if len(counts) != 3 {
			t.Fatalf("expected 3 child categories, got %d", len(counts))
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one request, got %d", got)
	}
	if got := cache.ttls["catalog:onelevel:495"]; got != time.Minute {
		t.Fatalf("expected the search TTL, got %v", got)
	}
}
