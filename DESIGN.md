# DESIGN.md (automationshutdown)

An internal ops console: one screen, one decision. Shut down every matching VM, or not.

Dials: ENERGY 1 / RHYTHM 1 / MOTION 1. A calm tool for a tense moment, not a landing page.

## Decisions and reasons (one line each)

- Neutral HeroUI palette plus a single red `danger` accent only on the shutdown button and force-stop states, so the destructive moment reads instantly.
- No gradients, no glass, no decorative grids, so it looks like infra tooling instead of an AI template.
- Inter for UI (tabular-nums keep the VMID column stable) and JetBrains Mono for VMIDs plus log timestamps, because the realtime log is read as data.
- Preview table columns VMID, name, node, type, tags: only the fields that decide the run.
- The confirm modal always states the target count and the dry-run mode, because the action cannot be undone.
- Light/dark follows the system (default system), because operators rotate across shifts and devices.
- Honest empty state ("No running vm right now...") plus an error state with retry, because the preview depends on a Proxmox that can be down.
