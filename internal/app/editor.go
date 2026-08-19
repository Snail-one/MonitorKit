package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func selectTextEditor() (string, error) {
	for _, name := range []string{"vim", "nano", "vi"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到 vim、nano 或 vi，请先安装任意一个编辑器")
}

func editorValue() (string, bool) {
	editor, err := selectTextEditor()
	if err != nil {
		return "未安装", false
	}
	return filepathBase(editor), true
}

func openTextEditor(ctx context.Context, editor, path string) error {
	inputInfo, err := os.Stdin.Stat()
	if err != nil || inputInfo.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("当前没有可用的交互终端，无法打开 %s", filepathBase(editor))
	}
	command := exec.CommandContext(ctx, editor, path)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func filepathBase(path string) string {
	parts := strings.FieldsFunc(path, func(char rune) bool { return char == '/' || char == '\\' })
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}
