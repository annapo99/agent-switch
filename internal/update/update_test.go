package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{latest: "v0.1.1", current: "v0.1.0", want: true},
		{latest: "v0.2.0", current: "v0.1.9", want: true},
		{latest: "v1.0.0", current: "v1.0.0", want: false},
		{latest: "v1.0.0", current: "v1.0.1", want: false},
		{latest: "0.10.0", current: "v0.9.9", want: true},
	}

	for _, tt := range tests {
		if got := IsNewer(tt.latest, tt.current); got != tt.want {
			t.Fatalf("IsNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestCheckLatestFetchesAndCachesRelease(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintln(w, `{"tag_name":"v0.2.0"}`)
	}))
	defer server.Close()
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	cfg := Config{
		Home:           t.TempDir(),
		CurrentVersion: "v0.1.0",
		LatestURL:      server.URL,
		Now:            func() time.Time { return now },
		CacheTTL:       time.Hour,
	}

	first, err := CheckLatest(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CheckLatest(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !first.Available || first.Version != "v0.2.0" {
		t.Fatalf("first = %#v", first)
	}
	if !second.Available || second.Version != "v0.2.0" {
		t.Fatalf("second = %#v", second)
	}
	if calls != 1 {
		t.Fatalf("latest endpoint calls = %d, want 1", calls)
	}
}

func TestCheckLatestSkipsDevelopmentVersion(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintln(w, `{"tag_name":"v0.2.0"}`)
	}))
	defer server.Close()
	cfg := Config{
		Home:           t.TempDir(),
		CurrentVersion: "dev",
		LatestURL:      server.URL,
	}

	info, err := CheckLatest(context.Background(), cfg)

	if err != nil {
		t.Fatal(err)
	}
	if info.Available || info.Version != "" || calls != 0 {
		t.Fatalf("info=%#v calls=%d", info, calls)
	}
}

func TestUpdateInstallsVerifiedRelease(t *testing.T) {
	archive := archiveWithAGS(t, []byte("new ags binary"))
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintln(w, `{"tag_name":"v0.2.0"}`)
		case "/download/v0.2.0/ags_darwin_arm64.tar.gz":
			_, _ = w.Write(archive)
		case "/download/v0.2.0/checksums.txt":
			fmt.Fprintf(w, "%x  ags_darwin_arm64.tar.gz\n", sum)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	executable := filepath.Join(t.TempDir(), "ags")
	if err := os.WriteFile(executable, []byte("old ags binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Home:            t.TempDir(),
		CurrentVersion:  "v0.1.0",
		ExecutablePath:  executable,
		LatestURL:       server.URL + "/latest",
		DownloadBaseURL: server.URL + "/download/v0.2.0",
		OS:              "darwin",
		Arch:            "arm64",
	}

	result, err := Update(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Updated || result.Version != "v0.2.0" {
		t.Fatalf("result = %#v", result)
	}
	if string(data) != "new ags binary" {
		t.Fatalf("installed binary = %q", string(data))
	}
}

func TestUpdateRejectsChecksumMismatch(t *testing.T) {
	archive := archiveWithAGS(t, []byte("new ags binary"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintln(w, `{"tag_name":"v0.2.0"}`)
		case "/download/v0.2.0/ags_darwin_arm64.tar.gz":
			_, _ = w.Write(archive)
		case "/download/v0.2.0/checksums.txt":
			fmt.Fprintln(w, "0000000000000000000000000000000000000000000000000000000000000000  ags_darwin_arm64.tar.gz")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	executable := filepath.Join(t.TempDir(), "ags")
	if err := os.WriteFile(executable, []byte("old ags binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Home:            t.TempDir(),
		CurrentVersion:  "v0.1.0",
		ExecutablePath:  executable,
		LatestURL:       server.URL + "/latest",
		DownloadBaseURL: server.URL + "/download/v0.2.0",
		OS:              "darwin",
		Arch:            "arm64",
	}

	_, err := Update(context.Background(), cfg)

	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old ags binary" {
		t.Fatalf("binary should not be replaced, got %q", string(data))
	}
}

func archiveWithAGS(t *testing.T, content []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "ags", Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
