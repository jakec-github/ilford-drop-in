// Whole percentages that still add up to 100: each share rounds down, and the
// points lost go to the shares that lost the most (largest remainder).
export function wholeShares(counts: number[], total: number): number[] {
  const exact = counts.map((c) => (c / total) * 100);
  const shares = exact.map(Math.floor);
  let left = 100 - shares.reduce((a, b) => a + b, 0);
  const byRemainder = exact
    .map((e, i) => ({ i, rest: e - Math.floor(e) }))
    .sort((a, b) => b.rest - a.rest);
  for (const { i } of byRemainder) {
    if (left-- <= 0) break;
    shares[i] += 1;
  }
  return shares;
}
