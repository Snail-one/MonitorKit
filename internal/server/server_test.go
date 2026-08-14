package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Snail-one/Snailbash/internal/manager"
)

type fakeManager struct{}

func (fakeManager) Install(_ context.Context, name, version string) (manager.Status, error) {
	return manager.Status{Name: name, Installed: true, Version: version, ServiceState: "active"}, nil
}
func (fakeManager) Uninstall(_ context.Context, _ string, _ bool) error { return nil }
func (fakeManager) Status(_ context.Context, name string) (manager.Status, error) {
	return manager.Status{Name: name, Installed: true, ServiceState: "active"}, nil
}

func TestNewRequiresTokenForPublicListen(t *testing.T) {
	if _, err := New(fakeManager{}, "", "0.0.0.0:8088"); err == nil {
		t.Fatal("New accepted public listen address without a token")
	}
	if _, err := New(fakeManager{}, "", "127.0.0.1:8088"); err != nil {
		t.Fatalf("New rejected loopback address: %v", err)
	}
}

func TestBearerAuthentication(t *testing.T) {
	handler, err := New(fakeManager{}, "secret", "127.0.0.1:8088")
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/components", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("without token status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/components", nil)
	request.Header.Set("Authorization", "secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("raw token status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/components", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("with token status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHealthDoesNotRequireAuthentication(t *testing.T) {
	handler, err := New(fakeManager{}, "secret", "127.0.0.1:8088")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d", response.Code)
	}
}
