package main

import "testing"

func TestParseByteSize(t *testing.T) {
	n, err := parseByteSize("500MiB")
	if err != nil || n != 500*(1<<20) {
		t.Fatalf("got %d, %v", n, err)
	}
	if _, err := parseByteSize("large"); err == nil {
		t.Fatal("invalid size accepted")
	}
}
