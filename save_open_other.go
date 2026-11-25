//go:build !windows

package main

// openSavedFile は Windows 以外では何もしない
// (Android 版はブリッジ経由で保存するので、この経路自体を使わない)。
func openSavedFile(path string) {}
