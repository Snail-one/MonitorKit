package manager

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func extractBinaries(archivePath, destination string, names []string) (map[string]string, error) {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractZip(archivePath, destination, names)
	}
	if strings.HasSuffix(archivePath, ".tar.gz") {
		return extractTarGz(archivePath, destination, names)
	}
	return nil, fmt.Errorf("不支持的压缩格式：%s", archivePath)
}

func extractTarGz(archivePath, destination string, names []string) (map[string]string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	found := make(map[string]string)
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		base := filepath.Base(filepath.Clean(header.Name))
		if header.Typeflag != tar.TypeReg || !contains(names, base) {
			continue
		}
		path := filepath.Join(destination, base)
		if err := copyBinary(path, reader); err != nil {
			return nil, err
		}
		found[base] = path
	}
	return requireBinaries(found, names)
}

func extractZip(archivePath, destination string, names []string) (map[string]string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	found := make(map[string]string)
	for _, entry := range reader.File {
		base := filepath.Base(filepath.Clean(entry.Name))
		// Loki 的发布文件在压缩包内名为 loki-linux-<arch>。
		logicalName := base
		if len(names) == 1 && names[0] == "loki" && strings.HasPrefix(base, "loki-linux-") {
			logicalName = "loki"
		}
		if entry.FileInfo().IsDir() || !contains(names, logicalName) {
			continue
		}
		input, err := entry.Open()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(destination, logicalName)
		err = copyBinary(path, input)
		closeErr := input.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		found[logicalName] = path
	}
	return requireBinaries(found, names)
}

func copyBinary(path string, source io.Reader) error {
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, source)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func requireBinaries(found map[string]string, names []string) (map[string]string, error) {
	for _, name := range names {
		if found[name] == "" {
			return nil, fmt.Errorf("压缩包内缺少 %s", name)
		}
	}
	return found, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
