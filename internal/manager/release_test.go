package manager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type releaseRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip releaseRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestDownloadReportsVerifiedByteProgress(t *testing.T) {
	content := []byte("verified release fixture")
	digest := sha256.Sum256(content)

	mgr, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr.client.Transport = releaseRoundTripper(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Body:          io.NopCloser(bytes.NewReader(content)),
			ContentLength: int64(len(content)),
			Header:        make(http.Header),
		}, nil
	})
	asset := releaseAsset{
		Name:        "prometheus-test.tar.gz",
		DownloadURL: "https://example.invalid/prometheus-test.tar.gz",
		Digest:      "sha256:" + fmt.Sprintf("%x", digest),
	}
	destination := filepath.Join(t.TempDir(), asset.Name)
	var downloaded, total int64
	var done bool
	if err := mgr.download(context.Background(), asset, destination, func(current, size int64, complete bool) {
		downloaded, total, done = current, size, complete
	}); err != nil {
		t.Fatal(err)
	}

	if downloaded != int64(len(content)) || total != int64(len(content)) || !done {
		t.Fatalf("progress = %d/%d done=%v, want %d/%d done=true", downloaded, total, done, len(content), len(content))
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("downloaded content = %q, want %q", got, content)
	}
}
