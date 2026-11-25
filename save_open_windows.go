//go:build windows

package main

import "os/exec"

// openSavedFile はエクスプローラを開き、保存したファイルを選択状態にする。
func openSavedFile(path string) {
	_ = exec.Command("explorer.exe", "/select,"+path).Start()
}
