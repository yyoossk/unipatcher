package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// saveDir は結果ファイルの保存先フォルダを返す。
// テスト用に UNIPATCHER_SAVE_DIR で上書きできる。通常はユーザーの
// Downloads フォルダ (無ければ saveHandler が作る)。
func saveDir() (string, error) {
	if d := strings.TrimSpace(os.Getenv("UNIPATCHER_SAVE_DIR")); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

// /api/save?name=<出力名>: 結果バイト列 (リクエストボディそのもの) を
// Downloads フォルダへ保存し、保存したフルパスを JSON で返す。
// 同名ファイルがあれば上書きする (ブラウザの「置き換え」に相当)。
// 保存できたら Windows ではエクスプローラでそのファイルを開く
// (UNIPATCHER_NO_OPEN=1 で抑止。テストでは必ず指定する)。
func saveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "method not allowed")
		return
	}
	name := filepath.Base(strings.TrimSpace(r.URL.Query().Get("name")))
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		writeErr(w, 400, "bad file name")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, "read error: "+err.Error())
		return
	}
	dir, err := saveDir()
	if err != nil {
		writeErr(w, 500, "save dir error: "+err.Error())
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, 500, "mkdir error: "+err.Error())
		return
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		writeErr(w, 500, "write error: "+err.Error())
		return
	}
	if os.Getenv("UNIPATCHER_NO_OPEN") == "" {
		openSavedFile(path)
	}
	writeJSON(w, map[string]any{"path": path, "name": name, "size": len(b)})
}
