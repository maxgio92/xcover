package symcache_test

import (
	"testing"

	"github.com/maxgio92/utrace/pkg/symcache"
)

func TestNewSymCache(t *testing.T) {
	cache := symcache.NewSymCache()
	if cache == nil {
		t.Fatal("NewSymCache returned nil")
	}
}

func TestSet(t *testing.T) {
	myfunc := "foo"
	cache := symcache.NewSymCache()
	cache.Set(myfunc, 1234)
	name, _ := cache.Get(1234)
	if name != myfunc {
		t.Fatal("TestAdd returned wrong value")
	}
}

func TestGet(t *testing.T) {
	cache := symcache.NewSymCache()
	name, err := cache.Get(1234)
	if name != "" {
		t.Fatal("TestGet returned wrong value")
	}
	if err == nil {
		t.Fatal("TestGet did not return error")
	}
}
