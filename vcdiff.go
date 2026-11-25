package main

// VCDIFF (RFC 3284) のデコーダとエンコーダ。
// xdelta3 が作るパッチ (二次圧縮なし) をデコードできる。
// エンコーダは共通プレフィックス/サフィックスを COPY、残りを ADD/RUN で出す
// 簡易版だが、仕様に沿った正しい VCDIFF を生成する。

import (
	"encoding/binary"
	"errors"
	"hash/adler32"
)

// 命令種別
const (
	vcNoop = 0
	vcAdd  = 1
	vcRun  = 2
	vcCopy = 3
)

type vcCodeTable struct {
	inst1, inst2 [256]byte
	size1, size2 [256]byte
	mode1, mode2 [256]byte
	nearSize     int
	sameSize     int
}

// defaultCodeTable は RFC 3284 のデフォルトコード表を、
// open-vcdiff の並び規則から生成する。
func defaultCodeTable() *vcCodeTable {
	t := &vcCodeTable{nearSize: 4, sameSize: 3}
	t.inst1[0] = vcRun
	for i := 1; i <= 18; i++ {
		t.inst1[i] = vcAdd
		t.size1[i] = byte(i - 1)
	}
	sizes := []byte{0, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18}
	for i := 19; i <= 162; i++ {
		t.inst1[i] = vcCopy
		t.size1[i] = sizes[(i-19)%16]
		t.mode1[i] = byte((i - 19) / 16)
	}
	s1pat := []byte{1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4}
	s2pat := []byte{4, 5, 6}
	for i := 163; i <= 234; i++ {
		t.inst1[i] = vcAdd
		t.inst2[i] = vcCopy
		t.size1[i] = s1pat[(i-163)%12]
		t.size2[i] = s2pat[(i-163)%3]
		t.mode2[i] = byte((i - 163) / 12)
	}
	for i := 235; i <= 246; i++ {
		t.inst1[i] = vcAdd
		t.inst2[i] = vcCopy
		t.size1[i] = byte((i-235)%4 + 1)
		t.size2[i] = 4
		t.mode2[i] = byte(6 + (i-235)/4)
	}
	for i := 247; i <= 255; i++ {
		t.inst1[i] = vcCopy
		t.inst2[i] = vcAdd
		t.size1[i] = 4
		t.size2[i] = 1
		t.mode1[i] = byte(i - 247)
	}
	return t
}

// vcVarint は VCDIFF の可変長整数 (上位ビット継続、ビッグエンディアン)。
func vcReadVarint(r *byteReader) (int, error) {
	v := 0
	for i := 0; i < 5; i++ {
		c, err := r.byte()
		if err != nil {
			return 0, err
		}
		v = (v << 7) | int(c&0x7F)
		if c&0x80 == 0 {
			return v, nil
		}
	}
	return 0, errors.New("VCDIFF varint too long")
}

func vcAppendVarint(dst []byte, v int) []byte {
	var tmp [5]byte // 下位グループから格納する
	n := 0
	for {
		tmp[n] = byte(v & 0x7F)
		v >>= 7
		n++
		if v == 0 {
			break
		}
	}
	for i := n - 1; i >= 0; i-- {
		b := tmp[i]
		if i != 0 {
			b |= 0x80 // 最後に出すバイト以外は継続ビットを立てる
		}
		dst = append(dst, b)
	}
	return dst
}

// アドレスキャッシュ
type vcAddrCache struct {
	near []int
	same []int
	next int
}

func newVCAddrCache(nearSize, sameSize int) *vcAddrCache {
	return &vcAddrCache{near: make([]int, nearSize), same: make([]int, sameSize*256)}
}

func (c *vcAddrCache) decode(r *byteReader, mode byte, herePos int) (int, error) {
	var addr int
	nearModes := len(c.near)
	switch {
	case mode == 0: // SELF
		v, err := vcReadVarint(r)
		if err != nil {
			return 0, err
		}
		addr = v
	case mode == 1: // HERE
		v, err := vcReadVarint(r)
		if err != nil {
			return 0, err
		}
		addr = herePos - v
	case int(mode) >= 2 && int(mode) < 2+nearModes: // NEAR
		v, err := vcReadVarint(r)
		if err != nil {
			return 0, err
		}
		addr = c.near[mode-2] + v
	default: // SAME
		idx := (int(mode) - (2 + nearModes)) * 256
		b, err := r.byte()
		if err != nil {
			return 0, err
		}
		addr = c.same[idx+int(b)]
	}
	if addr < 0 {
		return 0, errors.New("VCDIFF negative COPY address")
	}
	// キャッシュ更新
	if len(c.near) > 0 {
		c.near[c.next%len(c.near)] = addr
		c.next++
	}
	if len(c.same) > 0 {
		c.same[addr%len(c.same)] = addr
	}
	return addr, nil
}

func vcdiffDecode(src, delta []byte) ([]byte, error) {
	if len(delta) < 5 || delta[0] != 0xD6 || delta[1] != 0xC3 || delta[2] != 0xC4 {
		return nil, errors.New("not a VCDIFF file")
	}
	r := &byteReader{b: delta, pos: 4} // 4 バイト目 (バージョン 0) は呼び出し側で確認済み相当
	hdrInd, err := r.byte()
	if err != nil {
		return nil, err
	}
	table := defaultCodeTable()
	if hdrInd&0x01 != 0 { // 二次圧縮
		id, err := r.byte()
		if err != nil {
			return nil, err
		}
		if id != 0 {
			return nil, errors.New("VCDIFF secondary compressor not supported")
		}
	}
	if hdrInd&0x02 != 0 { // カスタムコード表
		nearB, err := r.byte()
		if err != nil {
			return nil, err
		}
		sameB, err := r.byte()
		if err != nil {
			return nil, err
		}
		ct := &vcCodeTable{nearSize: int(nearB), sameSize: int(sameB)}
		read256 := func(dst *[256]byte) error {
			raw, err := r.bytes(256)
			if err != nil {
				return err
			}
			copy(dst[:], raw)
			return nil
		}
		for _, dst := range []*[256]byte{&ct.inst1, &ct.inst2, &ct.size1, &ct.size2, &ct.mode1, &ct.mode2} {
			if err := read256(dst); err != nil {
				return nil, err
			}
		}
		table = ct
	}
	if hdrInd&0x04 != 0 { // アプリケーションヘッダ
		n, err := vcReadVarint(r)
		if err != nil {
			return nil, err
		}
		if _, err := r.bytes(n); err != nil {
			return nil, err
		}
	}

	var result []byte
	for r.pos < len(delta) {
		winInd, err := r.byte()
		if err != nil {
			return nil, err
		}
		var srcWindow []byte
		if winInd&0x03 != 0 {
			winLen, err := vcReadVarint(r)
			if err != nil {
				return nil, err
			}
			winPos, err := vcReadVarint(r)
			if err != nil {
				return nil, err
			}
			var base []byte
			if winInd&0x01 != 0 {
				base = src
			} else {
				base = result
			}
			if winPos < 0 || winPos+winLen > len(base) {
				return nil, errors.New("VCDIFF source window out of range")
			}
			srcWindow = base[winPos : winPos+winLen]
		}
		deltaLen, err := vcReadVarint(r)
		if err != nil {
			return nil, err
		}
		winStart := r.pos
		winEnd := winStart + deltaLen
		if winEnd > len(delta) {
			return nil, errors.New("VCDIFF window overruns file")
		}
		tgtLen, err := vcReadVarint(r)
		if err != nil {
			return nil, err
		}
		deltaInd, err := r.byte()
		if err != nil {
			return nil, err
		}
		if deltaInd != 0 {
			return nil, errors.New("VCDIFF compressed sections not supported")
		}
		lenData, err := vcReadVarint(r)
		if err != nil {
			return nil, err
		}
		lenInst, err := vcReadVarint(r)
		if err != nil {
			return nil, err
		}
		lenAddr, err := vcReadVarint(r)
		if err != nil {
			return nil, err
		}
		var wantAdler uint32
		hasAdler := winInd&0x04 != 0
		if hasAdler {
			raw, err := r.bytes(4)
			if err != nil {
				return nil, err
			}
			wantAdler = binary.BigEndian.Uint32(raw)
		}
		dataSec, err := r.bytes(lenData)
		if err != nil {
			return nil, err
		}
		instSec, err := r.bytes(lenInst)
		if err != nil {
			return nil, err
		}
		addrSec, err := r.bytes(lenAddr)
		if err != nil {
			return nil, err
		}
		if r.pos != winEnd {
			return nil, errors.New("VCDIFF window length mismatch")
		}

		out := make([]byte, 0, tgtLen)
		dataR := &byteReader{b: dataSec}
		instR := &byteReader{b: instSec}
		addrR := &byteReader{b: addrSec}
		cache := newVCAddrCache(table.nearSize, table.sameSize)
		execInst := func(inst byte, size int, mode byte) error {
			switch inst {
			case vcNoop:
				return nil
			case vcAdd:
				if size == 0 {
					v, err := vcReadVarint(instR)
					if err != nil {
						return err
					}
					size = v
				}
				raw, err := dataR.bytes(size)
				if err != nil {
					return err
				}
				out = append(out, raw...)
				return nil
			case vcRun:
				if size == 0 {
					v, err := vcReadVarint(instR)
					if err != nil {
						return err
					}
					size = v
				}
				c, err := dataR.byte()
				if err != nil {
					return err
				}
				for i := 0; i < size; i++ {
					out = append(out, c)
				}
				return nil
			case vcCopy:
				if size == 0 {
					v, err := vcReadVarint(instR)
					if err != nil {
						return err
					}
					size = v
				}
				here := len(srcWindow) + len(out)
				addr, err := cache.decode(addrR, mode, here)
				if err != nil {
					return err
				}
				for i := 0; i < size; i++ {
					a := addr + i
					var c byte
					switch {
					case a < len(srcWindow):
						c = srcWindow[a]
					case a-len(srcWindow) < len(out):
						c = out[a-len(srcWindow)]
					default:
						return errors.New("VCDIFF COPY out of range")
					}
					out = append(out, c)
				}
				return nil
			}
			return errors.New("VCDIFF bad instruction")
		}
		for len(out) < tgtLen {
			op, err := instR.byte()
			if err != nil {
				return nil, err
			}
			if err := execInst(table.inst1[op], int(table.size1[op]), table.mode1[op]); err != nil {
				return nil, err
			}
			if len(out) >= tgtLen {
				break
			}
			if table.inst2[op] != vcNoop {
				if err := execInst(table.inst2[op], int(table.size2[op]), table.mode2[op]); err != nil {
					return nil, err
				}
			}
		}
		if len(out) != tgtLen {
			return nil, errors.New("VCDIFF target window size mismatch")
		}
		if hasAdler && adler32.Checksum(out) != wantAdler {
			return nil, errors.New("VCDIFF Adler32 mismatch (wrong source ROM?)")
		}
		result = append(result, out...)
	}
	return result, nil
}

// ---- エンコーダ ----

func vcdiffEncode(src, dst []byte) ([]byte, error) {
	out := []byte{0xD6, 0xC3, 0xC4, 0x00, 0x00} // マジック+バージョン+ヘッダ指示子 0

	// 共通プレフィックスとサフィックスの長さ
	p := 0
	for p < len(src) && p < len(dst) && src[p] == dst[p] {
		p++
	}
	s := 0
	for s < len(src)-p && s < len(dst)-p && src[len(src)-1-s] == dst[len(dst)-1-s] {
		s++
	}

	var dataSec, instSec, addrSec []byte
	copyInst := func(addr, size int) {
		// opcode 19 = COPY サイズ 0 モード 0 (SELF)、サイズは可変長で続く
		instSec = append(instSec, 19)
		instSec = vcAppendVarint(instSec, size)
		addrSec = vcAppendVarint(addrSec, addr)
	}
	if p > 0 {
		copyInst(0, p)
	}
	mid := dst[p : len(dst)-s]
	if len(mid) > 0 {
		allSame := true
		for _, c := range mid {
			if c != mid[0] {
				allSame = false
				break
			}
		}
		if allSame && len(mid) >= 4 {
			instSec = append(instSec, 0) // RUN サイズ 0
			instSec = vcAppendVarint(instSec, len(mid))
			dataSec = append(dataSec, mid[0])
		} else {
			instSec = append(instSec, 1) // ADD サイズ 0
			instSec = vcAppendVarint(instSec, len(mid))
			dataSec = append(dataSec, mid...)
		}
	}
	if s > 0 {
		copyInst(len(src)-s, s)
	}

	var win []byte
	winInd := byte(0x04) // Adler32 付き
	if len(src) > 0 {
		winInd |= 0x01 // VCD_SOURCE
	}
	win = append(win, winInd)
	if len(src) > 0 {
		win = vcAppendVarint(win, len(src))
		win = vcAppendVarint(win, 0)
	}
	var delta []byte
	delta = vcAppendVarint(delta, len(dst))
	delta = append(delta, 0) // Delta_Indicator
	delta = vcAppendVarint(delta, len(dataSec))
	delta = vcAppendVarint(delta, len(instSec))
	delta = vcAppendVarint(delta, len(addrSec))
	var ad [4]byte
	binary.BigEndian.PutUint32(ad[:], adler32.Checksum(dst))
	delta = append(delta, ad[:]...)
	delta = append(delta, dataSec...)
	delta = append(delta, instSec...)
	delta = append(delta, addrSec...)
	win = vcAppendVarint(win, len(delta))
	win = append(win, delta...)
	out = append(out, win...)
	return out, nil
}
