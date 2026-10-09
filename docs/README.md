# Documentation

Current main-branch documentation, verified against the two running machines on
**October 9, 2026**. Addresses and versions describe this installation; adjust
host-specific settings when provisioning another environment.

## Find the right guide

| Task | Guide |
|---|---|
| Sign in, manage teams, upload data, submit/download jobs | [Department operating guide](department-rollout.md) |
| Understand services and source layout | [Architecture](architecture.md) |
| Install/inspect k3s, join a worker, verify accelerator allocation | [k3s setup](k3s-setup.md) |
| Connect privately and administer either host | [Tailscale and SSH](tailscale-ssh.md) |
| Understand dataset/model pools and physical capacity | [Storage](storage.md) |
| Build, deploy, restart or troubleshoot services | [Deployment operations](../deploy/private/README.md) |
| Develop locally with the existing authenticated API | [Developer setup](development.md) |
| Authenticate and submit from a terminal | [CLI](../cli/docs/setup.md) |
| Run automated checks and read recorded live evidence | [Testing](testing.md) |
| Maintain UI components and interactions | [Design system](design-system.md) |
| Read completed scope, exclusions and release verification | [Execution record](department-rollout-execution.md) |
| See what repository cleanup removed and verified | [Cleanup record](repository-cleanup.md) |

## Access and execution

```text
Browser / CLI → Tailscale portal :8088 → Nginx → Go API → k3s
                                                 |       |
                                             Better Auth + team policy
                                                         |
                                       CPU/NVIDIA or Tenstorrent Job
                                                         |
                                   scoped dataset/model NFS mounts on QuietBox
```

Mist login is separate from Tailscale membership and Linux SSH login. Researchers
use the portal; administrator SSH is restricted by the installed host rules.
The worker reaches the control plane and storage through LAN addresses.

[Earlier plans and single-owner guides](archive/README.md) are historical only.
Current guidance does not require Redis, an SSH session to launch a job, a public
cloud load balancer, or permission to approve every tagged image.
