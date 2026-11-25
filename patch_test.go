package main

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"os"
	"testing"
)

func randPair(t *testing.T, seed int64) (src, dst []byte) {
	t.Helper()
	rnd := rand.New(rand.NewSource(seed))
	src = make([]byte, 8192)
	rnd.Read(src)
	dst = append([]byte{}, src...)
	// 数カ所書き換え、末尾を伸ばす
	for i := 0; i < 30; i++ {
		pos := rnd.Intn(len(dst))
		dst[pos] ^= 0xFF
	}
	dst = append(dst, []byte("EXTRA-DATA-AT-END")...)
	// 中ほどに同じバイトの連続を入れる (RUN の練習)
	for i := 2000; i < 2600; i++ {
		dst[i] = 0x5A
	}
	return src, dst
}

func TestRoundTripCreateApply(t *testing.T) {
	for _, format := range []string{"ips", "ups", "bps", "xdelta"} {
		src, dst := randPair(t, 1234)
		patch, err := CreatePatch(src, dst, format)
		if err != nil {
			t.Fatalf("%s create: %v", format, err)
		}
		got, err := ApplyPatch(src, patch, true)
		if err != nil {
			t.Fatalf("%s apply: %v", format, err)
		}
		if !bytes.Equal(got, dst) {
			t.Fatalf("%s round-trip mismatch: got %d bytes, want %d", format, len(got), len(dst))
		}
	}
}

func TestRoundTripShrink(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	src := make([]byte, 5000)
	rnd.Read(src)
	dst := append([]byte{}, src[:3000]...)
	dst[10] ^= 0xFF
	for _, format := range []string{"ips", "ups", "bps", "xdelta"} {
		patch, err := CreatePatch(src, dst, format)
		if err != nil {
			t.Fatalf("%s create: %v", format, err)
		}
		got, err := ApplyPatch(src, patch, true)
		if err != nil {
			t.Fatalf("%s apply: %v", format, err)
		}
		if !bytes.Equal(got, dst) {
			t.Fatalf("%s shrink round-trip mismatch", format)
		}
	}
}

func TestPatchInfoUPSBPS(t *testing.T) {
	src, dst := randPair(t, 99)
	for _, format := range []string{"ups", "bps"} {
		patch, err := CreatePatch(src, dst, format)
		if err != nil {
			t.Fatal(err)
		}
		info, err := GetPatchInfo(patch)
		if err != nil {
			t.Fatal(err)
		}
		if !info.HasSourceCRC || info.SourceCRC != crc32b(src) {
			t.Fatalf("%s source CRC: got %08X want %08X", format, info.SourceCRC, crc32b(src))
		}
		if !info.HasTargetCRC || info.TargetCRC != crc32b(dst) {
			t.Fatalf("%s target CRC: got %08X want %08X", format, info.TargetCRC, crc32b(dst))
		}
		if info.SourceSize != int64(len(src)) || info.TargetSize != int64(len(dst)) {
			t.Fatalf("%s sizes wrong: %d/%d", format, info.SourceSize, info.TargetSize)
		}
	}
}

func TestPatchInfoNoCRC(t *testing.T) {
	src, dst := randPair(t, 5)
	patch, _ := CreatePatch(src, dst, "ips")
	info, err := GetPatchInfo(patch)
	if err != nil {
		t.Fatal(err)
	}
	if info.HasSourceCRC {
		t.Fatal("IPS must not report a source CRC")
	}
	if info.Format != "ips" {
		t.Fatalf("format = %s", info.Format)
	}
}

func TestCRCMismatchDetection(t *testing.T) {
	src, dst := randPair(t, 3)
	other, _ := randPair(t, 4)
	for _, format := range []string{"ups", "bps"} {
		patch, _ := CreatePatch(src, dst, format)
		if _, err := ApplyPatch(other, patch, true); err == nil {
			t.Fatalf("%s: strict apply to wrong ROM must fail", format)
		}
		info, _ := GetPatchInfo(patch)
		if crc32b(other) == info.SourceCRC {
			t.Fatal("test setup broken: CRCs collide")
		}
		// 非 strict では適用できる (警告モードで続行した相当)
		if _, err := ApplyPatch(other, patch, false); err != nil {
			t.Fatalf("%s: non-strict apply should proceed: %v", format, err)
		}
	}
}

func TestIPS32Apply(t *testing.T) {
	// 手作りの IPS32 (EEPS) パッチ: オフセット 4 に 3 バイト書く
	src := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	p := []byte("EEPS")
	p = append(p, 0, 0, 0, 4, 0, 3, 0xAA, 0xBB, 0xCC)
	p = append(p, []byte("EEOF")...)
	got, err := ApplyPatch(src, p, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 0, 0, 0, 0xAA, 0xBB, 0xCC, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestAPSApplyFixture(t *testing.T) {
	// 手作りの APS (GBA) フィクスチャ
	src := make([]byte, 0x20000)
	for i := range src {
		src[i] = byte(i)
	}
	dst := append([]byte{}, src...)
	dst[0x10005] = 0xEE
	dst[0x10006] = 0xDD
	// ブロック 1 (オフセット 0x10000) のレコードを作る
	block := dst[0x10000 : 0x10000+16]
	p := []byte("APS1")
	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], uint32(len(src)))
	p = append(p, u32[:]...)
	binary.LittleEndian.PutUint32(u32[:], uint32(len(dst)))
	p = append(p, u32[:]...)
	var rec [10]byte
	binary.LittleEndian.PutUint32(rec[0:], 0x10000)
	binary.LittleEndian.PutUint16(rec[4:], crc16(src[0x10000:0x10000+16]))
	binary.LittleEndian.PutUint16(rec[6:], crc16(block))
	binary.LittleEndian.PutUint16(rec[8:], 16)
	p = append(p, rec[:]...)
	p = append(p, block...)
	got, err := ApplyPatch(src, p, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, dst) {
		t.Fatal("APS apply mismatch")
	}
	// 違うロムでは CRC16 で弾かれる
	bad := append([]byte{}, src...)
	bad[0x10000] ^= 0xFF
	if _, err := ApplyPatch(bad, p, true); err == nil {
		t.Fatal("APS apply to wrong ROM must fail")
	}
}

func TestPPF1ApplyFixture(t *testing.T) {
	src := []byte("0123456789ABCDEF")
	p := []byte("PPF1.0")
	p = append(p, make([]byte, 50)...) // 説明文
	var rec [5]byte
	binary.LittleEndian.PutUint32(rec[0:], 4)
	rec[4] = 3
	p = append(p, rec[:]...)
	p = append(p, []byte("XYZ")...)
	got, err := ApplyPatch(src, p, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "0123XYZ789ABCDEF" {
		t.Fatalf("got %q", got)
	}
}

func TestPPF3ApplyFixture(t *testing.T) {
	src := []byte("0123456789ABCDEF")
	p := []byte("PPF3.0")
	p = append(p, make([]byte, 50)...) // 説明文
	p = append(p, 0, 0, 0, 0)          // 種別/ブロックチェックなし/Undo なし/予備
	var rec [9]byte
	binary.LittleEndian.PutUint64(rec[0:], 2)
	rec[8] = 2
	p = append(p, rec[:]...)
	p = append(p, []byte("QQ")...)
	got, err := ApplyPatch(src, p, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "01QQ456789ABCDEF" {
		t.Fatalf("got %q", got)
	}
}

// xdelta3 (本物) が作ったフィクスチャをデコードできること。
func TestXdelta3RealFixture(t *testing.T) {
	src, err := os.ReadFile("testdata/xd_src.bin")
	if err != nil {
		t.Skip("fixture missing")
	}
	want, err := os.ReadFile("testdata/xd_tgt.bin")
	if err != nil {
		t.Skip("fixture missing")
	}
	// 二次圧縮なし (-S none) の本物フィクスチャはデコードできること
	patch, err := os.ReadFile("testdata/xd_patch_none.vcdiff")
	if err != nil {
		t.Skip("fixture missing")
	}
	got, err := ApplyPatch(src, patch, true)
	if err != nil {
		t.Fatalf("xdelta3 -S none fixture: decode failed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("xdelta3 -S none fixture: decoded output mismatch (%d vs %d bytes)", len(got), len(want))
	}
	// 既定設定 (FGK 二次圧縮) のフィクスチャは、未対応として明確にエラーになること
	fgk, err := os.ReadFile("testdata/xd_patch.vcdiff")
	if err != nil {
		t.Skip("fixture missing")
	}
	if _, err := ApplyPatch(src, fgk, true); err == nil {
		t.Fatal("FGK-compressed xdelta3 fixture should return a not-supported error")
	}
}

// 自作エンコーダの出力が自分のデコーダで往復すること (xdelta3 本物での検証は verify で別途)。
func TestVCDIFFEncodeDecodeLarge(t *testing.T) {
	rnd := rand.New(rand.NewSource(21))
	src := make([]byte, 100000)
	rnd.Read(src)
	dst := append([]byte{}, src...)
	dst = append(dst[:50000], append([]byte("INSERTED"), dst[50000:]...)...)
	patch, err := CreatePatch(src, dst, "xdelta")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ApplyPatch(src, patch, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, dst) {
		t.Fatal("large VCDIFF round-trip mismatch")
	}
}
