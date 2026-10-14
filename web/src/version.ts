// An installed app has no browser reload, so a changed server version is the
// signal that the frontend it loaded is stale.

/** Remembers the first server version it sees; true once a later one differs. */
export function versionWatch(): (version: string | undefined) => boolean {
  let first: string | undefined
  return (version) => {
    if (version === undefined) return false
    first ??= version
    return version !== first
  }
}
