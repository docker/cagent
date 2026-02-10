package oca

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want 'Bearer test-token'", auth)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "model-1", "owned_by": "oracle"},
				{"id": "model-2", "owned_by": "oracle"},
			},
		})
	}))
	defer server.Close()

	models, err := FetchModels(context.Background(), server.URL+"/", "test-token")
	if err != nil {
		t.Fatalf("FetchModels() error = %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ID != "model-1" {
		t.Errorf("models[0].ID = %q, want %q", models[0].ID, "model-1")
	}
	if models[1].ID != "model-2" {
		t.Errorf("models[1].ID = %q, want %q", models[1].ID, "model-2")
	}
}

func TestFetchModels_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	}))
	defer server.Close()

	_, err := FetchModels(context.Background(), server.URL+"/", "bad-token")
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
}
