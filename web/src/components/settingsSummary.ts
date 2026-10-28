// settingsSummary is what a closed SettingsSection says beside its title: how
// many it holds, or None. Nothing while the list is still loading, rather than
// a None that is not yet known to be true.
export function settingsSummary(
  count: number | null | undefined,
  one: string,
  many: string,
): string | undefined {
  if (count === null || count === undefined) return undefined;
  if (count === 0) return "None";
  return `${count} ${count === 1 ? one : many}`;
}
