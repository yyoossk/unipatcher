package main

// PPF (1.0/2.0/3.0) の適用と、APS (GBA) の適用。

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// ---- PPF ----

// applyPPF は PPF パッチを適用する。
// 構造 (Rom Patcher JS 等の一般的な実装に基づく):
//   - 0..5: "PPF1.0" / "PPF2.0" / "PPF3.0"
//   - 6..55: 説明文 (50 バイト)
//   - PPF1: 56 からレコード (オフセット u32 LE、個数 u8、データ)
//   - PPF2: 56 に元ファイルサイズ u32 LE、続いて 1024 バイトのブロックチェック、
//     その後 PPF1 と同じレコード
//   - PPF3: 56 にイメージ種別、57 にブロックチェック有無、58 に Undo 有無、59 は予備。
//     ブロックチェックがあれば 1024 バイトを読み飛ばす。
//     レコードはオフセット u64 LE、個数 u8、データ (、Undo データ)。
func applyPPF(src, p []byte) ([]byte, error) {
	if len(p) < 56 {
		return nil, errors.New("broken PPF patch")
	}
	version := p[3]
	// 末尾の FILE_ID.DIZ ブロックはレコードではないので切り落とす
	data := p
	if idx := bytes.Index(data, []byte("@BEGIN_FILE_ID.DIZ")); idx >= 4 {
		data = data[:idx-4]
	}
	out := append([]byte{}, src...)
	pos := 56
	write := func(off int64, chunk []byte) {
		end := off + int64(len(chunk))
		if end > int64(len(out)) {
			out = append(out, make([]byte, end-int64(len(out)))...)
		}
		copy(out[off:], chunk)
	}
	switch version {
	case '1':
		for pos < len(data) {
			if pos+5 > len(data) {
				return nil, errors.New("broken PPF1 record")
			}
			off := int64(binary.LittleEndian.Uint32(data[pos:]))
			n := int(data[pos+4])
			pos += 5
			if pos+n > len(data) {
				return nil, errors.New("broken PPF1 record data")
			}
			write(off, data[pos:pos+n])
			pos += n
		}
	case '2':
		if len(data) < 56+4+1024 {
			return nil, errors.New("broken PPF2 patch")
		}
		pos = 56 + 4 + 1024
		for pos < len(data) {
			if pos+5 > len(data) {
				return nil, errors.New("broken PPF2 record")
			}
			off := int64(binary.LittleEndian.Uint32(data[pos:]))
			n := int(data[pos+4])
			pos += 5
			if pos+n > len(data) {
				return nil, errors.New("broken PPF2 record data")
			}
			write(off, data[pos:pos+n])
			pos += n
		}
	case '3':
		if len(data) < 60 {
			return nil, errors.New("broken PPF3 patch")
		}
		blockCheck := data[57] != 0
		undo := data[58] != 0
		pos = 60
		if blockCheck {
			pos += 1024
		}
		if pos > len(data) {
			return nil, errors.New("broken PPF3 patch")
		}
		for pos < len(data) {
			if pos+9 > len(data) {
				return nil, errors.New("broken PPF3 record")
			}
			off := int64(binary.LittleEndian.Uint64(data[pos:]))
			n := int(data[pos+8])
			pos += 9
			if pos+n > len(data) {
				return nil, errors.New("broken PPF3 record data")
			}
			write(off, data[pos:pos+n])
			pos += n
			if undo {
				pos += n // Undo データは読み飛ばす
				if pos > len(data) {
					return nil, errors.New("broken PPF3 undo data")
				}
			}
		}
	default:
		return nil, errors.New("unsupported PPF version")
	}
	return out, nil
}

// ---- APS (GBA) ----

// crc16 は CRC-16/ARC (多項式 0xA001 の反射形、初期値 0)。
func crc16(b []byte) uint16 {
	var crc uint16
	for _, c := range b {
		crc ^= uint16(c)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

const apsBlockSize = 0x10000 // 64KB

// applyAPS は APS (GBA) パッチを適用する。
// 構造:
//   - 0..3: "APS1"
//   - 4..7: 元ロムのサイズ (u32 LE)
//   - 8..11: 適用後ロムのサイズ (u32 LE)
//   - 以降、変更のある 64KB ブロックごとのレコード:
//     オフセット (u32 LE)、元ブロックの CRC16 (u16 LE)、
//     新ブロックの CRC16 (u16 LE)、データ長 (u16 LE)、新しいブロックデータ。
//
// CRC16 が新ブロック側と一致する場合は適用済みとみなしてそのままにする
// (双方向チェック)。どちらとも一致しない場合はエラーにする。
func applyAPS(patch, src []byte) ([]byte, error) {
	if len(patch) < 12 {
		return nil, errors.New("broken APS patch")
	}
	origSize := int(binary.LittleEndian.Uint32(patch[4:]))
	newSize := int(binary.LittleEndian.Uint32(patch[8:]))
	if origSize != len(src) {
		return nil, errors.New("APS: source ROM size mismatch")
	}
	out := make([]byte, newSize)
	copy(out, src)
	r := &byteReader{b: patch, pos: 12}
	for r.pos < len(patch) {
		hdr, err := r.bytes(10)
		if err != nil {
			return nil, errors.New("broken APS record")
		}
		off := int(binary.LittleEndian.Uint32(hdr[0:]))
		crcOrig := binary.LittleEndian.Uint16(hdr[4:])
		crcNew := binary.LittleEndian.Uint16(hdr[6:])
		n := int(binary.LittleEndian.Uint16(hdr[8:]))
		data, err := r.bytes(n)
		if err != nil {
			return nil, errors.New("broken APS record data")
		}
		if off+n > origSize && off >= newSize {
			return nil, errors.New("APS record out of range")
		}
		// 元ブロック (元ロムの同じ位置) の CRC16 を確認する
		var cur []byte
		if off < len(src) {
			end := off + n
			if end > len(src) {
				end = len(src)
			}
			cur = src[off:end]
		}
		switch crc16(cur) {
		case crcOrig:
			// 未適用: 書き込む
			if off+n > len(out) {
				return nil, errors.New("APS record out of range")
			}
			copy(out[off:], data)
			written := out[off : off+n]
			if crc16(written) != crcNew {
				return nil, errors.New("APS: block CRC mismatch after writing")
			}
		case crcNew:
			// 適用済み: そのまま
			if off+n <= len(out) {
				copy(out[off:], data)
			}
		default:
			return nil, errors.New("APS: block CRC mismatch (wrong ROM?)")
		}
	}
	return out, nil
}
