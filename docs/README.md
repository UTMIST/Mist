# Mist Documentation

This directory contains the documentation site for Mist, powered by **Hugo** and the **Hextra** documentation theme.

## Viewing Documentation Locally

To preview the documentation locally with live-reload:

```bash
cd docs
hugo server
```

The site will be available at `http://localhost:1313/docs/`.

To build the static site (outputting to `docs/public`, which Vite bundles into `web-interface/dist/docs`):

```bash
cd docs
hugo
```

## Documentation Guides

Documentation source files are located in `content/`:

| Guide | Description |
|---|---|
| [Department operating guide](content/department-rollout.md) | Sign in, manage teams, upload data, submit/download jobs |
| [Architecture](content/architecture.md) | Service topologies, boundaries, and codebase layout |
| [k3s setup](content/k3s-setup.md) | Cluster setup, worker nodes, and hardware acceleration |
| [Tailscale and SSH](content/tailscale-ssh.md) | Remote secure networking and administrator SSH |
| [Storage](content/storage.md) | Dataset and model shared storage pools |
| [Developer setup](content/development.md) | Local frontend and backend development |
| [Testing](content/testing.md) | Automated tests and evidence runs |
| [Design system](content/design-system.md) | Portal design principles and components |
| [Frontend architecture](content/frontend-architecture.md) | UI layout and backend offloading plan |
| [Web & docs deployment](content/web-deployment.md) | Vercel build and multi-provider deployment guide |
| [Execution record](content/department-rollout-execution.md) | Department rollout status and validation |
| [Cleanup record](content/repository-cleanup.md) | Repository maintenance history |
| [Historical archive](content/archive/_index.md) | Archived previous-generation guides and plans |
