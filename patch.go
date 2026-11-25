package main

// パッチ形式の実装 (適用と作成)。
// 全て純 Go で、外部バイナリには依存しない。

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

func crc32b(b []byte) uint32 { return crc32.ChecksumIEEE(b) }

// PatchInfo はパッチファイルから読み取れる情報。
type PatchInfo struct {
	Format       string `json:"format"`       // ips / ips32 / ups / bps / aps / ppf / xdelta
	FormatName   string `json:"formatName"`   // 表示名
	HasSourceCRC bool   `json:"hasSourceCRC"` // 元ロムの CRC32 が記録されているか
	SourceCRC    uint32 `json:"sourceCRC"`
	HasTargetCRC bool   `json:"hasTargetCRC"`
	TargetCRC    uint32 `json:"targetCRC"`
	SourceSize   int64  `json:"sourceSize"` // -1 = 不明
	TargetSize   int64  `json:"targetSize"` // -1 = 不明
}

// DetectFormat はマジックから形式を判定する (拡張子は見ない)。
func DetectFormat(p []byte) (string, error) {
	switch {
	case bytes.HasPrefix(p, []byte("PATCH")):
		return "ips", nil
	case bytes.HasPrefix(p, []byte("EEPS")):
		return "ips32", nil
	case bytes.HasPrefix(p, []byte("UPS1")):
		return "ups", nil
	case bytes.HasPrefix(p, []byte("BPS1")):
		return "bps", nil
	case bytes.HasPrefix(p, []byte("APS1")):
		return "aps", nil
	case len(p) >= 6 && bytes.HasPrefix(p, []byte("PPF")) && p[3] >= '1' && p[3] <= '3' && p[4] == '.' && p[5] == '0':
		return "ppf", nil
	case len(p) >= 4 && p[0] == 0xD6 && p[1] == 0xC3 && p[2] == 0xC4 && p[3] == 0x00:
		return "xdelta", nil
	}
	return "", errors.New("unknown patch format")
}

var formatNames = map[string]string{
	"ips": "IPS", "ips32": "IPS32", "ups": "UPS", "bps": "BPS",
	"aps": "APS (GBA)", "ppf": "PPF", "xdelta": "xdelta (VCDIFF)",
}

// GetPatchInfo は形式ごとのヘッダ/フッタを読んで情報を返す。
func GetPatchInfo(p []byte) (*PatchInfo, error) {
	f, err := DetectFormat(p)
	if err != nil {
		return nil, err
	}
	info := &PatchInfo{Format: f, FormatName: formatNames[f], SourceSize: -1, TargetSize: -1}
	switch f {
	case "ups":
		r := &byteReader{b: p, pos: 4}
		ss, e1 := r.varint()
		ts, e2 := r.varint()
		if e1 != nil || e2 != nil || len(p) < 12 {
			return nil, errors.New("broken UPS patch")
		}
		info.SourceSize, info.TargetSize = int64(ss), int64(ts)
		info.SourceCRC = binary.LittleEndian.Uint32(p[len(p)-12:])
		info.TargetCRC = binary.LittleEndian.Uint32(p[len(p)-8:])
		info.HasSourceCRC, info.HasTargetCRC = true, true
	case "bps":
		r := &byteReader{b: p, pos: 4}
		ss, e1 := r.varint()
		ts, e2 := r.varint()
		if e1 != nil || e2 != nil || len(p) < 12 {
			return nil, errors.New("broken BPS patch")
		}
		info.SourceSize, info.TargetSize = int64(ss), int64(ts)
		info.SourceCRC = binary.LittleEndian.Uint32(p[len(p)-12:])
		info.TargetCRC = binary.LittleEndian.Uint32(p[len(p)-8:])
		info.HasSourceCRC, info.HasTargetCRC = true, true
	case "aps":
		if len(p) >= 12 {
			info.SourceSize = int64(binary.LittleEndian.Uint32(p[4:]))
			info.TargetSize = int64(binary.LittleEndian.Uint32(p[8:]))
		}
	case "ppf":
		if p[3] == '2' && len(p) >= 60 {
			info.SourceSize = int64(binary.LittleEndian.Uint32(p[56:]))
		}
	}
	return info, nil
}

// ApplyPatch はパッチを適用する。
// strict=true のとき、形式が持つ CRC (UPS/BPS) が合わなければエラーにする。
func ApplyPatch(src, p []byte, strict bool) ([]byte, error) {
	f, err := DetectFormat(p)
	if err != nil {
		return nil, err
	}
	switch f {
	case "ips":
		return applyIPS(src, p, false)
	case "ips32":
		return applyIPS(src, p, true)
	case "ups":
		return applyUPS(src, p, strict)
	case "bps":
		return applyBPS(src, p, strict)
	case "aps":
		return applyAPS(p, src)
	case "ppf":
		return applyPPF(src, p)
	case "xdelta":
		return vcdiffDecode(src, p)
	}
	return nil, errors.New("unsupported format")
}

// CreatePatch は指定形式のパッチを作る (ips/ups/bps/xdelta)。
func CreatePatch(src, dst []byte, format string) ([]byte, error) {
	switch format {
	case "ips":
		return createIPS(src, dst)
	case "ups":
		return createUPS(src, dst)
	case "bps":
		return createBPS(src, dst)
	case "xdelta":
		return vcdiffEncode(src, dst)
	}
	return nil, fmt.Errorf("cannot create %s patches", format)
}

// ---- 共通: byteReader と可変長整数 ----

type byteReader struct {
	b   []byte
	pos int
}

func (r *byteReader) byte() (byte, error) {
	if r.pos >= len(r.b) {
		return 0, errors.New("unexpected end of data")
	}
	c := r.b[r.pos]
	r.pos++
	return c, nil
}

func (r *byteReader) bytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.b) {
		return nil, errors.New("unexpected end of data")
	}
	out := r.b[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

// varint は UPS/BPS 形式の可変長整数 (byuu 方式)。
func (r *byteReader) varint() (uint64, error) {
	var x uint64
	var shift uint64 = 1
	for {
		c, err := r.byte()
		if err != nil {
			return 0, err
		}
		x += uint64(c&0x7F) * shift
		if c&0x80 != 0 {
			return x, nil
		}
		shift <<= 7
		x += shift
		if shift > (1 << 60) {
			return 0, errors.New("varint too large")
		}
	}
}

func appendVarint(dst []byte, x uint64) []byte {
	for {
		c := byte(x & 0x7F)
		x >>= 7
		if x == 0 {
			return append(dst, c|0x80)
		}
		dst = append(dst, c)
		x--
	}
}

// ---- IPS / IPS32 ----

func applyIPS(src, p []byte, is32 bool) ([]byte, error) {
	hdrLen, offLen := 5, 3
	eofMark := []byte("EOF")
	if is32 {
		hdrLen, offLen = 4, 4
		eofMark = []byte("EEOF")
	}
	if len(p) < hdrLen+len(eofMark) {
		return nil, errors.New("broken IPS patch")
	}
	out := append([]byte{}, src...)
	r := &byteReader{b: p, pos: hdrLen}
	readOff := func() (int, bool, error) {
		rest := p[r.pos:]
		if bytes.HasPrefix(rest, eofMark) {
			r.pos += len(eofMark)
			return 0, true, nil
		}
		raw, err := r.bytes(offLen)
		if err != nil {
			return 0, false, err
		}
		v := 0
		for _, c := range raw {
			v = v<<8 | int(c)
		}
		return v, false, nil
	}
	for {
		off, isEOF, err := readOff()
		if err != nil {
			return nil, err
		}
		if isEOF {
			break
		}
		szRaw, err := r.bytes(2)
		if err != nil {
			return nil, err
		}
		size := int(binary.BigEndian.Uint16(szRaw))
		if size == 0 { // RLE
			cntRaw, err := r.bytes(2)
			if err != nil {
				return nil, err
			}
			val, err := r.byte()
			if err != nil {
				return nil, err
			}
			cnt := int(binary.BigEndian.Uint16(cntRaw))
			for i := 0; i < cnt; i++ {
				if off+i >= len(out) {
					out = append(out, make([]byte, off+i-len(out)+1)...)
				}
				out[off+i] = val
			}
		} else {
			data, err := r.bytes(size)
			if err != nil {
				return nil, err
			}
			if off+size > len(out) {
				out = append(out, make([]byte, off+size-len(out))...)
			}
			copy(out[off:], data)
		}
	}
	// EOF の後に 3 バイト (IPS32 は 4 バイト) あれば切り詰めサイズ
	rem := len(p) - r.pos
	want := 3
	if is32 {
		want = 4
	}
	if rem == want {
		n := 0
		for _, c := range p[r.pos:] {
			n = n<<8 | int(c)
		}
		if n < len(out) {
			out = out[:n]
		} else if n > len(out) {
			out = append(out, make([]byte, n-len(out))...)
		}
	}
	return out, nil
}

func createIPS(src, dst []byte) ([]byte, error) {
	out := []byte("PATCH")
	flush := func(off int, data []byte) {
		for len(data) > 0 {
			n := len(data)
			if n > 0xFFFF {
				n = 0xFFFF
			}
			out = append(out, byte(off>>16), byte(off>>8), byte(off))
			out = append(out, byte(n>>8), byte(n))
			out = append(out, data[:n]...)
			off += n
			data = data[n:]
		}
	}
	i := 0
	for i < len(dst) {
		if i < len(src) && src[i] == dst[i] {
			i++
			continue
		}
		start := i
		for i < len(dst) && !(i < len(src) && src[i] == dst[i]) {
			i++
		}
		if start > 0xFFFFFF {
			return nil, errors.New("IPS cannot address beyond 16MB")
		}
		flush(start, dst[start:i])
	}
	out = append(out, []byte("EOF")...)
	if len(dst) != len(src) {
		out = append(out, byte(len(dst)>>16), byte(len(dst)>>8), byte(len(dst)))
	}
	return out, nil
}

// ---- UPS ----

func applyUPS(src, p []byte, strict bool) ([]byte, error) {
	if len(p) < 4+12 {
		return nil, errors.New("broken UPS patch")
	}
	r := &byteReader{b: p, pos: 4}
	srcSize, err := r.varint()
	if err != nil {
		return nil, err
	}
	tgtSize, err := r.varint()
	if err != nil {
		return nil, err
	}
	_ = srcSize
	if strict {
		if crc32b(p[:len(p)-4]) != binary.LittleEndian.Uint32(p[len(p)-4:]) {
			return nil, errors.New("patch CRC mismatch (broken UPS patch)")
		}
		if crc32b(src) != binary.LittleEndian.Uint32(p[len(p)-12:]) {
			return nil, errors.New("source ROM CRC mismatch")
		}
	}
	out := make([]byte, tgtSize)
	copy(out, src)
	pos := 0
	end := len(p) - 12
	for r.pos < end {
		skip, err := r.varint()
		if err != nil {
			return nil, err
		}
		pos += int(skip)
		for {
			if r.pos > end {
				return nil, errors.New("broken UPS patch")
			}
			c, err := r.byte()
			if err != nil {
				return nil, err
			}
			if pos < int(tgtSize) {
				var s byte
				if pos < len(src) {
					s = src[pos]
				}
				out[pos] = s ^ c
			}
			pos++
			if c == 0 {
				break
			}
		}
	}
	if strict && crc32b(out) != binary.LittleEndian.Uint32(p[len(p)-8:]) {
		return nil, errors.New("target CRC mismatch after patching")
	}
	return out, nil
}

func createUPS(src, dst []byte) ([]byte, error) {
	out := []byte("UPS1")
	out = appendVarint(out, uint64(len(src)))
	out = appendVarint(out, uint64(len(dst)))
	pos := 0
	srcAt := func(i int) byte {
		if i < len(src) {
			return src[i]
		}
		return 0
	}
	for pos < len(dst) {
		// 次の差分までスキップ
		skip := 0
		for pos+skip < len(dst) && srcAt(pos+skip) == dst[pos+skip] {
			skip++
		}
		if pos+skip >= len(dst) {
			break
		}
		out = appendVarint(out, uint64(skip))
		pos += skip
		// 差分が続く間 XOR を出し、0 で終端する
		for pos < len(dst) && srcAt(pos) != dst[pos] {
			out = append(out, srcAt(pos)^dst[pos])
			pos++
		}
		out = append(out, 0)
		pos++ // 終端の 0 の分も位置が進む
	}
	var crcBuf [4]byte
	binary.LittleEndian.PutUint32(crcBuf[:], crc32b(src))
	out = append(out, crcBuf[:]...)
	binary.LittleEndian.PutUint32(crcBuf[:], crc32b(dst))
	out = append(out, crcBuf[:]...)
	binary.LittleEndian.PutUint32(crcBuf[:], crc32b(out))
	out = append(out, crcBuf[:]...)
	return out, nil
}

// ---- BPS ----

func applyBPS(src, p []byte, strict bool) ([]byte, error) {
	if len(p) < 4+12 {
		return nil, errors.New("broken BPS patch")
	}
	r := &byteReader{b: p, pos: 4}
	srcSize, err := r.varint()
	if err != nil {
		return nil, err
	}
	tgtSize, err := r.varint()
	if err != nil {
		return nil, err
	}
	metaSize, err := r.varint()
	if err != nil {
		return nil, err
	}
	if _, err := r.bytes(int(metaSize)); err != nil {
		return nil, err
	}
	if strict {
		if crc32b(p[:len(p)-4]) != binary.LittleEndian.Uint32(p[len(p)-4:]) {
			return nil, errors.New("patch CRC mismatch (broken BPS patch)")
		}
		if crc32b(src) != binary.LittleEndian.Uint32(p[len(p)-12:]) {
			return nil, errors.New("source ROM CRC mismatch")
		}
		if int(srcSize) != len(src) {
			return nil, errors.New("source ROM size mismatch")
		}
	}
	out := make([]byte, 0, tgtSize)
	srcCopyPos, tgtCopyPos := 0, 0
	end := len(p) - 12
	signedVarint := func() (int, error) {
		v, err := r.varint()
		if err != nil {
			return 0, err
		}
		n := int(v >> 1)
		if v&1 != 0 {
			n = -n
		}
		return n, nil
	}
	for r.pos < end {
		v, err := r.varint()
		if err != nil {
			return nil, err
		}
		cmd := v & 3
		length := int(v>>2) + 1
		switch cmd {
		case 0: // SourceRead: 出力位置と同じ位置を元ロムから読む
			for i := 0; i < length; i++ {
				var c byte
				if len(out) < len(src) {
					c = src[len(out)]
				}
				if len(out) < int(tgtSize) {
					out = append(out, c)
				}
			}
		case 1: // TargetRead: パッチ内のデータをそのまま書く
			data, err := r.bytes(length)
			if err != nil {
				return nil, err
			}
			for _, c := range data {
				if len(out) < int(tgtSize) {
					out = append(out, c)
				}
			}
		case 2: // SourceCopy: 専用ポインタで元ロムからコピー
			off, err := signedVarint()
			if err != nil {
				return nil, err
			}
			srcCopyPos += off
			for i := 0; i < length; i++ {
				if srcCopyPos < 0 || srcCopyPos >= len(src) {
					return nil, errors.New("BPS SourceCopy out of range")
				}
				if len(out) < int(tgtSize) {
					out = append(out, src[srcCopyPos])
				}
				srcCopyPos++
			}
		case 3: // TargetCopy: 専用ポインタで出力済み領域からコピー
			off, err := signedVarint()
			if err != nil {
				return nil, err
			}
			tgtCopyPos += off
			for i := 0; i < length; i++ {
				if tgtCopyPos < 0 || tgtCopyPos >= len(out) {
					return nil, errors.New("BPS TargetCopy out of range")
				}
				c := out[tgtCopyPos]
				if len(out) < int(tgtSize) {
					out = append(out, c)
				}
				tgtCopyPos++
			}
		}
	}
	if uint64(len(out)) != tgtSize {
		return nil, errors.New("BPS output size mismatch")
	}
	if strict && crc32b(out) != binary.LittleEndian.Uint32(p[len(p)-8:]) {
		return nil, errors.New("target CRC mismatch after patching")
	}
	return out, nil
}

func createBPS(src, dst []byte) ([]byte, error) {
	out := []byte("BPS1")
	out = appendVarint(out, uint64(len(src)))
	out = appendVarint(out, uint64(len(dst)))
	out = appendVarint(out, 0) // メタデータなし
	emit := func(cmd uint64, length int, payload func()) {
		out = appendVarint(out, uint64(length-1)<<2|cmd)
		if payload != nil {
			payload()
		}
	}
	i := 0
	for i < len(dst) {
		// 一致区間は SourceRead、差分区間は TargetRead にする
		if i < len(src) && src[i] == dst[i] {
			j := i
			for j < len(dst) && j < len(src) && src[j] == dst[j] {
				j++
			}
			emit(0, j-i, nil)
			i = j
		} else {
			j := i
			for j < len(dst) && !(j < len(src) && src[j] == dst[j]) {
				j++
			}
			data := dst[i:j]
			emit(1, len(data), func() { out = append(out, data...) })
			i = j
		}
	}
	var crcBuf [4]byte
	binary.LittleEndian.PutUint32(crcBuf[:], crc32b(src))
	out = append(out, crcBuf[:]...)
	binary.LittleEndian.PutUint32(crcBuf[:], crc32b(dst))
	out = append(out, crcBuf[:]...)
	binary.LittleEndian.PutUint32(crcBuf[:], crc32b(out))
	out = append(out, crcBuf[:]...)
	return out, nil
}
