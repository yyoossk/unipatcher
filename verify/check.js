// UniPatcher 画面検証:
//  1. <script> を抽出して node --check 相当の構文確認 (このスクリプト自体が読み込む前に別途実施)
//  2. jsdom でページを実行し、ページ構造・タブ切替・12言語辞書・説明文・
//     ドラッグ&ドロップ・CRC 照合表示を検証する。
const fs = require("fs");
const path = require("path");
const { JSDOM } = require("jsdom");

const html = fs.readFileSync(path.join(__dirname, "..", "page.html"), "utf8");
const m = html.match(/<script>([\s\S]*)<\/script>/);
if (!m) { console.error("FAIL: no script block"); process.exit(1); }
fs.writeFileSync(path.join(__dirname, "served-script.js"), m[1]);

let failures = 0;
function assert(cond, msg) {
  if (cond) console.log("ok - " + msg);
  else { failures++; console.log("FAIL - " + msg); }
}

const fetchCalls = [];
const dom = new JSDOM(html, {
  runScripts: "dangerously",
  url: "http://127.0.0.1/",
  beforeParse(window) {
    window.fetch = (url) => {
      fetchCalls.push(url);
      if (url === "/api/hash") {
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ name: "x", size: 3, crc32: "AABBCCDD", md5: "md5x", sha1: "sha1x" }), headers: { get: () => null } });
      }
      if (url === "/api/patchinfo") {
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ name: "p", size: 10, crc32: "00000000", format: "bps", formatName: "BPS", hasSourceCRC: true, sourceCRC: "AABBCCDD", hasTargetCRC: true, targetCRC: "11223344", sourceSize: 3, targetSize: 3 }), headers: { get: () => null } });
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

function effectiveVisible(el) {
  let e = el;
  while (e && e !== document.documentElement) {
    if (e.style && e.style.display === "none") return false;
    e = e.parentElement;
  }
  return true;
}

(async () => {
  await sleep(50);
  // --- ページ構造: 3 ページとも BODY 直下 ---
  for (const n of ["apply", "create", "settings"]) {
    const p = document.getElementById("page-" + n);
    assert(!!p, "page-" + n + " exists");
    assert(p && p.parentElement && p.parentElement.tagName === "BODY", "page-" + n + " parent is BODY");
  }
  // --- 初期表示とタブ切替 (実効表示で確認) ---
  assert(effectiveVisible(document.getElementById("page-apply")), "apply visible at start");
  assert(!effectiveVisible(document.getElementById("page-create")), "create hidden at start");
  assert(!effectiveVisible(document.getElementById("page-settings")), "settings hidden at start");
  document.getElementById("tabbtn-create").click();
  assert(effectiveVisible(document.getElementById("page-create")) && !effectiveVisible(document.getElementById("page-apply")), "tab switch to create works");
  document.getElementById("tabbtn-settings").click();
  assert(effectiveVisible(document.getElementById("page-settings")) && !effectiveVisible(document.getElementById("page-create")), "tab switch to settings works");
  document.getElementById("tabbtn-apply").click();
  assert(effectiveVisible(document.getElementById("page-apply")), "tab switch back to apply works");

  // --- 辞書: 全キー × 12言語、t() がキー名を返さない、説明文が入っている ---
  const LANGS = window.eval("LANGS");
  const enKeys = window.eval("Object.keys(I18N.en)");
  assert(enKeys.length >= 40, "dictionary has >=40 keys (" + enKeys.length + ")");
  for (const lang of LANGS) {
    const missing = window.eval(`Object.keys(I18N.en).filter(k => !I18N[${JSON.stringify(lang)}][k])`);
    assert(missing.length === 0, "lang " + lang + " has all keys" + (missing.length ? " missing: " + missing.join(",") : ""));
    window.eval(`LANG = ${JSON.stringify(lang)}; applyLang();`);
    for (const k of ["tabApply", "descApply", "descCreate", "descSettings", "crcInfo", "formatsApply", "formatsCreate"]) {
      const v = window.eval(`t(${JSON.stringify(k)})`);
      assert(v && v !== k, "t(" + k + ") in " + lang + " returns text");
    }
    const desc = document.querySelector('#page-apply [data-i18n="descApply"]').textContent;
    assert(desc.length > 20 && desc.includes("\n"), "descApply rendered with sentence line breaks in " + lang);
    const crc = document.querySelector('#page-apply [data-i18n="crcInfo"]').textContent;
    assert(crc.length > 40 && crc.includes("\n"), "crcInfo rendered in " + lang);
    const dc = document.querySelector('#page-create [data-i18n="descCreate"]').textContent;
    assert(dc.length > 20, "descCreate rendered in " + lang);
    const ds = document.querySelector('#page-settings [data-i18n="descSettings"]').textContent;
    assert(ds.length > 20, "descSettings rendered in " + lang);
  }
  window.eval(`LANG = "en"; applyLang();`);

  // --- ドラッグ&ドロップ: 全ファイル欄 ---
  for (const id of ["apply-rom", "apply-patch", "create-orig", "create-mod"]) {
    const ff = document.getElementById("ff-" + id);
    const over = new window.Event("dragover", { cancelable: true, bubbles: true });
    ff.dispatchEvent(over);
    assert(over.defaultPrevented, "dragover prevented on ff-" + id);
    assert(ff.classList.contains("drag"), "dragover highlights ff-" + id);
    const drop = new window.Event("drop", { cancelable: true, bubbles: true });
    drop.dataTransfer = { files: [new window.File(["abc"], "drop-" + id + ".bin")] };
    ff.dispatchEvent(drop);
    assert(drop.defaultPrevented, "drop prevented on ff-" + id);
    await sleep(30);
    assert(document.getElementById("fn-" + id).textContent === "drop-" + id + ".bin", "drop sets file on ff-" + id);
  }
  assert(fetchCalls.includes("/api/hash"), "hash API called after rom drop");
  assert(fetchCalls.includes("/api/patchinfo"), "patchinfo API called after patch drop");

  // --- CRC 表示と照合マーク ---
  const romMeta = document.getElementById("meta-apply-rom").textContent;
  assert(romMeta.includes("AABBCCDD"), "ROM CRC32 shown next to ROM field");
  const patchMeta = document.getElementById("meta-apply-patch").textContent;
  assert(patchMeta.includes("BPS"), "patch format shown next to patch field");
  assert(patchMeta.includes("AABBCCDD"), "expected CRC shown next to patch field");
  const ind = document.getElementById("match-ind");
  assert(!!ind && ind.className === "ok", "CRC match indicator shows OK");

  // --- 設定が効くこと: MD5 表示 ON で再描画される ---
  window.eval(`settings.showMD5 = true; applyLang();`);
  assert(document.getElementById("meta-apply-rom").textContent.includes("md5x"), "MD5 shown when enabled in settings");
  window.eval(`settings.showMD5 = false; applyLang();`);

  console.log(failures === 0 ? "\nALL JSDOM CHECKS PASSED" : "\n" + failures + " FAILURES");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.error("FAIL: exception", e); process.exit(1); });
