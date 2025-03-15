package symcache

import (
	"github.com/pkg/errors"
	"sync"
)

var (
	ErrKeyNotFound = errors.New("key not found")
)

type addr uint64
type name string

type SymCache struct {
	syms map[addr]name
	lock sync.RWMutex
}

func NewSymCache() *SymCache {
	cache := new(SymCache)
	cache.syms = make(map[addr]name)

	return cache
}

func (s *SymCache) Set(sym string, ip uint64) {
	defer s.lock.Unlock()
	s.lock.Lock()
	s.syms[addr(ip)] = name(sym)
}

func (s *SymCache) Get(ip uint64) (string, error) {
	defer s.lock.RUnlock()
	s.lock.RLock()
	sym, ok := s.syms[addr(ip)]
	if !ok {
		return "", ErrKeyNotFound
	}

	return string(sym), nil
}
