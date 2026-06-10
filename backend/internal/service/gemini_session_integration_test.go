package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

// TestGeminiSessionContinuousConversation
func TestGeminiSessionContinuousConversation(t *testing.T) {
	store := NewDigestSessionStore()
	groupID := int64(1)
	prefixHash := "test_prefix_hash"
	sessionUUID := "session-uuid-12345"
	accountID := int64(100)

	req1 := &antigravity.GeminiRequest{
		SystemInstruction: &antigravity.GeminiContent{
			Parts: []antigravity.GeminiPart{{Text: "You are a helpful assistant"}},
		},
		Contents: []antigravity.GeminiContent{
			{Role: "user", Parts: []antigravity.GeminiPart{{Text: "Hello, what's your name?"}}},
		},
	}
	chain1 := BuildGeminiDigestChain(req1)
	t.Logf("Round 1 chain: %s", chain1)

	_, _, _, found := store.Find(groupID, prefixHash, chain1)
	if found {
		t.Error("Round 1: should not find existing session")
	}

	//
	store.Save(groupID, prefixHash, chain1, sessionUUID, accountID, "")

	req2 := &antigravity.GeminiRequest{
		SystemInstruction: &antigravity.GeminiContent{
			Parts: []antigravity.GeminiPart{{Text: "You are a helpful assistant"}},
		},
		Contents: []antigravity.GeminiContent{
			{Role: "user", Parts: []antigravity.GeminiPart{{Text: "Hello, what's your name?"}}},
			{Role: "model", Parts: []antigravity.GeminiPart{{Text: "I'm Claude, nice to meet you!"}}},
			{Role: "user", Parts: []antigravity.GeminiPart{{Text: "What can you do?"}}},
		},
	}
	chain2 := BuildGeminiDigestChain(req2)
	t.Logf("Round 2 chain: %s", chain2)

	foundUUID, foundAccID, matchedChain, found := store.Find(groupID, prefixHash, chain2)
	if !found {
		t.Error("Round 2: should find session via prefix matching")
	}
	if foundUUID != sessionUUID {
		t.Errorf("Round 2: expected UUID %s, got %s", sessionUUID, foundUUID)
	}
	if foundAccID != accountID {
		t.Errorf("Round 2: expected accountID %d, got %d", accountID, foundAccID)
	}

	//
	store.Save(groupID, prefixHash, chain2, sessionUUID, accountID, matchedChain)

	req3 := &antigravity.GeminiRequest{
		SystemInstruction: &antigravity.GeminiContent{
			Parts: []antigravity.GeminiPart{{Text: "You are a helpful assistant"}},
		},
		Contents: []antigravity.GeminiContent{
			{Role: "user", Parts: []antigravity.GeminiPart{{Text: "Hello, what's your name?"}}},
			{Role: "model", Parts: []antigravity.GeminiPart{{Text: "I'm Claude, nice to meet you!"}}},
			{Role: "user", Parts: []antigravity.GeminiPart{{Text: "What can you do?"}}},
			{Role: "model", Parts: []antigravity.GeminiPart{{Text: "I can help with coding, writing, and more!"}}},
			{Role: "user", Parts: []antigravity.GeminiPart{{Text: "Great, help me write some Go code"}}},
		},
	}
	chain3 := BuildGeminiDigestChain(req3)
	t.Logf("Round 3 chain: %s", chain3)

	foundUUID, foundAccID, _, found = store.Find(groupID, prefixHash, chain3)
	if !found {
		t.Error("Round 3: should find session via prefix matching")
	}
	if foundUUID != sessionUUID {
		t.Errorf("Round 3: expected UUID %s, got %s", sessionUUID, foundUUID)
	}
	if foundAccID != accountID {
		t.Errorf("Round 3: expected accountID %d, got %d", accountID, foundAccID)
	}
}

// TestGeminiSessionDifferentConversations
func TestGeminiSessionDifferentConversations(t *testing.T) {
	store := NewDigestSessionStore()
	groupID := int64(1)
	prefixHash := "test_prefix_hash"

	req1 := &antigravity.GeminiRequest{
		Contents: []antigravity.GeminiContent{
			{Role: "user", Parts: []antigravity.GeminiPart{{Text: "Tell me about Go programming"}}},
		},
	}
	chain1 := BuildGeminiDigestChain(req1)
	store.Save(groupID, prefixHash, chain1, "session-1", 100, "")

	req2 := &antigravity.GeminiRequest{
		Contents: []antigravity.GeminiContent{
			{Role: "user", Parts: []antigravity.GeminiPart{{Text: "What's the weather today?"}}},
		},
	}
	chain2 := BuildGeminiDigestChain(req2)

	_, _, _, found := store.Find(groupID, prefixHash, chain2)
	if found {
		t.Error("Different conversations should not match")
	}
}

// TestGeminiSessionPrefixMatchingOrder
func TestGeminiSessionPrefixMatchingOrder(t *testing.T) {
	store := NewDigestSessionStore()
	groupID := int64(1)
	prefixHash := "test_prefix_hash"

	store.Save(groupID, prefixHash, "s:sys-u:q1", "session-round1", 1, "")
	store.Save(groupID, prefixHash, "s:sys-u:q1-m:a1", "session-round2", 2, "")
	store.Save(groupID, prefixHash, "s:sys-u:q1-m:a1-u:q2", "session-round3", 3, "")

	_, accID, _, found := store.Find(groupID, prefixHash, "s:sys-u:q1-m:a1-u:q2-m:a2")
	if !found {
		t.Error("Should find session")
	}
	if accID != 3 {
		t.Errorf("Should match longest prefix (account 3), got account %d", accID)
	}
}
