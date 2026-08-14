package ui

import (
	"fmt"
	"time"
)

// During provides immediate feedback for slow lifecycle operations. Only the
// renderer runs concurrently; component mutations remain serialized by Manager.
func (u *UI) During(label string, operation func() error) error {
	if !u.interactive {
		fmt.Fprintf(u.out, "[执行] %s\n", label)
		err := operation()
		if err == nil {
			fmt.Fprintf(u.out, "[完成] %s\n", label)
		}
		return err
	}

	result := make(chan error, 1)
	go func() { result <- operation() }()
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	ticker := time.NewTicker(90 * time.Millisecond)
	defer ticker.Stop()
	index := 0
	for {
		select {
		case err := <-result:
			mark := u.Success("✓")
			if err != nil {
				mark = u.paint(red, "×")
			}
			fmt.Fprintf(u.out, "\r\033[2K%s %s\n", mark, label)
			return err
		case <-ticker.C:
			fmt.Fprintf(u.out, "\r\033[2K%s %s", u.paint(orange, frames[index%len(frames)]), label)
			index++
		}
	}
}
