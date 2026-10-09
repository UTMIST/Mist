# Mist web interface

The `/jobs` page submits real Kubernetes jobs. `/machines` shows live hardware
inventory. Other routes retain their prototype behavior.

## Start locally

Requires Node.js 22.16.0 or newer. Start the Mist API or its port-forward on
`127.0.0.1:3000`, then run:

```bash
npm install --package-lock=false
npm run dev -- --host 127.0.0.1 --port 3001 --strictPort
```

Open http://127.0.0.1:3001/jobs. Existing user services on this server already
run these endpoints: `systemctl --user status mist-api-forward mist-web`.

Vite proxies `/api` to port 3000; `MIST_API_PROXY` changes the target.
For a production build, configure a same-origin `/api` reverse proxy or set
`VITE_API_URL` at build time. The development proxy is not part of the static build.

## Jobs and hardware

- Python scripts, approved container image references, and small training checks.
- Whole NVIDIA GPUs and Tenstorrent boards; each board contains two chips.
- Image defaults, executable/argument overrides, environment variables,
  optional working directory, CPU/RAM requests, and deadline.
- Real waiting/running/completed/failed/cancelled states, placement, logs,
  exit codes, and cancellation.
- Live whole-device totals, allocations, available counts, and node readiness.
- Persistent output location `/outputs` on the selected node. Uploads and
  browser result downloads are subsequent work.

Image eligibility is server policy, not a guarantee of CUDA/TT compatibility.
TT currently uses its tested image and host runtime. Availability is a snapshot;
Kubernetes controls allocation. CPU/RAM are allocatable capacity, not live usage.

This pilot has one configured owner and no per-request authentication. See
[the foundation guide](../docs/local-job-foundation.md) for exact API contracts,
examples, storage behavior, local access, and remaining work.

## Code organization

- `src/api.ts`: typed API requests and response models.
- `src/hooks/usePolling.ts`: independent polling, cancellation and recovery.
- `src/components/JobsPage.tsx`: page data and actions.
- `src/components/jobs/`: submission form, job cards, log viewer.
- `src/components/HardwarePanel.tsx`: shared live inventory table.
- `src/components/MachinesPage.tsx`: machine inventory route content.
- `src/routes/`: TanStack Router file routes; `routeTree.gen.ts` is generated.

Keep hardware policy/execution in the API. Components should describe the
response and request fields without reproducing the Kubernetes scheduler.

## Verify

```bash
npm test
npx tsc --noEmit
npm run build
```

Interaction tests cover state/log/cancel flows, TT board submission, connection
recovery, literal arguments and working-directory settings, and invalid image/
count rejection. Browser-to-cluster checks are documented in
[deployment verification](../deploy/k3s/README.md#foundation-acceptance).

The app uses React, TypeScript, TanStack Router, Vite, and Tailwind CSS.
Follow the existing Card/Button styles and explicit form labels. Check only
changed files with Prettier/ESLint rather than rewriting unrelated prototypes.
