package manager

import (
	"context"
	"fmt"
	"runtime"
)

type componentSpec struct {
	name       string
	repository string
	user       string
	binaries   []string
	assetName  func(version, arch string) string
	config     func(dataDir string, port int) string
	unit       func(mtls, remoteWrite bool, port int) string
	validate   func(ctx context.Context, configPath string) error
}

func registeredSpecs() []componentSpec {
	return []componentSpec{
		{
			name:       "prometheus",
			repository: "prometheus/prometheus",
			user:       "prometheus",
			binaries:   []string{"prometheus", "promtool"},
			assetName: func(version, arch string) string {
				return fmt.Sprintf("prometheus-%s.linux-%s.tar.gz", version, arch)
			},
			config: prometheusConfig,
			unit:   prometheusUnit,
			validate: func(ctx context.Context, configPath string) error {
				return run(ctx, "/usr/local/bin/promtool", "check", "config", configPath)
			},
		},
		{
			name:       "loki",
			repository: "grafana/loki",
			user:       "loki",
			binaries:   []string{"loki"},
			assetName: func(_ string, arch string) string {
				if arch == "armv7" {
					arch = "arm"
				}
				return fmt.Sprintf("loki-linux-%s.zip", arch)
			},
			config: lokiConfig,
			unit: func(mtls, remoteWrite bool, port int) string {
				return lokiUnit(mtls, port, 0)
			},
			validate: func(ctx context.Context, configPath string) error {
				return run(ctx, "/usr/local/bin/loki", "-config.file="+configPath, "-verify-config=true")
			},
		},
	}
}

func platformArch() (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("中心端只支持 Linux，当前系统为 %s", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64", "arm64", "386", "ppc64le", "s390x":
		return runtime.GOARCH, nil
	case "arm":
		return "armv7", nil
	default:
		return "", fmt.Errorf("不支持的处理器架构：%s", runtime.GOARCH)
	}
}

func (m *Manager) generatedMainConfigLocked(name string, listenPort, grpcPort int) string {
	if name == "loki" {
		return lokiConfigWithGRPC("/var/lib/loki", listenPort, grpcPort)
	}
	return prometheusConfig("/var/lib/prometheus", listenPort)
}

func prometheusConfig(_ string, port int) string {
	return fmt.Sprintf(`global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["127.0.0.1:%d"]
`, port)
}

func lokiConfig(dataDir string, port int) string {
	return lokiConfigWithGRPC(dataDir, port, 0)
}

func lokiConfigWithGRPC(dataDir string, httpPort, grpcPort int) string {
	grpcBlock := ""
	if grpcPort > 0 {
		grpcBlock = fmt.Sprintf("\n  grpc_listen_address: 127.0.0.1\n  grpc_listen_port: %d", grpcPort)
	}
	return fmt.Sprintf(`auth_enabled: false

server:
  http_listen_address: 0.0.0.0
  http_listen_port: %d%s

common:
  path_prefix: %s
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory
  storage:
    filesystem:
      chunks_directory: %s/chunks
      rules_directory: %s/rules

schema_config:
  configs:
    - from: 2024-04-01
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

limits_config:
  allow_structured_metadata: true
`, httpPort, grpcBlock, dataDir, dataDir, dataDir)
}

func lokiUnit(mtls bool, httpPort, grpcPort int) string {
	tlsArguments := ""
	if mtls {
		tlsArguments = " -server.http-tls-cert-path=/etc/loki/tls/server.crt -server.http-tls-key-path=/etc/loki/tls/server.key -server.http-tls-client-auth=RequireAndVerifyClientCert -server.http-tls-ca-path=/etc/loki/tls/client-ca.crt"
	}
	grpcArguments := ""
	if grpcPort > 0 {
		grpcArguments = fmt.Sprintf(" -server.grpc-listen-address=127.0.0.1 -server.grpc-listen-port=%d", grpcPort)
	}
	return fmt.Sprintf(`[Unit]
Description=Grafana Loki log server
Wants=network-online.target
After=network-online.target

[Service]
User=loki
Group=loki
Type=simple
ExecStart=/usr/local/bin/loki -config.file=/etc/loki/loki.yml -server.http-listen-address=0.0.0.0 -server.http-listen-port=%d%s%s
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/loki

[Install]
WantedBy=multi-user.target
`, httpPort, grpcArguments, tlsArguments)
}
