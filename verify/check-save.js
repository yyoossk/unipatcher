// UniPatcher 保存場所の検証:
//  - PC: 結果は /api/save でサーバが Downloads へ保存し、完了ダイアログに
//    フルパスが出る。/api/save が失敗したら従来のアンカーダウンロードに
//    落ち、ダイアログにパスは出ない。
//  - Android (UniBridge): 完了ダイアログに保存場所 (ダウンロード
//    フォルダ) の行が出る。当てる/作成の両方で確認する。
//  - PC のダイアログには Android 用の行は出ない。
const fs = require("fs");
const path = require("path");
const { JSDOM } = require("jsdom");

const html = fs.readFileSync(path.join(__dirname, "..", "page.html"), "utf8");

let failures = 0;
function assert(cond, msg) {
  if (cond) console.log("ok - " + msg);
  else { failures++; console.log("FAIL - " + msg); }
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const RESULT = Buffer.from("result bytes for save checks");
const WIN_DIR = "C:\\Users\\test\\Downloads\\";

function makeDom({ android = false, saveOk = true } = {}) {
  const rec = { saveCalls: [], anchorNames: [], bridgeNames: [] };
  const bridge = {
    beginSave(name) { rec.bridgeNames.push(name); return true; },
    appendSave() { return true; },
    finishSave() { return true; },
    cancelSave() {},
  };
  const dom = new JSDOM(html, {
    runScripts: "dangerously",
    url: "http://127.0.0.1/",
    beforeParse(window) {
      if (android) window.UniBridge = bridge;
      window.fetch = (url) => {
        if (url === "/api/hash") {
          return Promise.resolve({ ok: true, status: 200, json: async () => ({ name: "x", size: 3, crc32: "AABBCCDD", md5: "md5x", sha1: "sha1x" }), headers: { get: () => null } });
        }
        if (url === "/api/patchinfo") {
          return Promise.resolve({ ok: true, status: 200, json: async () => ({ name: "p", size: 10, crc32: "00000000", format: "bps", formatName: "BPS", hasSourceCRC: true, sourceCRC: "AABBCCDD", hasTargetCRC: true, targetCRC: "11223344", sourceSize: 3, targetSize: 3 }), headers: { get: () => null } });
        }
        if (url.startsWith("/api/save")) {
          const name = decodeURIComponent(url.split("name=")[1] || "");
          rec.saveCalls.push(name);
          if (!saveOk) return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: "boom" }), headers: { get: () => null } });
          return Promise.resolve({ ok: true, status: 200, json: async () => ({ path: WIN_DIR + name }), headers: { get: () => null } });
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
      window.HTMLAnchorElement.prototype.click = function () { rec.anchorNames.push(this.download); };
    }
  });
  return { dom, rec };
}

async function setViaDrop(window, document, id, name) {
  const ff = document.getElementById("ff-" + id);
  const drop = new window.Event("drop", { cancelable: true, bubbles: true });
  drop.dataTransfer = { files: [new window.File(["abc"], name)] };
  ff.dispatchEvent(drop);
  await sleep(40);
}
async function fillAndRun(window, document) {
  await setViaDrop(window, document, "apply-rom", "game.nds");
  await setViaDrop(window, document, "apply-patch", "hack.bps");
  document.getElementById("apply-btn").click();
  await sleep(120);
  const applyMsg = document.getElementById("dialog-msg").textContent;
  await setViaDrop(window, document, "create-orig", "orig.nds");
  await setViaDrop(window, document, "create-mod", "mod.nds");
  document.getElementById("create-btn").click();
  await sleep(120);
  const createMsg = document.getElementById("dialog-msg").textContent;
  return { applyMsg, createMsg };
}

(async () => {
  // ---- A: PC、/api/save 成功 ----
  {
    const { dom, rec } = makeDom({});
    const { window } = dom; const { document } = window;
    await sleep(50);
    const { applyMsg, createMsg } = await fillAndRun(window, document);
    assert(rec.saveCalls.includes("game (patched).nds"), "A: /api/save called for apply with rom-ext name");
    assert(rec.saveCalls.includes("mod.bps"), "A: /api/save called for create");
    assert(applyMsg.includes("Save location: " + WIN_DIR + "game (patched).nds"), "A: apply dialog shows full saved path");
    assert(createMsg.includes("Save location: " + WIN_DIR + "mod.bps"), "A: create dialog shows full saved path");
    assert(!applyMsg.includes("Downloads folder (Download)") && !createMsg.includes("Downloads folder (Download)"),
      "A: desktop dialogs do NOT contain the Android save-dir line");
    assert(rec.anchorNames.length === 0, "A: anchor fallback not used");
    assert(rec.bridgeNames.length === 0, "A: bridge not used on desktop");
  }

  // ---- B: PC、/api/save 失敗 → アンカーへフォールバック ----
  {
    const { dom, rec } = makeDom({ saveOk: false });
    const { window } = dom; const { document } = window;
    await sleep(50);
    await setViaDrop(window, document, "apply-rom", "game.nds");
    await setViaDrop(window, document, "apply-patch", "hack.bps");
    document.getElementById("apply-btn").click();
    await sleep(120);
    const msg = document.getElementById("dialog-msg").textContent;
    assert(rec.saveCalls.length >= 1, "B: /api/save was attempted");
    assert(rec.anchorNames.includes("game (patched).nds"), "B: anchor fallback downloaded the result");
    assert(msg.includes("game (patched).nds"), "B: success dialog still shown");
    assert(!msg.includes("Save location:"), "B: no path line in dialog on fallback");
  }

  // ---- C: Android (UniBridge) ----
  {
    const { dom, rec } = makeDom({ android: true });
    const { window } = dom; const { document } = window;
    await sleep(50);
    const { applyMsg, createMsg } = await fillAndRun(window, document);
    assert(rec.bridgeNames.includes("game (patched).nds"), "C: bridge saved apply result");
    assert(rec.bridgeNames.includes("mod.bps"), "C: bridge saved create result");
    assert(applyMsg.includes("Save location: Downloads folder (Download)"), "C: apply dialog shows save directory line");
    assert(createMsg.includes("Save location: Downloads folder (Download)"), "C: create dialog shows save directory line");
    assert(rec.saveCalls.length === 0, "C: /api/save not used on Android");
  }

  console.log(failures === 0 ? "\nALL SAVE-LOCATION CHECKS PASSED" : "\n" + failures + " FAILURES");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.error("FAIL: exception", e); process.exit(1); });
