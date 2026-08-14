// Package version exposes build metadata injected by release builds.
package version

import (
	"fmt"
	"runtime"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func Info() string {
	return fmt.Sprintf(
		"monitorkit %s\ncommit: %s\nbuild date: %s\ngo: %s\nplatform: %s/%s",
		Version,
		Commit,
		BuildDate,
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	)
}
