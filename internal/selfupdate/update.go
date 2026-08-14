// Package selfupdate safely delegates binary updates to the repository's
// versioned installer contract.
package selfupdate

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const (
	InstallScriptURL = "https://raw.githubusercontent.com/Snail-one/MonitorKit/main/scripts/install.sh"
	maxScriptSize    = 1024 * 1024
)

func Run(ctx context.Context, arguments ...string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	return run(ctx, client, InstallScriptURL, os.Stdin, os.Stdout, os.Stderr, arguments...)
}

func run(ctx context.Context, client *http.Client, scriptURL string, stdin io.Reader, stdout, stderr io.Writer, arguments ...string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scriptURL, nil)
	if err != nil {
		return fmt.Errorf("创建安装脚本请求：%w", err)
	}
	req.Header.Set("User-Agent", "monitorkit-selfupdate")
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载安装脚本失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("下载安装脚本失败：HTTP %d %s", response.StatusCode, http.StatusText(response.StatusCode))
	}

	fmt.Fprintln(stderr, "[信息] 正在下载安装管理脚本")
	data, err := io.ReadAll(io.LimitReader(response.Body, maxScriptSize+1))
	if err != nil {
		return fmt.Errorf("读取安装脚本：%w", err)
	}
	if len(data) > maxScriptSize {
		return fmt.Errorf("安装脚本超过 %d 字节限制", maxScriptSize)
	}
	if !bytes.HasPrefix(data, []byte("#!/bin/sh")) {
		return fmt.Errorf("下载内容不是有效的 MonitorKit 安装脚本")
	}

	temporary, err := os.CreateTemp("", "monitorkit-action-*.sh")
	if err != nil {
		return fmt.Errorf("创建安装脚本临时文件：%w", err)
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return fmt.Errorf("设置安装脚本权限：%w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("保存安装脚本：%w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("保存安装脚本：%w", err)
	}

	command := exec.CommandContext(ctx, "sh", append([]string{path}, arguments...)...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("安装脚本执行失败：%w", err)
	}
	return nil
}
