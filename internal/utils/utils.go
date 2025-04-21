package utils

import (
	"hash/fnv"
	"sync"
)

func Hash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))

	return h.Sum64()
}

func LenSyncMap(m *sync.Map) int {
	var i int
	m.Range(func(k, v interface{}) bool {
		i++
		return true
	})
	return i
}
