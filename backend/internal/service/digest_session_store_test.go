//go:build unit

package service

import (
	"fmt"
	"sync"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDigestSessionStore_SaveAndFind(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "s:a1-u:b2-m:c3", "uuid-1", 100, "")

	uuid, accountID, _, found := store.Find(1, "prefix", "s:a1-u:b2-m:c3")
	require.True(t, found)
	assert.Equal(t, "uuid-1", uuid)
	assert.Equal(t, int64(100), accountID)
}

func TestDigestSessionStore_PrefixMatch(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "u:a-m:b", "uuid-short", 10, "")

	uuid, accountID, matchedChain, found := store.Find(1, "prefix", "u:a-m:b-u:c-m:d")
	require.True(t, found)
	assert.Equal(t, "uuid-short", uuid)
	assert.Equal(t, int64(10), accountID)
	assert.Equal(t, "u:a-m:b", matchedChain)
}

func TestDigestSessionStore_LongestPrefixMatch(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "u:a", "uuid-1", 1, "")
	store.Save(1, "prefix", "u:a-m:b", "uuid-2", 2, "")
	store.Save(1, "prefix", "u:a-m:b-u:c", "uuid-3", 3, "")

	// "u:a-m:b-u:c"（
	uuid, accountID, _, found := store.Find(1, "prefix", "u:a-m:b-u:c-m:d-u:e")
	require.True(t, found)
	assert.Equal(t, "uuid-3", uuid)
	assert.Equal(t, int64(3), accountID)

	// "u:a-m:b"
	uuid, accountID, _, found = store.Find(1, "prefix", "u:a-m:b-u:x")
	require.True(t, found)
	assert.Equal(t, "uuid-2", uuid)
	assert.Equal(t, int64(2), accountID)
}

func TestDigestSessionStore_SaveDeletesOldChain(t *testing.T) {
	store := NewDigestSessionStore()

	// "u:a-m:b"
	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "")

	//
	store.Save(1, "prefix", "u:a-m:b-u:c-m:d", "uuid-1", 100, "u:a-m:b")

	// "u:a-m:b"
	_, _, _, found := store.Find(1, "prefix", "u:a-m:b")
	assert.False(t, found, "old chain should be deleted")

	uuid, accountID, _, found := store.Find(1, "prefix", "u:a-m:b-u:c-m:d")
	require.True(t, found)
	assert.Equal(t, "uuid-1", uuid)
	assert.Equal(t, int64(100), accountID)
}

func TestDigestSessionStore_DifferentSessionsNoInterference(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "s:sys-u:user1", "uuid-1", 100, "")
	store.Save(1, "prefix", "s:sys-u:user2", "uuid-2", 200, "")

	uuid, accountID, _, found := store.Find(1, "prefix", "s:sys-u:user1-m:reply1")
	require.True(t, found)
	assert.Equal(t, "uuid-1", uuid)
	assert.Equal(t, int64(100), accountID)

	uuid, accountID, _, found = store.Find(1, "prefix", "s:sys-u:user2-m:reply2")
	require.True(t, found)
	assert.Equal(t, "uuid-2", uuid)
	assert.Equal(t, int64(200), accountID)
}

func TestDigestSessionStore_NoMatch(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "")

	//
	_, _, _, found := store.Find(1, "prefix", "u:x-m:y")
	assert.False(t, found)
}

func TestDigestSessionStore_DifferentPrefixHash(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix1", "u:a-m:b", "uuid-1", 100, "")

	//
	_, _, _, found := store.Find(1, "prefix2", "u:a-m:b")
	assert.False(t, found)
}

func TestDigestSessionStore_DifferentGroupID(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "")

	//
	_, _, _, found := store.Find(2, "prefix", "u:a-m:b")
	assert.False(t, found)
}

func TestDigestSessionStore_EmptyDigestChain(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "", "uuid-1", 100, "")
	_, _, _, found := store.Find(1, "prefix", "")
	assert.False(t, found)
}

func TestDigestSessionStore_TTLExpiration(t *testing.T) {
	store := &DigestSessionStore{
		cache: gocache.New(100*time.Millisecond, 50*time.Millisecond),
	}

	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "")

	_, _, _, found := store.Find(1, "prefix", "u:a-m:b")
	require.True(t, found)

	time.Sleep(300 * time.Millisecond)

	_, _, _, found = store.Find(1, "prefix", "u:a-m:b")
	assert.False(t, found)
}

func TestDigestSessionStore_ConcurrentSafety(t *testing.T) {
	store := NewDigestSessionStore()

	var wg sync.WaitGroup
	const goroutines = 50
	const operations = 100

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			prefix := fmt.Sprintf("prefix-%d", id%5)
			for i := 0; i < operations; i++ {
				chain := fmt.Sprintf("u:%d-m:%d", id, i)
				uuid := fmt.Sprintf("uuid-%d-%d", id, i)
				store.Save(1, prefix, chain, uuid, int64(id), "")
				store.Find(1, prefix, chain)
			}
		}(g)
	}
	wg.Wait()
}

func TestDigestSessionStore_MultipleSessions(t *testing.T) {
	store := NewDigestSessionStore()

	sessions := []struct {
		chain     string
		uuid      string
		accountID int64
	}{
		{"u:session1", "uuid-1", 1},
		{"u:session2-m:reply2", "uuid-2", 2},
		{"u:session3-m:reply3-u:msg3", "uuid-3", 3},
	}

	for _, sess := range sessions {
		store.Save(1, "prefix", sess.chain, sess.uuid, sess.accountID, "")
	}

	for _, sess := range sessions {
		uuid, accountID, _, found := store.Find(1, "prefix", sess.chain)
		require.True(t, found, "should find session: %s", sess.chain)
		assert.Equal(t, sess.uuid, uuid)
		assert.Equal(t, sess.accountID, accountID)
	}

	uuid, accountID, _, found := store.Find(1, "prefix", "u:session2-m:reply2-u:newmsg")
	require.True(t, found)
	assert.Equal(t, "uuid-2", uuid)
	assert.Equal(t, int64(2), accountID)
}

func TestDigestSessionStore_Performance1000Sessions(t *testing.T) {
	store := NewDigestSessionStore()

	//
	for i := 0; i < 1000; i++ {
		chain := fmt.Sprintf("s:sys-u:user%d-m:reply%d", i, i)
		store.Save(1, "prefix", chain, fmt.Sprintf("uuid-%d", i), int64(i), "")
	}

	start := time.Now()
	const lookups = 10000
	for i := 0; i < lookups; i++ {
		idx := i % 1000
		chain := fmt.Sprintf("s:sys-u:user%d-m:reply%d-u:newmsg", idx, idx)
		_, _, _, found := store.Find(1, "prefix", chain)
		assert.True(t, found)
	}
	elapsed := time.Since(start)
	t.Logf("%d lookups in %v (%.0f ns/op)", lookups, elapsed, float64(elapsed.Nanoseconds())/lookups)
}

func TestDigestSessionStore_FindReturnsMatchedChain(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "u:a-m:b-u:c", "uuid-1", 100, "")

	_, _, matchedChain, found := store.Find(1, "prefix", "u:a-m:b-u:c")
	require.True(t, found)
	assert.Equal(t, "u:a-m:b-u:c", matchedChain)

	_, _, matchedChain, found = store.Find(1, "prefix", "u:a-m:b-u:c-m:d-u:e")
	require.True(t, found)
	assert.Equal(t, "u:a-m:b-u:c", matchedChain)
}

func TestDigestSessionStore_CacheItemCountStable(t *testing.T) {
	store := NewDigestSessionStore()

	//
	//
	for conv := 0; conv < 100; conv++ {
		var prevMatchedChain string
		for round := 0; round < 10; round++ {
			chain := fmt.Sprintf("s:sys-u:user%d", conv)
			for r := 0; r < round; r++ {
				chain += fmt.Sprintf("-m:a%d-u:q%d", r, r+1)
			}
			uuid := fmt.Sprintf("uuid-conv%d", conv)

			_, _, matched, _ := store.Find(1, "prefix", chain)
			store.Save(1, "prefix", chain, uuid, int64(conv), matched)
			prevMatchedChain = matched
			_ = prevMatchedChain
		}
	}

	// 100 × 1 key/= ≤ 100
	// ×10=1000
	itemCount := store.cache.ItemCount()
	assert.LessOrEqual(t, itemCount, 100, "cache should have at most 100 items (1 per conversation), got %d", itemCount)
	t.Logf("Cache item count after 100 conversations × 10 rounds: %d", itemCount)
}

func TestDigestSessionStore_TTLPreventsUnboundedGrowth(t *testing.T) {
	//
	store := &DigestSessionStore{
		cache: gocache.New(100*time.Millisecond, 50*time.Millisecond),
	}

	//
	for i := 0; i < 500; i++ {
		chain := fmt.Sprintf("u:user%d", i)
		store.Save(1, "prefix", chain, fmt.Sprintf("uuid-%d", i), int64(i), "")
	}

	assert.Equal(t, 500, store.cache.ItemCount())

	// +
	time.Sleep(300 * time.Millisecond)

	assert.Equal(t, 0, store.cache.ItemCount(), "all items should be expired and cleaned up")
}

func TestDigestSessionStore_SaveSameChainNoDelete(t *testing.T) {
	store := NewDigestSessionStore()

	//
	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "")

	// == digestChain，
	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "u:a-m:b")

	uuid, accountID, _, found := store.Find(1, "prefix", "u:a-m:b")
	require.True(t, found)
	assert.Equal(t, "uuid-1", uuid)
	assert.Equal(t, int64(100), accountID)
}
