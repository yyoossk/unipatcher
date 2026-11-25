// UniPatcher 出力名の回帰テスト:
// 適用結果のファイル名が、パッチの形式ではなく元のロムの形式 (拡張子)
// になることを確認する。実報告: .bps パッチを当てたら結果が
// 「… (patched).bps」という名前で保存されてしまった。
const fs = require("fs");
const path = require("path");
const { JSDOM } = require("jsdom");

const html = fs.readFileSync(path.join(__dirname, "..", "page.html"), "utf8");

let failures = 0;
function assert(cond, msg) {
  if (cond) console.log("ok - " + msg);
  else { failures++; console.log("FAIL - " + msg); }
}

const savedNames = [];
const RESULT = Buffer.from("result bytes");
const dom = new JSDOM(html, {
  runScripts: "dangerously",
  url: "http://127.0.0.1/",
  beforeParse(window) {
    window.fetch = (url) => {
      if (url === "/api/hash") {
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ name: "x", size: 3, crc32: "AABBCCDD", md5: "md5x", sha1: "sha1x" }), headers: { get: () => null } });
      }
      if (url === "/api/patchinfo") {
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ name: "p", size: 10, crc32: "00000000", format: "bps", formatName: "BPS", hasSourceCRC: true, sourceCRC: "AABBCCDD", hasTargetCRC: true, targetCRC: "11223344", sourceSize: 3, targetSize: 3 }), headers: { get: () => null } });
      }
      if (url === "/api/apply" || url === "/api/create") {
        return Promise.resolve({
          ok: true, status: 200,
          json: async () => ({}),
          headers: { get: (h) => (h === "X-Output-CRC32" ? "DEADBEEF" : h === "X-Output-Size" ? String(RESULT.length) : null) },
          blob: async () => new window.Blob([RESULT]),
        });
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => ({}), headers: { get: () => null }, blob: async () => new window.Blob([]) });
    };
    if (!window.URL.createObjectURL) window.URL.createObjectURL = () => "blob:fake";
    if (!window.URL.revokeObjectURL) window.URL.revokeObjectURL = () => {};
    // デスクトップ経路のダウンロード (a.click) を横取りして名前を記録する
    window.HTMLAnchorElement.prototype.click = function () { savedNames.push(this.download); };
  }
});
const { window } = dom;
const { document } = window;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function setViaDrop(id, name) {
  const ff = document.getElementById("ff-" + id);
  const drop = new window.Event("drop", { cancelable: true, bubbles: true });
  drop.dataTransfer = { files: [new window.File(["abc"], name)] };
  ff.dispatchEvent(drop);
  await sleep(40);
}
function typeInto(id, value) {
  const el = document.getElementById(id);
  el.value = value;
  el.dispatchEvent(new window.Event("input", { bubbles: true }));
}
function lastSaved() { return savedNames[savedNames.length - 1]; }

(async () => {
  await sleep(50);
  assert(typeof window.eval("enforceRomExt") === "function", "enforceRomExt exists");
  assert(window.eval(`enforceRomExt("x.bps", "game")`) === "x", "rom without ext: patch ext stripped");
  assert(window.eval(`enforceRomExt("x.txt", "game")`) === "x.txt", "rom without ext: non-patch ext kept");
  assert(window.eval(`enforceRomExt("x", "game.nds")`) === "x.nds", "missing ext gets rom ext");
  assert(window.eval(`enforcePatchExt("x.ips", "bps")`) === "x.bps", "create: ext forced to selected format");

  // (a) ROM game.nds + パッチ hack.bps → game (patched).nds
  await setViaDrop("apply-rom", "game.nds");
  await setViaDrop("apply-patch", "hack.bps");
  assert(document.getElementById("apply-outname").value === "game (patched).nds",
    "(a) auto name from rom (got: " + document.getElementById("apply-outname").value + ")");
  document.getElementById("apply-btn").click();
  await sleep(80);
  assert(lastSaved() === "game (patched).nds", "(a) saved as game (patched).nds (got: " + lastSaved() + ")");

  // (b) stale: 先に a.bps をロム欄に入れて自動入力させ、その後 game.nds に変える
  await setViaDrop("apply-rom", "a.bps");
  assert(document.getElementById("apply-outname").value === "a (patched).bps",
    "(b) first rom fills field (got: " + document.getElementById("apply-outname").value + ")");
  await setViaDrop("apply-rom", "game.nds");
  assert(document.getElementById("apply-outname").value === "game (patched).nds",
    "(b) field regenerated for new rom (got: " + document.getElementById("apply-outname").value + ")");
  document.getElementById("apply-btn").click();
  await sleep(80);
  assert(lastSaved() === "game (patched).nds", "(b) saved as game (patched).nds (got: " + lastSaved() + ")");

  // (c) 手入力 myhack.bps + ROM game.nds → myhack.nds
  typeInto("apply-outname", "myhack.bps");
  document.getElementById("apply-btn").click();
  await sleep(80);
  assert(lastSaved() === "myhack.nds", "(c) manual name ext corrected to .nds (got: " + lastSaved() + ")");

  // (d) 手入力のベース名は、パッチを変えても上書きされない
  typeInto("apply-outname", "custom");
  await setViaDrop("apply-patch", "other.ups");
  assert(document.getElementById("apply-outname").value === "custom",
    "(d) manual edit not overwritten when patch changes (got: " + document.getElementById("apply-outname").value + ")");
  document.getElementById("apply-btn").click();
  await sleep(80);
  assert(lastSaved() === "custom.nds", "(d) saved keeps manual base name with rom ext (got: " + lastSaved() + ")");

  // 作成側: 自動入力は選択形式の拡張子、手入力の違う拡張子は作成時にそろえる
  await setViaDrop("create-orig", "orig.nds");
  await setViaDrop("create-mod", "mod.nds");
  assert(document.getElementById("create-outname").value === "mod.bps",
    "create auto name uses selected format ext (got: " + document.getElementById("create-outname").value + ")");
  typeInto("create-outname", "mypatch.ips");
  document.getElementById("create-btn").click();
  await sleep(80);
  assert(lastSaved() === "mypatch.bps", "create: manual .ips corrected to selected .bps (got: " + lastSaved() + ")");
  const fmt = document.getElementById("create-format");
  fmt.value = "ips";
  fmt.dispatchEvent(new window.Event("change", { bubbles: true }));
  typeInto("create-outname", "cool.ups");
  document.getElementById("create-btn").click();
  await sleep(80);
  assert(lastSaved() === "cool.ips", "create: manual .ups corrected to selected .ips (got: " + lastSaved() + ")");

  console.log(failures === 0 ? "\nALL OUTNAME CHECKS PASSED" : "\n" + failures + " FAILURES");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.error("FAIL: exception", e); process.exit(1); });
