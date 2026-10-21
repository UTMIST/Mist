# Web Interface and Documentation Deployment (Agent Reference)

This document outlines the deployment configuration for Mist's unified web presence (React SPA + Hugo static documentation).

## Core Architecture

- **Web interface**: Located in `web-interface/`. Built with Vite, React 19, Tailwind CSS 4, and TanStack Router.
- **Documentation site**: Located in `docs/`. Built with Hugo (Hextra theme) and Hugo Modules.
- **Combined target**: Both compile into `web-interface/dist/`. The web app resides at the root `/`, and documentation resides under `/docs/`.

## Vercel Target Setup

- **Root config**: `vercel.json` in repository root handles deployments where Vercel root is `.`.
- **Subpath config**: `web-interface/vercel.json` handles deployments where Vercel Root Directory is `web-interface`.
- **Prerequisites**: Hugo and Node.js/npm installed on the build environment with outgoing internet access.
- **Build command**: `npm run typecheck && npm run build` (or `npm --prefix web-interface ...`).
- **Rewrite rule**: `/((?!api(?:/|$)|auth(?:/|$)|docs(?:/|$)).*)` -> `/index.html`.
- **Redirect rule**: `/docs` -> `/docs/` (301 permanent).

## Switching Providers Requirements

When migrating to another static host or container platform:
1. Ensure Node.js >= 22 is available during build.
2. Publish `web-interface/dist`.
3. Route `/docs/*` directly to static files; DO NOT apply SPA fallback (`/index.html`) to `/docs/*`.
4. Redirect `/docs` to `/docs/`.
5. Point unmatched paths (excluding `/api`, `/auth`, `/docs`) to `/index.html` (HTTP 200).
6. Provide `VITE_API_URL` if Mist API is not served under the same origin.
