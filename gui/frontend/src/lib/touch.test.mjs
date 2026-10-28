import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";

// Use the project's compiler so the tests also run on Node 20 (release CI).
const { outputText } = ts.transpileModule(
  readFileSync(new URL("./touch.ts", import.meta.url), "utf8"),
  { compilerOptions: { module: ts.ModuleKind.ESNext } }
);
const { inputOf, isTapOnHidden } = await import(
  `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`
);

test("a finger is touch; the mouse and a precise pen are not", () => {
  assert.equal(inputOf({ pointerType: "touch" }), "touch");
  assert.equal(inputOf({ pointerType: "mouse" }), "mouse");
  assert.equal(inputOf({ pointerType: "pen" }), "mouse");
});

test("a finger on a hover-only control that is still hidden passes through", () => {
  assert.equal(isTapOnHidden({ pointerType: "touch" }, "0"), true);
  // Fading in: a finger can't have hovered it long enough to mean it.
  assert.equal(isTapOnHidden({ pointerType: "touch" }, "0.2"), true);
});

test("a shown control takes the tap, and the mouse always clicks it", () => {
  assert.equal(isTapOnHidden({ pointerType: "touch" }, "1"), false);
  assert.equal(isTapOnHidden({ pointerType: "mouse" }, "0"), false);
});
