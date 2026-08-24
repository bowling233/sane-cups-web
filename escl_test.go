package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestExecuteESCLScanWithUnverifiedSelfSignedCertificate(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/eSCL/ScanJobs":
			if u, p, ok := r.BasicAuth(); !ok || u != "user" || p != "pass" {
				t.Error("missing authentication")
			}
			w.Header().Set("Location", server.URL+"/eSCL/ScanJobs/1")
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/NextDocument"):
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("image-data"))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	out := t.TempDir() + "/scan.png"
	s := &ScanConfig{Endpoint: server.URL + "/eSCL", TLS: TLSConfig{Verify: false}, Auth: DeviceAuth{Type: "basic", Username: "user", Password: "pass"}}
	if err := executeESCLScan(context.Background(), s, 300, "Color", "png", out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != "image-data" {
		t.Fatalf("output %q, %v", data, err)
	}
}

func TestESCLVerifiedTLSRejectsSelfSignedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	c, err := esclClient(&ScanConfig{TLS: TLSConfig{Verify: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Get(server.URL); err == nil {
		t.Fatal("self-signed certificate was accepted")
	}
}
