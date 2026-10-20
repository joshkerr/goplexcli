import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";

// Use the project's compiler so the tests also run on Node 20 (release CI).
const { outputText } = ts.transpileModule(
  readFileSync(new URL("./gridScroll.ts", import.meta.url), "utf8"),
  { compilerOptions: { module: ts.ModuleKind.ESNext } }
);
const { reflowScrollTop } = await import(
  `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`
);

test("opening a detail pane keeps the first visible poster and row offset", () => {
  // Item 200 is a quarter-row above the viewport in a five-column grid.
  assert.equal(
    reflowScrollTop(24 + 40.25 * 320, { cols: 5, rowH: 320 }, { cols: 2, rowH: 350 }, 24),
    24 + 100.25 * 350
  );
});

test("closing a pane keeps the anchor in the first visible row", () => {
  // Item 202 moves into the row starting at item 200 when the grid widens.
  assert.equal(
    reflowScrollTop(24 + 101.5 * 350, { cols: 2, rowH: 350 }, { cols: 5, rowH: 320 }, 24),
    24 + 40.5 * 320
  );
});

test("measured row-height corrections preserve the visible row", () => {
  assert.equal(
    reflowScrollTop(24 + 100.5 * 350, { cols: 2, rowH: 350 }, { cols: 2, rowH: 330 }, 24),
    24 + 100.5 * 330
  );
});

test("resizing at the top does not scroll past the top padding", () => {
  for (const top of [0, 12, 24]) {
    assert.equal(reflowScrollTop(top, { cols: 5, rowH: 320 }, { cols: 2, rowH: 350 }, 24), top);
  }
});
