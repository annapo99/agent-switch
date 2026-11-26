package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchClaudeLoginIdentityReadsTheProfileAndIgnoresRedirects(t *testing.T) {
	var redirected bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/profile":
			if r.Header.Get("Authorization") != "Bearer token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"account":{"uuid":"account-1","email_address":"a@example.com"},"organization":{"uuid":"org-1","name":"Org"}}`))
		case "/moved":
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
		default:
			redirected = true
		}
	}))
	defer server.Close()
	original := ClaudeProfileURL
	defer func() { ClaudeProfileURL = original }()

	ClaudeProfileURL = server.URL + "/profile"
	got, err := FetchClaudeLoginIdentity("token")
	if err != nil || got.AccountUUID != "account-1" || got.OrganizationUUID != "org-1" {
		t.Fatalf("got=%+v err=%v", got, err)
	}

	ClaudeProfileURL = server.URL + "/moved"
	if _, err := FetchClaudeLoginIdentity("token"); err == nil || redirected {
		t.Fatalf("redirect err=%v followed=%v", err, redirected)
	}
}
