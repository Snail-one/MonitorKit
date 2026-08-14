package manager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type releaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Digest      string `json:"digest"`
}

type githubRelease struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

func (m *Manager) resolveRelease(ctx context.Context, spec componentSpec, wanted, arch string) (string, releaseAsset, error) {
	if wanted == "" {
		wanted = "latest"
	}
	if wanted != "latest" && !validVersion(wanted) {
		return "", releaseAsset{}, fmt.Errorf("无效版本 %q，应为 latest 或 x.y.z", wanted)
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", spec.repository)
	if wanted != "latest" {
		endpoint = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/v%s", spec.repository, strings.TrimPrefix(wanted, "v"))
	}
	var release githubRelease
	if err := m.getJSON(ctx, endpoint, &release); err != nil {
		return "", releaseAsset{}, fmt.Errorf("查询 %s 版本失败：%w", spec.name, err)
	}
	version := strings.TrimPrefix(release.TagName, "v")
	if !validVersion(version) {
		return "", releaseAsset{}, fmt.Errorf("GitHub 返回了无效版本 %q", release.TagName)
	}
	wantedAsset := spec.assetName(version, arch)
	for _, asset := range release.Assets {
		if asset.Name == wantedAsset {
			if asset.DownloadURL == "" {
				return "", releaseAsset{}, fmt.Errorf("发布资源 %s 缺少下载地址", wantedAsset)
			}
			if asset.Digest == "" {
				asset.Digest = m.digestFromChecksumAsset(ctx, release.Assets, wantedAsset)
			}
			return version, asset, nil
		}
	}
	return "", releaseAsset{}, fmt.Errorf("%s %s 没有适用于 linux/%s 的资源 %s", spec.name, version, arch, wantedAsset)
}

func (m *Manager) getJSON(ctx context.Context, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "monitorkit")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(target)
}

func (m *Manager) digestFromChecksumAsset(ctx context.Context, assets []releaseAsset, wantedName string) string {
	for _, asset := range assets {
		lowerName := strings.ToLower(asset.Name)
		if !strings.Contains(lowerName, "sha256") && !strings.Contains(lowerName, "checksum") {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "monitorkit")
		resp, err := m.client.Do(req)
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		for _, line := range strings.Split(string(body), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || strings.TrimPrefix(fields[len(fields)-1], "*") != wantedName {
				continue
			}
			digest := strings.ToLower(fields[0])
			if len(digest) != sha256.Size*2 {
				continue
			}
			if _, err := hex.DecodeString(digest); err == nil {
				return "sha256:" + digest
			}
		}
	}
	return ""
}

type downloadProgressFunc func(downloaded, total int64, done bool)

type downloadProgressWriter struct {
	total       int64
	downloaded  int64
	lastPercent int64
	lastBytes   int64
	report      downloadProgressFunc
}

func (w *downloadProgressWriter) Write(data []byte) (int, error) {
	w.downloaded += int64(len(data))
	if w.report == nil {
		return len(data), nil
	}
	if w.total > 0 {
		percent := w.downloaded * 100 / w.total
		if percent >= w.lastPercent+2 || w.downloaded >= w.total {
			w.lastPercent = percent
			w.report(w.downloaded, w.total, false)
		}
	} else if w.downloaded-w.lastBytes >= 1024*1024 {
		w.lastBytes = w.downloaded
		w.report(w.downloaded, w.total, false)
	}
	return len(data), nil
}

func (m *Manager) download(ctx context.Context, asset releaseAsset, destination string, progress downloadProgressFunc) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "monitorkit")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 %s 失败：HTTP %s", asset.Name, resp.Status)
	}
	if progress != nil {
		progress(0, resp.ContentLength, false)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	progressWriter := &downloadProgressWriter{total: resp.ContentLength, lastPercent: -2, report: progress}
	_, copyErr := io.Copy(io.MultiWriter(file, hash, progressWriter), resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}

	if asset.Digest == "" {
		return fmt.Errorf("发布资源 %s 没有 SHA-256 摘要，拒绝安装未校验文件", asset.Name)
	}
	want, ok := strings.CutPrefix(strings.ToLower(asset.Digest), "sha256:")
	if !ok || len(want) != sha256.Size*2 {
		return fmt.Errorf("发布资源 %s 的摘要格式无效", asset.Name)
	}
	if _, err := hex.DecodeString(want); err != nil {
		return fmt.Errorf("发布资源 %s 的摘要格式无效：%w", asset.Name, err)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return fmt.Errorf("发布资源 %s 的 SHA-256 校验失败", asset.Name)
	}
	if progress != nil {
		progress(progressWriter.downloaded, resp.ContentLength, true)
	}
	return nil
}

func validVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}
