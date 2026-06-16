package schema

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleSchemaJSON = `{
  "nodes": [
    {
      "kind": "DctVCenter",
      "attributes": [
        {"name": "vcenter_name", "kind": "Text", "optional": false},
        {"name": "total_vcpu", "kind": "Number", "optional": true},
        {"name": "is_active", "kind": "Boolean", "optional": true}
      ]
    }
  ],
  "generics": []
}`

func TestFetchBuildsRegistry(t *testing.T) {
	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path + "?" + req.URL.RawQuery
		gotKey = req.Header.Get("X-INFRAHUB-KEY")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleSchemaJSON))
	}))
	defer srv.Close()

	reg, err := Fetch(context.Background(), srv.URL, "tok-123", "main")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if gotPath != "/api/schema?branch=main" {
		t.Errorf("requested %q, want /api/schema?branch=main", gotPath)
	}
	if gotKey != "tok-123" {
		t.Errorf("X-INFRAHUB-KEY = %q, want tok-123", gotKey)
	}

	a, ok := reg.Attribute("DctVCenter", "total_vcpu")
	if !ok || a.Kind != "Number" || !a.Optional {
		t.Errorf("total_vcpu = %+v ok=%v, want {Number true} true", a, ok)
	}
	if a, ok := reg.Attribute("DctVCenter", "vcenter_name"); !ok || a.Optional {
		t.Errorf("vcenter_name optional = %v ok=%v, want false true", a.Optional, ok)
	}
}

func TestFetchErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := Fetch(context.Background(), srv.URL, "tok", "main"); err == nil {
		t.Fatal("expected an error on HTTP 401, got nil")
	}
}

func TestFetchErrorsOnBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	if _, err := Fetch(context.Background(), srv.URL, "tok", "main"); err == nil {
		t.Fatal("expected an error on invalid JSON, got nil")
	}
}
