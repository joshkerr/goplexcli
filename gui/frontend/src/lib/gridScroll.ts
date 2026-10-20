export interface GridLayout {
  cols: number;
  rowH: number;
}

// Keep the first visible item and the fractional offset into its row when
// resizing changes the number of columns or the measured poster height.
export function reflowScrollTop(
  scrollTop: number,
  previous: GridLayout,
  next: GridLayout,
  padding: number
): number {
  if (scrollTop <= padding) return scrollTop;
  const rowPosition = (scrollTop - padding) / previous.rowH;
  const row = Math.floor(rowPosition);
  const item = row * previous.cols;
  return padding + (Math.floor(item / next.cols) + rowPosition - row) * next.rowH;
}
