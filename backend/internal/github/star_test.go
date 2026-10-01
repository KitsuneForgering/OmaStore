package github

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
)

func TestStar(t *testing.T) {
	var mu sync.Mutex
	starred := map[string]bool{}
	mux := http.NewServeMux()
	mux.HandleFunc("/user/starred/{owner}/{repo}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		name := r.PathValue("owner") + "/" + r.PathValue("repo")
		if name == "acme/locked" {
			http.Error(w, `{"message":"Resource not accessible by personal access token"}`, http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if !starred[name] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
		case http.MethodPut:
			starred[name] = true
		case http.MethodDelete:
			delete(starred, name)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	c := newTest(t, mux)
	ctx := context.Background()

	if s, err := c.IsStarred(ctx, "acme/photo"); err != nil || s {
		t.Fatalf("IsStarred before = %v, %v", s, err)
	}
	if err := c.SetStarred(ctx, "acme/photo", true); err != nil {
		t.Fatal(err)
	}
	if s, err := c.IsStarred(ctx, "acme/photo"); err != nil || !s {
		t.Fatalf("IsStarred after star = %v, %v", s, err)
	}
	if err := c.SetStarred(ctx, "acme/photo", false); err != nil {
		t.Fatal(err)
	}
	if s, _ := c.IsStarred(ctx, "acme/photo"); s {
		t.Error("still starred after unstar")
	}
	if err := c.SetStarred(ctx, "acme/locked", true); !errors.Is(err, ErrStarForbidden) {
		t.Errorf("forbidden: err = %v", err)
	}
	if err := c.SetStarred(ctx, "not a repo", true); err == nil {
		t.Error("invalid name accepted")
	}
}

func TestStarNeedsToken(t *testing.T) {
	anon, err := New(Options{BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := anon.IsStarred(context.Background(), "acme/photo"); !errors.Is(err, ErrNoToken) {
		t.Errorf("IsStarred: err = %v", err)
	}
	if err := anon.SetStarred(context.Background(), "acme/photo", true); !errors.Is(err, ErrNoToken) {
		t.Errorf("SetStarred: err = %v", err)
	}
	if anon.Requests() != 0 {
		t.Errorf("anonymous client made %d requests", anon.Requests())
	}
}
