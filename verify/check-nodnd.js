// UniPatcher ドラッグ&ドロップ表記の検証:
//  - Android シェル相当 (UniBridge スタブあり) では、表示テキストの
//    どこにもドラッグ&ドロップの文言が出ないこと (ja / en で確認し、
//    tapHint キーが全12言語にあることも確認する)。
//  - スタブなし (Windows 相当) では、従来の dropHint が表示されること。
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

// 各言語のドラッグ&ドロップ相当語 (小文字で照合)
const DND_WORDS = ["ドラッグ", "drag", "glisser", "déposer", "ziehen", "trascina",
  "arrastra", "suelta", "перетащ", "arraste", "przeciągn", "upuść",
  "드래그", "拖放", "拖拽"];

function visibleText(document) {
  const parts = [];
  document.body.querySelectorAll("*").forEach((el) => {
    if (el.tagName === "SCRIPT" || el.tagName === "STYLE") return;
    if (el.children.length === 0 && el.textContent) parts.push(el.textContent);
  });
  return parts.join("\n");
}

function makeDom(withBridge) {
  return new JSDOM(html, {
    runScripts: "dangerously",
    url: "http://127.0.0.1/",
    beforeParse(window) {
      if (withBridge) {
        window.UniBridge = {
          beginSave() { return true; },
          appendSave() { return true; },
          finishSave() { return true; },
          cancelSave() {},
        };
      }
      window.fetch = () => Promise.resolve({ ok: true, status: 200, json: async () => ({}), headers: { get: () => null }, blob: async () => new window.Blob([]) });
      if (!window.URL.createObjectURL) window.URL.createObjectURL = () => "blob:fake";
      if (!window.URL.revokeObjectURL) window.URL.revokeObjectURL = () => {};
    }
  });
}

(async () => {
  // --- tapHint キーが全12言語にある ---
  {
    const dom = makeDom(false);
    const { window } = dom;
    await sleep(30);
    const LANGS = window.eval("LANGS");
    for (const lang of LANGS) {
      const v = window.eval(`I18N[${JSON.stringify(lang)}].tapHint`);
      assert(typeof v === "string" && v.length > 0, "tapHint exists in " + lang);
      const low = (v || "").toLowerCase();
      assert(!DND_WORDS.some((w) => low.includes(w)), "tapHint in " + lang + " has no drag & drop wording");
    }
    window.close();
  }

  // --- Android 相当: 文言が消えている ---
  {
    const dom = makeDom(true);
    const { window, } = dom;
    const { document } = window;
    await sleep(50);
    assert(window.eval("IS_ANDROID") === true, "IS_ANDROID true with UniBridge stub");
    for (const lang of ["ja", "en"]) {
      window.eval(`LANG = ${JSON.stringify(lang)}; applyLang();`);
      const text = visibleText(document).toLowerCase();
      const hit = DND_WORDS.filter((w) => text.includes(w));
      assert(hit.length === 0, "no drag & drop wording visible in Android mode (" + lang + ")" + (hit.length ? " found: " + hit.join(",") : ""));
      const hint = document.querySelector('[data-i18n="dropHint"]').textContent;
      const tap = window.eval(`t("tapHint")`);
      assert(hint === tap && hint.length > 0, "hint shows tapHint text in Android mode (" + lang + "): " + hint);
    }
    window.close();
  }

  // --- Windows 相当: 従来文言のまま ---
  {
    const dom = makeDom(false);
    const { window } = dom;
    const { document } = window;
    await sleep(50);
    assert(window.eval("IS_ANDROID") === false, "IS_ANDROID false without bridge");
    for (const lang of ["ja", "en"]) {
      window.eval(`LANG = ${JSON.stringify(lang)}; applyLang();`);
      const hint = document.querySelector('[data-i18n="dropHint"]').textContent;
      const orig = window.eval(`t("dropHint")`);
      assert(hint === orig && hint.length > 0, "hint shows original dropHint without bridge (" + lang + "): " + hint);
    }
    const text = visibleText(document).toLowerCase();
    assert(DND_WORDS.some((w) => text.includes(w)), "drag & drop wording still visible without bridge (en)");
    window.close();
  }

  console.log(failures === 0 ? "\nALL NO-DND CHECKS PASSED" : "\n" + failures + " FAILURES");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.error("FAIL: exception", e); process.exit(1); });
