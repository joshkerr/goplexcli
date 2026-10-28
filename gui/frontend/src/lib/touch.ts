// Touch on a Mac with a touch display (macOS 27). macOS turns a finger's tap
// into a click and a tap-and-hold into a context menu, so the app works by
// touch as it is. This tracks which input is in use for the few places a
// mouse idiom has to tell them apart:
//
//  - <html data-input="touch"> while a finger is in use. Under it index.css
//    grows a small control's hit area (.touch-target) to Apple's 36pt touch
//    target and gives menus taller rows, as Apple's own controls do; the
//    mouse keeps its precision and nothing looks different.
//  - isTapOnHidden(): a finger landing where a hover-only control sits while
//    it is still hidden. A finger can't hover, so the tap is meant for what
//    is under the control.

/** The input a pointerdown comes from. */
export function inputOf(e: { pointerType: string }): "touch" | "mouse" {
  return e.pointerType === "touch" ? "touch" : "mouse";
}

/** Whether a press on a hover-only control, whose computed opacity was
 * `opacity` as it landed, is a finger on it while it was still hidden. */
export function isTapOnHidden(e: { pointerType: string }, opacity: string): boolean {
  return inputOf(e) === "touch" && Number(opacity) < 0.5;
}

/** Follow the input in use; once, at startup. */
export function trackInput(): void {
  const root = document.documentElement;
  // Capture, so the mode is set before the press reaches any handler.
  window.addEventListener(
    "pointerdown",
    (e) => {
      root.dataset.input = inputOf(e);
    },
    true
  );
  // Back to the mouse the moment it moves, before it can click.
  window.addEventListener(
    "pointermove",
    (e) => {
      if (e.pointerType === "mouse" && root.dataset.input === "touch") root.dataset.input = "mouse";
    },
    true
  );
}
