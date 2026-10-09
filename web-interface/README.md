# Mist web interface

The authenticated React portal serves real team Jobs, hardware inventory,
dataset uploads, common/member folder browsing, account administration and team
policy/sharing controls. Production: **http://100.73.139.66:8088** through Tailscale.
See the [operating guide](../docs/department-rollout.md).

## Start locally

Use Node.js **22.16.0 or newer**. The main machine's existing API port-forward
listens on `127.0.0.1:3000`; its Vite development service uses port 3001.
Check those services before starting duplicate listeners:

```bash
systemctl --user status mist-api-forward mist-web
```

With the API/port-forward available and port 3001 free, from this directory:

```bash
npm ci
npm run dev -- --host 127.0.0.1 --port 3001 --strictPort
```

Vite forwards `/api` and `/auth` to the API on port 3000. `MIST_API_PROXY` changes
that target. Authenticate with a real Mist account and select your team; this
preview reaches real jobs/data. See [development](../docs/development.md) for
other-host origins and backend work. Production uses a static Nginx release,
not Vite or these per-user services.

## Responsibilities

- `src/auth.tsx`, `teams.tsx`: account/team context; server authorization remains
  authoritative for every operation.
- `src/api.ts`: typed API models/requests and upload progress.
- `src/components/JobsPage.tsx`, `components/jobs/`: paginated history, focused
  submission dialog, actual logs, cancellation and saved files.
- `src/components/DatasetsPage.tsx`, `TeamStorageBrowser.tsx`: uploads and
  authorized common/member/shared storage browsing.
- `src/components/TeamsPage.tsx`, `AccountPage.tsx`: team policy/membership/sharing,
  member administration and password controls.
- `src/components/Picker.tsx`, `Tabs.tsx`, `Modal.tsx`, `Card.tsx`: shared controls.
- `src/components/HardwarePanel.tsx`, `MachinesPage.tsx`: live node/device inventory.
- `src/hooks/usePolling.ts`: independent polling, cancellation and recovery.
- `src/routes/`: TanStack file routes; `routeTree.gen.ts` is generated.

Compute choices request whole GPUs or TT boards. One TT board has two chips.
Custom tagged images follow team registry policy and need compatible libraries;
TT may use the tested host profile or an image-provided runtime. Container mode
preserves image startup with blank overrides. CPU/RAM inventory shows allocation,
not live utilization. Inputs are read-only; save results in `/outputs`.

The old fake account helpers/chart and unused `use-immer` dependency are retired.
Current behavior and components follow [the design system](../docs/design-system.md).

## Verify

```bash
npm test
npx tsc --noEmit
npm run lint
npm run build
```

See [testing](../docs/testing.md) for browser-to-cluster checks and their scope.
