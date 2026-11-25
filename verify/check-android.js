// UniPatcher Android 経路の検証:
// ページに UniBridge (Android シェルの名前付き public ブリッジ) の
// スタブを入れた状態で jsdom を実行し、「パッチを当てる」の結果が
// blob のアンカーダウンロードではなくブリッジ経由で保存され、
// 渡されるバイト列が結果そのものと一致することを確認する。
const fs = require("fs");
const path = require("path");
const { JSDOM } = require("jsdom");

const html = fs.readFileSync(path.join(__dirname, "..", "page.html"), "utf8");

let failures = 0;
function assert(cond, msg) {
  if (cond) console.log("ok - " + msg);
  else { failures++; console.log("FAIL - " + msg); }
}

const RESULT = Buffer.from("hello patched world, this is the result rom bytes");
const bridge = {
  calls: [],
  chunks: [],
  name: null,
  beginSave(name) { this.calls.push("begin"); this.name = name; this.chunks = []; return true; },
  appendSave(b64) { this.calls.push("append"); this.chunks.push(Buffer.from(b64, "base64")); return true; },
  finishSave() { this.calls.push("finish"); return true; },
  cancelSave() { this.calls.push("cancel"); },
};

const dom = new JSDOM(html, {
  runScripts: "dangerously",
  url: "http://127.0.0.1/",
  beforeParse(window) {
    window.UniBridge = bridge;
    window.fetch = (url, opts) => {
      if (url === "/api/hash") {
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ name: "x", size: 3, crc32: "AABBCCDD", md5: "md5x", sha1: "sha1x" }), headers: { get: () => null } });
      }
      if (url === "/api/patchinfo") {
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ name: "p", size: 10, crc32: "00000000", format: "ips", formatName: "IPS", hasSourceCRC: false, sourceCRC: "", hasTargetCRC: false, targetCRC: "", sourceSize: 3, targetSize: 3 }), headers: { get: () => null } });
      }
      if (url === "/api/apply") {
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
  }
});
const { window } = dom;
const { document } = window;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

(async () => {
  await sleep(50);
  assert(typeof window.UniBridge.beginSave === "function", "UniBridge visible to the page");

  // ファイル欄へドロップで ROM とパッチをセット (Android ではタップ=
  // ファイル選択だが、ページ側の取り込み経路は同一)
  for (const [id, name] of [["apply-rom", "game.gba"], ["apply-patch", "fix.ips"]]) {
    const ff = document.getElementById("ff-" + id);
    const drop = new window.Event("drop", { cancelable: true, bubbles: true });
    drop.dataTransfer = { files: [new window.File(["abc"], name)] };
    ff.dispatchEvent(drop);
    await sleep(30);
    assert(document.getElementById("fn-" + id).textContent === name, "file set on " + id);
  }
  document.getElementById("apply-outname").value = "game-patched.gba";

  document.getElementById("apply-btn").click();
  await sleep(120);

  assert(bridge.calls[0] === "begin", "bridge beginSave called first");
  assert(bridge.name === "game-patched.gba", "bridge received the output name (got: " + bridge.name + ")");
  assert(bridge.calls.filter((c) => c === "append").length >= 1, "bridge appendSave called");
  assert(bridge.calls[bridge.calls.length - 1] === "finish", "bridge finishSave called last");
  const saved = Buffer.concat(bridge.chunks);
  assert(saved.equals(RESULT), "bytes handed to the bridge equal the result bytes");
  const dlg = document.getElementById("dialog");
  assert(dlg.style.display === "flex" && document.getElementById("dialog-msg").textContent.includes("game-patched.gba"),
    "success dialog shown after bridge save");

  // ブリッジが保存失敗を返したら、成功ではなくエラーダイアログになる
  bridge.beginSave = () => false;
  document.getElementById("apply-btn").click();
  await sleep(120);
  assert(document.getElementById("dialog-msg").textContent.length > 0 &&
    !document.getElementById("dialog-msg").textContent.includes("game-patched.gba"),
    "error dialog shown when bridge save fails");

  console.log(failures === 0 ? "\nALL ANDROID-PATH CHECKS PASSED" : "\n" + failures + " FAILURES");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.error("FAIL: exception", e); process.exit(1); });
