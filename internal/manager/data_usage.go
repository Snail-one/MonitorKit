package manager

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// DataUsage is the on-disk size of a component data directory.
type DataUsage struct {
	Path   string
	Bytes  int64
	Exists bool
}

func (m *Manager) componentDataUsageLocked(name string) (DataUsage, error) {
	path, err := m.componentDataDirLocked(name)
	if err != nil {
		return DataUsage{}, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return DataUsage{Path: path}, nil
	}
	if err != nil {
		return DataUsage{}, err
	}
	if !info.IsDir() {
		return DataUsage{Path: path, Bytes: info.Size(), Exists: true}, nil
	}
	size, err := directorySize(path)
	if err != nil {
		return DataUsage{}, err
	}
	return DataUsage{Path: path, Bytes: size, Exists: true}, nil
}

func (m *Manager) componentDataDirLocked(name string) (string, error) {
	if name == "loki" {
		content, err := os.ReadFile(m.path("/etc/loki/loki.yml"))
		if err == nil {
			if value, ok := yamlFindScalar(content, "path_prefix"); ok && value != "" {
				if filepath.IsAbs(value) && !m.isLiveRoot() {
					return m.path(value), nil
				}
				return value, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return m.path("/var/lib/" + name), nil
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}
