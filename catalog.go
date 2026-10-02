package lcsc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// CatalogService handles category tree operations.
type CatalogService service

const (
	// catalogTreeTTL is the cache time of the category tree. The tree
	// changes rarely.
	catalogTreeTTL = 24 * time.Hour

	// catalogTreeCacheKey is the cache key of the category tree. The tree
	// does not depend on the currency.
	catalogTreeCacheKey = "catalog:tree"

	// mroRootCategoryID is the id of the root category "Maintenance,
	// Repair & Operations". Its subtree repeats categories of other roots
	// under different ids, so [CatalogService.Tree] removes it.
	mroRootCategoryID = 1729
)

// Category is a node of the LCSC category tree. The id is the same id
// that products send in [Product.WmCatalogID].
type Category struct {
	// ID is the category id (categoryId).
	ID int `json:"categoryId"`

	// Name is the English category name.
	Name string `json:"categoryNameEn"`

	// ParentID is the id of the parent category. It is 0 for a root
	// category. The client sets it from the tree structure.
	ParentID int `json:"parentId"`

	// Level is the depth of the category in the tree. A root category has
	// level 1. The client sets it from the tree structure.
	Level int `json:"level"`

	// Children holds the child categories in the server order. It is empty
	// for a leaf category.
	Children []Category `json:"childrenList"`
}

// IsLeaf reports whether the category has no child categories. Only leaf
// ids select products in [Filter.CatalogIDs].
func (c *Category) IsLeaf() bool {
	return c != nil && len(c.Children) == 0
}

// LeafIDs returns the ids of the leaf categories under c, in tree order.
// For a leaf category, it returns the id of c. It returns each id only one
// time.
func (c *Category) LeafIDs() []int {
	if c == nil {
		return nil
	}
	var ids []int
	seen := map[int]bool{}
	var walk func(n *Category)
	walk = func(n *Category) {
		if len(n.Children) == 0 {
			if !seen[n.ID] {
				seen[n.ID] = true
				ids = append(ids, n.ID)
			}
			return
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(c)
	return ids
}

// CategoryRef is the id and the English name of a category.
type CategoryRef struct {
	ID   int    `json:"catalogId"`
	Name string `json:"catalogNameEn"`
}

// CategoryCount is a child category with its number of products.
type CategoryCount struct {
	ID   int    `json:"catalogId"`
	Name string `json:"catalogNameEn"`

	// ProductCount is the number of products in the category.
	ProductCount int `json:"productNum"`
}

type catalogOneLevelWrapper struct {
	CatalogID     int             `json:"catalogId"`
	ChildCatelogs []CategoryCount `json:"childCatelogs"`
}

// Tree returns the LCSC category tree from /product/category/tree. The
// roots and the children are in the server order.
//
// Tree removes the root category "Maintenance, Repair & Operations" (id
// 1729) and its subtree. That subtree repeats categories of other roots
// under different ids. Some ids occur two times in the remaining tree (for
// example 1585 and 1586).
//
// The client caches the tree for 24 hours. Each call returns a new copy,
// so the caller can change it. When caching is disabled, each call sends a
// request. The tree response is about 400 KB.
func (s *CatalogService) Tree(ctx context.Context) ([]Category, error) {
	client := s.client
	if client.cacheConfig.Enabled && client.cache != nil {
		if cached, ok := client.cache.Get(catalogTreeCacheKey); ok {
			var tree []Category
			if err := json.Unmarshal(cached, &tree); err == nil && len(tree) > 0 {
				return tree, nil
			}
		}
	}

	var raw []Category
	if err := client.do(ctx, http.MethodGet, "/product/category/tree", nil, nil, &raw); err != nil {
		return nil, err
	}

	tree := make([]Category, 0, len(raw))
	for i := range raw {
		if raw[i].ID == mroRootCategoryID {
			continue
		}
		tree = append(tree, raw[i])
	}
	if len(tree) == 0 {
		return nil, fmt.Errorf("%w: empty category tree", ErrNotFound)
	}
	setCategoryParents(tree, 0, 1)

	if client.cacheConfig.Enabled && client.cache != nil {
		if data, err := json.Marshal(tree); err == nil {
			client.cache.Set(catalogTreeCacheKey, data, catalogTreeTTL)
		}
	}

	return tree, nil
}

// Path returns the categories from the root category down to the category
// with the given id. The last entry is the category itself. When the id
// occurs more than one time in the tree, Path uses the first one in tree
// order. Path returns [ErrNotFound] for an id that is not in the tree.
//
// Path gets the tree from [CatalogService.Tree].
func (s *CatalogService) Path(ctx context.Context, id int) ([]CategoryRef, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: category id must be positive, got %d", ErrInvalidRequest, id)
	}
	tree, err := s.Tree(ctx)
	if err != nil {
		return nil, err
	}
	path := findCategoryPath(tree, id, nil)
	if path == nil {
		return nil, fmt.Errorf("%w: category %d", ErrNotFound, id)
	}
	return path, nil
}

// Leaves returns the ids of the leaf categories under the category with the
// given id, in tree order. For a leaf category, it returns the id itself.
// Use the result in [Filter.CatalogIDs], because LCSC finds no products for
// the id of a parent category. Leaves returns [ErrNotFound] for an id that
// is not in the tree.
//
// Leaves gets the tree from [CatalogService.Tree].
func (s *CatalogService) Leaves(ctx context.Context, id int) ([]int, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: category id must be positive, got %d", ErrInvalidRequest, id)
	}
	tree, err := s.Tree(ctx)
	if err != nil {
		return nil, err
	}
	node := findCategory(tree, id)
	if node == nil {
		return nil, fmt.Errorf("%w: category %d", ErrNotFound, id)
	}
	return node.LeafIDs(), nil
}

// ChildCounts returns the child categories of a parent category with
// their numbers of products. It uses /product/catalog/menu/onelevel. For a
// leaf category, it returns an empty list and no error. It returns
// [ErrNotFound] when LCSC does not know the id.
//
// The client caches the response for CacheConfig.SearchTTL.
func (s *CatalogService) ChildCounts(ctx context.Context, parentID int) ([]CategoryCount, error) {
	if parentID <= 0 {
		return nil, fmt.Errorf("%w: category id must be positive, got %d", ErrInvalidRequest, parentID)
	}

	client := s.client
	cacheKey := "catalog:onelevel:" + strconv.Itoa(parentID)
	if client.cacheConfig.Enabled && client.cache != nil {
		if cached, ok := client.cache.Get(cacheKey); ok {
			var counts []CategoryCount
			if err := json.Unmarshal(cached, &counts); err == nil {
				return counts, nil
			}
		}
	}

	params := url.Values{}
	params.Set("catalogId", strconv.Itoa(parentID))

	// LCSC sends result null for an unknown id. A leaf id gives the
	// breadcrumb of the leaf and childCatelogs null.
	var wrapper *catalogOneLevelWrapper
	if err := client.do(ctx, http.MethodGet, "/product/catalog/menu/onelevel", params, nil, &wrapper); err != nil {
		return nil, err
	}
	if wrapper == nil {
		return nil, fmt.Errorf("%w: category %d", ErrNotFound, parentID)
	}
	counts := wrapper.ChildCatelogs

	if client.cacheConfig.Enabled && client.cache != nil {
		if data, err := json.Marshal(counts); err == nil {
			client.cache.Set(cacheKey, data, client.cacheConfig.SearchTTL)
		}
	}

	return counts, nil
}

// setCategoryParents sets ParentID and Level for each category in nodes
// and in their subtrees.
func setCategoryParents(nodes []Category, parentID, level int) {
	for i := range nodes {
		nodes[i].ParentID = parentID
		nodes[i].Level = level
		setCategoryParents(nodes[i].Children, nodes[i].ID, level+1)
	}
}

// findCategory returns the first category with the id in tree order, or
// nil.
func findCategory(nodes []Category, id int) *Category {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
		if found := findCategory(nodes[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}

// findCategoryPath returns the path from a root down to the first category
// with the id in tree order, or nil.
func findCategoryPath(nodes []Category, id int, prefix []CategoryRef) []CategoryRef {
	for i := range nodes {
		path := append(append([]CategoryRef(nil), prefix...), CategoryRef{ID: nodes[i].ID, Name: nodes[i].Name})
		if nodes[i].ID == id {
			return path
		}
		if found := findCategoryPath(nodes[i].Children, id, path); found != nil {
			return found
		}
	}
	return nil
}
