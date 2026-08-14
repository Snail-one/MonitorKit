// Package server exposes the manager as a small authenticated HTTP API.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/Snail-one/MonitorKit/internal/manager"
)

// Manager describes the application layer used by the transport.
type Manager interface {
	Install(ctx context.Context, name, version string) (manager.Status, error)
	Uninstall(ctx context.Context, name string, purge bool) error
	Status(ctx context.Context, name string) (manager.Status, error)
}

type API struct {
	manager Manager
	token   string
	mux     *http.ServeMux
}

type installRequest struct {
	Version string `json:"version"`
}

func New(mgr Manager, token, listen string) (http.Handler, error) {
	if mgr == nil {
		return nil, errors.New("manager 不能为空")
	}
	if token == "" && !isLoopbackListen(listen) {
		return nil, errors.New("监听非本机地址时必须设置 SNAILMON_TOKEN 或 --token")
	}
	api := &API{manager: mgr, token: token, mux: http.NewServeMux()}
	api.mux.HandleFunc("GET /healthz", api.health)
	api.mux.HandleFunc("GET /api/v1/components", api.listComponents)
	api.mux.HandleFunc("GET /api/v1/components/{name}", api.getComponent)
	api.mux.HandleFunc("POST /api/v1/components/{name}/install", api.installComponent)
	api.mux.HandleFunc("DELETE /api/v1/components/{name}", api.uninstallComponent)
	return api, nil
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path != "/healthz" && !a.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "unauthorized", "需要有效的 Bearer Token")
		return
	}
	a.mux.ServeHTTP(w, r)
}

func (a *API) authorized(r *http.Request) bool {
	if a.token == "" {
		return true
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimPrefix(header, prefix)
	return len(provided) == len(a.token) && subtle.ConstantTimeCompare([]byte(provided), []byte(a.token)) == 1
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) listComponents(w http.ResponseWriter, r *http.Request) {
	statuses := make([]manager.Status, 0, len(manager.ComponentNames()))
	for _, name := range manager.ComponentNames() {
		status, err := a.manager.Status(r.Context(), name)
		if err != nil {
			writeManagerError(w, err)
			return
		}
		statuses = append(statuses, status)
	}
	writeJSON(w, http.StatusOK, map[string]any{"components": statuses})
}

func (a *API) getComponent(w http.ResponseWriter, r *http.Request) {
	status, err := a.manager.Status(r.Context(), r.PathValue("name"))
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) installComponent(w http.ResponseWriter, r *http.Request) {
	var request installRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求 JSON 无效："+err.Error())
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求只能包含一个 JSON 对象")
		return
	}
	if request.Version == "" {
		request.Version = "latest"
	}
	status, err := a.manager.Install(r.Context(), r.PathValue("name"), request.Version)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) uninstallComponent(w http.ResponseWriter, r *http.Request) {
	purge := r.URL.Query().Get("purge") == "true"
	name := r.PathValue("name")
	if err := a.manager.Uninstall(r.Context(), name, purge); err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "uninstalled": true, "purged": purge})
}

func writeManagerError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "operation_failed"
	if strings.Contains(err.Error(), "不支持的组件") || strings.Contains(err.Error(), "无效版本") {
		status = http.StatusBadRequest
		code = "invalid_argument"
	}
	writeError(w, status, code, err.Error())
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func isLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
