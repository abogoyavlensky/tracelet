# A deploy during sign-in is not flagged as a new version

**Status: open**

## Problem

The shell records the server version the first time it reads the health
endpoint (`serverVersion` in `web/src/components/AppShell.tsx`) and shows a dot
on the refresh button when a later read differs. `AppShell` does not mount on
`/login`. If the login page stays open across a deploy and the user then signs
in, the first health read already returns the new version while the browser
still runs the old bundle. The new version becomes the baseline, the dot never
appears, and refresh only re-runs queries.

Found by the codex review of the refresh button. It is narrow: it needs the
login page left open across a deploy, and a reload of any kind fixes it.

## Proposed fix

Take the baseline at page load instead of at shell mount: read the health
endpoint once in `main.tsx` (or the root route) and feed the first version to
`versionWatch`. Do not bake a version into the Vite build; the plan for the
installable UI rejected that because it disagrees with dev builds and dirty
trees.
