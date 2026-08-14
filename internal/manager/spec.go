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
	config     func(dataDir string) string
	unit       func(mtls bool) string
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
			unit:   lokiUnit,
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

func prometheusConfig(_ string) string {
	return `global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["127.0.0.1:9090"]
`
}

func prometheusUnit(mtls bool) string {
	webConfigArgument := ""
	if mtls {
		webConfigArgument = " --web.config.file=/etc/prometheus/web.yml"
	}
	return fmt.Sprintf(`[Unit]
Description=Prometheus monitoring server
Wants=network-online.target
After=network-online.target

[Service]
User=prometheus
Group=prometheus
Type=simple
ExecStart=/usr/local/bin/prometheus --config.file=/etc/prometheus/prometheus.yml --storage.tsdb.path=/var/lib/prometheus --web.listen-address=0.0.0.0:9090 --web.enable-remote-write-receiver%s
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/prometheus

[Install]
WantedBy=multi-user.target
`, webConfigArgument)
}

func lokiConfig(dataDir string) string {
	return fmt.Sprintf(`auth_enabled: false

server:
  http_listen_address: 0.0.0.0
  http_listen_port: 3100

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
`, dataDir, dataDir, dataDir)
}

func lokiUnit(mtls bool) string {
	tlsArguments := ""
	if mtls {
		tlsArguments = " -server.http-tls-cert-path=/etc/loki/tls/server.crt -server.http-tls-key-path=/etc/loki/tls/server.key -server.http-tls-client-auth=RequireAndVerifyClientCert -server.http-tls-ca-path=/etc/loki/tls/client-ca.crt"
	}
	return fmt.Sprintf(`[Unit]
Description=Grafana Loki log server
Wants=network-online.target
After=network-online.target

[Service]
User=loki
Group=loki
Type=simple
ExecStart=/usr/local/bin/loki -config.file=/etc/loki/loki.yml%s
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/loki

[Install]
WantedBy=multi-user.target
`, tlsArguments)
}
