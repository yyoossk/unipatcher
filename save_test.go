package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// /api/save: 上書き先フォルダ (UNIPATCHER_SAVE_DIR) へ正確なバイト列を
// 書き、保存パスを JSON で返すこと。同名は上書きされること。
// UNIPATCHER_NO_OPEN=1 でエクスプローラ起動は抑止する (テスト前提)。
func TestSaveHandler(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIPATCHER_SAVE_DIR", dir)
	t.Setenv("UNIPATCHER_NO_OPEN", "1")

	payload := []byte("patched rom bytes \x00\x01\x02")
	req := httptest.NewRequest("POST", "/api/save?name=game%20(patched).nds", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	saveHandler(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var j map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	wantPath := filepath.Join(dir, "game (patched).nds")
	if j["path"] != wantPath {
		t.Fatalf("path = %v, want %v", j["path"], wantPath)
	}
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("saved file not found: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("saved bytes differ: %q vs %q", got, payload)
	}

	// 同名は上書きされる
	payload2 := []byte("second version")
	req2 := httptest.NewRequest("POST", "/api/save?name=game%20(patched).nds", bytes.NewReader(payload2))
	rec2 := httptest.NewRecorder()
	saveHandler(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("overwrite status = %d", rec2.Code)
	}
	got2, _ := os.ReadFile(wantPath)
	if !bytes.Equal(got2, payload2) {
		t.Fatalf("overwrite failed: %q", got2)
	}

	// パス区切りの混入はファイル名だけに丸める (フォルダ外へ書かない)
	req3 := httptest.NewRequest("POST", "/api/save?name=..%2Fevil.bin", bytes.NewReader(payload))
	rec3 := httptest.NewRecorder()
	saveHandler(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("sanitize status = %d", rec3.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.bin")); err != nil {
		t.Fatalf("sanitized file not in save dir: %v", err)
	}

	// 名前が無ければ 400
	req4 := httptest.NewRequest("POST", "/api/save", bytes.NewReader(payload))
	rec4 := httptest.NewRecorder()
	saveHandler(rec4, req4)
	if rec4.Code != 400 {
		t.Fatalf("empty name status = %d, want 400", rec4.Code)
	}
}
