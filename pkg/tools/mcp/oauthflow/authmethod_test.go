package oauthflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestRegisterClientHonorsServerAuthMethod(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"cid","client_secret":"sec","token_endpoint_auth_method":"client_secret_basic"}`))
	}))
	t.Cleanup(srv.Close)

	id, secret, method, err := RegisterClientWithClient(context.Background(), srv.Client(), &AuthorizationServerMetadata{
		RegistrationEndpoint:              srv.URL,
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post"},
	}, "http://127.0.0.1/cb", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id != "cid" || secret != "sec" || method != "client_secret_basic" {
		t.Fatalf("got id=%q secret=%q method=%q", id, secret, method)
	}
	if got["token_endpoint_auth_method"] != "client_secret_post" {
		t.Fatalf("registration asked for %v, want client_secret_post", got["token_endpoint_auth_method"])
	}
}

func TestRegisterClientAsksForBasicWhenThatIsAll(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"cid","client_secret":"sec","token_endpoint_auth_method":"client_secret_basic"}`))
	}))
	t.Cleanup(srv.Close)

	_, _, method, err := RegisterClientWithClient(context.Background(), srv.Client(), &AuthorizationServerMetadata{
		RegistrationEndpoint:              srv.URL,
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	}, "http://127.0.0.1/cb", nil)
	if err != nil {
		t.Fatal(err)
	}
	if method != "client_secret_basic" {
		t.Fatalf("method = %q", method)
	}
	if got["token_endpoint_auth_method"] != "client_secret_basic" {
		t.Fatalf("registration asked for %v", got["token_endpoint_auth_method"])
	}
}

func TestRegisterClientDefaultsToPost(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"cid","client_secret":"sec"}`))
	}))
	t.Cleanup(srv.Close)

	_, _, method, err := RegisterClientWithClient(context.Background(), srv.Client(), &AuthorizationServerMetadata{
		RegistrationEndpoint: srv.URL,
	}, "http://127.0.0.1/cb", nil)
	if err != nil {
		t.Fatal(err)
	}
	if method != "client_secret_post" {
		t.Fatalf("method = %q, want client_secret_post", method)
	}
	if got["token_endpoint_auth_method"] != "client_secret_post" {
		t.Fatalf("registration asked for %v", got["token_endpoint_auth_method"])
	}
}

func TestExchangeUsesBasicWhenRegisteredThatWay(t *testing.T) {
	var form url.Values
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer"}`))
	}))
	t.Cleanup(srv.Close)

	tok, err := ExchangeCodeForTokenWithClient(context.Background(), srv.Client(), srv.URL, "code", "ver", "cid", "sec", "http://127.0.0.1/cb", "", "client_secret_basic")
	if err != nil {
		t.Fatal(err)
	}
	if tok.TokenEndpointAuthMethod != "client_secret_basic" {
		t.Fatalf("stored method %q", tok.TokenEndpointAuthMethod)
	}
	if form.Get("client_secret") != "" || form.Get("client_id") != "" {
		t.Fatalf("basic auth still put credentials in the form: %v", form)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("cid:sec"))
	if auth != want {
		t.Fatalf("Authorization = %q, want %q", auth, want)
	}
}

func TestExchangePostsSecretByDefault(t *testing.T) {
	var form url.Values
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer"}`))
	}))
	t.Cleanup(srv.Close)

	if _, err := ExchangeCodeForTokenWithClient(context.Background(), srv.Client(), srv.URL, "code", "ver", "cid", "sec", "http://127.0.0.1/cb", "", ""); err != nil {
		t.Fatal(err)
	}
	if form.Get("client_secret") != "sec" || form.Get("client_id") != "cid" {
		t.Fatalf("form = %v", form)
	}
	if auth != "" {
		t.Fatalf("unexpected Authorization %q", auth)
	}
}
