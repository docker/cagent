package mcp

import (
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/stretchr/testify/assert"
)

func TestGlobalTokenManager(t *testing.T) {
	// Test that different URLs get different token stores
	url1 := "http://example1.com"
	url2 := "http://example2.com"

	store1 := GetTokenStore(url1)
	store2 := GetTokenStore(url2)

	// They should be different instances
	assert.NotSame(t, store1, store2, "Expected different token stores for different URLs")

	// Test that same URL returns same token store
	store1Again := GetTokenStore(url1)
	assert.Equal(t, store1, store1Again, "Expected same token store for same URL")

	// Verify they are actual MemoryTokenStore instances
	_, ok := store1.(*client.MemoryTokenStore)
	assert.True(t, ok, "Expected store1 to be a MemoryTokenStore")
	_, ok = store2.(*client.MemoryTokenStore)
	assert.True(t, ok, "Expected store2 to be a MemoryTokenStore")
}

func TestGlobalTokenManagerConcurrency(t *testing.T) {
	url := "http://concurrent-test.com"

	// Test concurrent access
	done := make(chan bool, 10)
	stores := make([]client.TokenStore, 10)

	for i := range 10 {
		go func(index int) {
			stores[index] = GetTokenStore(url)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for range 10 {
		<-done
	}

	// All stores should be the same instance
	firstStore := stores[0]
	for i := 1; i < 10; i++ {
		assert.Equal(t, firstStore, stores[i], "Expected all stores to be the same instance")
	}
}
