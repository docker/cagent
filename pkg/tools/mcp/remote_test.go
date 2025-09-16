package mcp

import (
	"testing"

	"github.com/mark3labs/mcp-go/client"
)

func TestGlobalTokenManager(t *testing.T) {
	// Test that different URLs get different token stores
	url1 := "http://example1.com"
	url2 := "http://example2.com"

	store1 := GetTokenStore(url1)
	store2 := GetTokenStore(url2)

	// They should be different instances
	if store1 == store2 {
		t.Error("Expected different token stores for different URLs")
	}

	// Test that same URL returns same token store
	store1Again := GetTokenStore(url1)
	if store1 != store1Again {
		t.Error("Expected same token store for same URL")
	}

	// Verify they are actual MemoryTokenStore instances
	if _, ok := store1.(*client.MemoryTokenStore); !ok {
		t.Error("Expected store1 to be a MemoryTokenStore")
	}
	if _, ok := store2.(*client.MemoryTokenStore); !ok {
		t.Error("Expected store2 to be a MemoryTokenStore")
	}
}

func TestGlobalTokenManagerConcurrency(t *testing.T) {
	url := "http://concurrent-test.com"

	// Test concurrent access
	done := make(chan bool, 10)
	stores := make([]client.TokenStore, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			stores[index] = GetTokenStore(url)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// All stores should be the same instance
	firstStore := stores[0]
	for i := 1; i < 10; i++ {
		if stores[i] != firstStore {
			t.Errorf("Expected all stores to be the same instance, but store[%d] differs", i)
		}
	}
}
