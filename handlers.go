package main

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
)

const maxUpload = 1 << 30 // 1GB

func readFormFile(r *http.Request, field string) ([]byte, string, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxUpload+(10<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, "", err
	}
	f, hdr, err := r.FormFile(field)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	return b, hdr.Filename, err
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// /api/hash: ファイルの CRC32 / MD5 / SHA-1 / サイズを返す。
func hashHandler(w http.ResponseWriter, r *http.Request) {
	b, name, err := readFormFile(r, "file")
	if err != nil {
		writeErr(w, 400, "file read error: "+err.Error())
		return
	}
	m := md5.Sum(b)
	s := sha1.Sum(b)
	writeJSON(w, map[string]any{
		"name":  name,
		"size":  len(b),
		"crc32": fmt.Sprintf("%08X", crc32.ChecksumIEEE(b)),
		"md5":   hex.EncodeToString(m[:]),
		"sha1":  hex.EncodeToString(s[:]),
	})
}

// /api/patchinfo: パッチの形式と、記録されている CRC などを返す。
func patchInfoHandler(w http.ResponseWriter, r *http.Request) {
	b, name, err := readFormFile(r, "file")
	if err != nil {
		writeErr(w, 400, "file read error: "+err.Error())
		return
	}
	info, err := GetPatchInfo(b)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"name":         name,
		"size":         len(b),
		"crc32":        fmt.Sprintf("%08X", crc32.ChecksumIEEE(b)),
		"format":       info.Format,
		"formatName":   info.FormatName,
		"hasSourceCRC": info.HasSourceCRC,
		"sourceCRC":    fmt.Sprintf("%08X", info.SourceCRC),
		"hasTargetCRC": info.HasTargetCRC,
		"targetCRC":    fmt.Sprintf("%08X", info.TargetCRC),
		"sourceSize":   info.SourceSize,
		"targetSize":   info.TargetSize,
	})
}

func sendBinary(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Output-CRC32", fmt.Sprintf("%08X", crc32.ChecksumIEEE(b)))
	w.Header().Set("X-Output-Size", fmt.Sprint(len(b)))
	w.Write(b)
}

// /api/apply: rom + patch を受け取り、適用結果を返す。
// crcMode: warn (既定) / block / ignore。force=1 で警告を無視して続行する。
func applyHandler(w http.ResponseWriter, r *http.Request) {
	rom, _, err := readFormFile(r, "rom")
	if err != nil {
		writeErr(w, 400, "rom read error: "+err.Error())
		return
	}
	patch, _, err := readFormFile(r, "patch")
	if err != nil {
		writeErr(w, 400, "patch read error: "+err.Error())
		return
	}
	crcMode := r.FormValue("crcMode")
	force := r.FormValue("force") == "1"
	info, err := GetPatchInfo(patch)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	mismatch := info.HasSourceCRC && crc32.ChecksumIEEE(rom) != info.SourceCRC
	if mismatch && crcMode != "ignore" {
		if crcMode == "block" {
			writeErr(w, 400, fmt.Sprintf("CRC mismatch: patch expects %08X but ROM is %08X", info.SourceCRC, crc32.ChecksumIEEE(rom)))
			return
		}
		if !force { // warn
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(409)
			json.NewEncoder(w).Encode(map[string]any{
				"error":       "crc_mismatch",
				"expectedCRC": fmt.Sprintf("%08X", info.SourceCRC),
				"actualCRC":   fmt.Sprintf("%08X", crc32.ChecksumIEEE(rom)),
			})
			return
		}
	}
	strict := crcMode == "block"
	out, err := ApplyPatch(rom, patch, strict)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// block モードでは結果の CRC も確認する
	if crcMode == "block" && info.HasTargetCRC && crc32.ChecksumIEEE(out) != info.TargetCRC {
		writeErr(w, 400, "target CRC mismatch after patching")
		return
	}
	sendBinary(w, out)
}

// /api/create: original + modified + format を受け取り、パッチを返す。
func createHandler(w http.ResponseWriter, r *http.Request) {
	orig, _, err := readFormFile(r, "original")
	if err != nil {
		writeErr(w, 400, "original read error: "+err.Error())
		return
	}
	mod, _, err := readFormFile(r, "modified")
	if err != nil {
		writeErr(w, 400, "modified read error: "+err.Error())
		return
	}
	format := r.FormValue("format")
	patch, err := CreatePatch(orig, mod, format)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	sendBinary(w, patch)
}
