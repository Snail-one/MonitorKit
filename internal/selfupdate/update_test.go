package selfupdate

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRunDownloadsAndExecutesInstaller(t *testing.T) {
	client := testClient(http.StatusOK, "#!/bin/sh\nprintf 'called:%s\\n' \"${1:-install}\"\n")
	var output, diagnostics bytes.Buffer
	err := run(context.Background(), client, "https://example.invalid/install.sh", strings.NewReader(""), &output, &diagnostics, "--uninstall")
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != "called:--uninstall\n" {
		t.Fatalf("output = %q", output.String())
	}
	if !strings.Contains(diagnostics.String(), "下载安装管理脚本") {
		t.Fatalf("diagnostics = %q", diagnostics.String())
	}
}

func TestRunRejectsUnexpectedContent(t *testing.T) {
	client := testClient(http.StatusOK, "not a shell script")
	err := run(context.Background(), client, "https://example.invalid/install.sh", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "不是有效") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunReportsHTTPFailure(t *testing.T) {
	client := testClient(http.StatusNotFound, "missing")
	err := run(context.Background(), client, "https://example.invalid/install.sh", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testClient(status int, body string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    status,
			Status:        http.StatusText(status),
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
			Header:        make(http.Header),
			Request:       request,
		}, nil
	})}
}
