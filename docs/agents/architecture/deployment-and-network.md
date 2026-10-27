# Deployment and network

Covers which host runs what, how hosts and users connect, where configuration
and secrets live, admin access, and how nodes are added. Overview:
[ARCHITECTURE.md](../../../ARCHITECTURE.md). Related decisions: D7, D10, D13.

## Topology

```mermaid
flowchart LR
    Users([Researchers + admins]) -->|Tailscale| TSN
    Users -->|Tailscale| TST
    subgraph NV["utmist-z1opa08 · 56 CPU · 157 GiB · 2× A4000"]
        TSN["tailscale0<br/>100.73.139.66"] -->|"portal HTTPS :443"| NVsvc["k3s server · mistd api + scheduler<br/>Postgres · Nginx portal"]
        NVgpu[gpu partition]
        ens6["ens6 · AQC107 10GBASE-T<br/>192.168.240.1"]
    end
    subgraph TT["utmist-tt · 32 CPU · 503 GiB · 4× n300"]
        TST["tailscale0<br/>100.95.175.37"] -->|"ssh :22"| TTsvc["k3s agent · NFS /mist · sshd + mist-gate<br/>mist-relay · provisioner"]
        TTpart[cpu + tt partitions]
        x710["enp10s0f0np0 · X710 10GBASE-T<br/>192.168.240.2"]
    end
    ens6 <-->|"Cat6a direct link, 10 GbE<br/>k3s, NFS, agents to API"| x710
    LAN[("Home LAN / internet<br/>10.0.0.0/24")]
    NV --- LAN
    TT --- LAN
```

## Hosts

| | `utmist-z1opa08` (NVIDIA) | `utmist-tt` (QuietBox) |
|---|---|---|
| Hardware | 56 CPUs, 157 GiB RAM, 2× RTX A4000 16 GB, 2× 447 GB SATA SSD | 32 CPUs, 503 GiB RAM, 4× n300 (8 chips), 3.6 TB NVMe |
| OS | Ubuntu 22.04, kernel 6.8, cgroup v2 | same |
| Roles | k3s server; `mistd api` + `mistd scheduler`; Postgres; portal (Nginx); `gpu` partition; backup target (M3) | k3s agent; `/mist` volume 1 + NFS server; login gateway (sshd, `mist-gate`, `mist-relay`); `mistd provision`; `cpu` and `tt` partitions |

## Networks

| Network | Addresses | Carries |
|---|---|---|
| **Direct link (10 GbE)** | NVIDIA `ens6` `192.168.240.1/24`, TT `enp10s0f0np0` `192.168.240.2/24` | k3s node traffic and Flannel, NFS, relay and provisioner → API, backups |
| LAN | NVIDIA `10.0.0.175` (currently over Wi-Fi), TT `10.0.0.112` | Internet access only |
| Tailscale | NVIDIA `100.73.139.66`, TT `100.95.175.37` | Users: portal (NVIDIA HTTPS :443), SSH login (TT :22); admin SSH |

**Moving the direct link to 10 GbE (M1):**

- **Cable:** Cat6a (or Cat6 under about 50 m) between the NVIDIA box's `ens6`
  (Aquantia AQC107) and the TT box's `enp10s0f0np0` (Intel X710). Both are
  10GBASE-T (RJ45) ports.
- **Addresses:** move `192.168.240.1/.2` from the 1 GbE I210 ports to those two
  ports, using a `/24` so more nodes can join.
- **MTU:** jumbo frames (9000) on both ends are optional, but help NFS.
- **Check:** `ethtool <iface>` shows `Speed: 10000Mb/s`. An `iperf3` run should
  reach above 9 Gb/s.

**Wired LAN.** The NVIDIA box should also reach the LAN over wired Ethernet
rather than Wi-Fi; one of its I210 ports frees up once the direct link moves.

**k3s** is installed fresh, with `--node-ip` and `--flannel-iface` on the direct
link, so cluster traffic never uses the LAN or Wi-Fi.

## Services

| Service | Host | Runs as |
|---|---|---|
| k3s server and agent | both | systemd |
| NVIDIA GPU Operator (device plugin + container toolkit) | NVIDIA | k3s |
| Tenstorrent DRA driver (M4) | TT | k3s |
| Postgres | NVIDIA | k3s StatefulSet, data on local SSD (never NFS) |
| `mistd api`, `mistd scheduler` | NVIDIA | k3s Deployments (1 replica each), reachable on `192.168.240.1` for agents |
| Portal (Nginx + static build) | NVIDIA | systemd, HTTPS on Tailscale (production), proxies `/api`; `:8088` development only |
| NFS server | TT | systemd |
| sshd + `mist-gate` | TT | systemd |
| `mist-relay` | TT | systemd (root) |
| `mistd provision` | TT (later every node) | systemd (root), includes gateway revocation/watchdog integration |
| `mistd cleanup-report` | both compute nodes | root-owned node service; narrowly scoped cleanup evidence |
| `mist-noded` (M5) | nodes moved to the node executor | systemd (root) |

## Configuration and secrets

- **Configuration in Git:** `deploy/` (manifests, systemd units, sshd and
  exports snippets, host setup scripts).
- **Settings in the database:** partitions, QoS, limits, volumes. Admins change
  them through the API.
- **Secrets never go in Git:**
  - Postgres credentials, the relay token and per-host agent tokens.
  - Kubernetes Secrets for in-cluster use; `/etc/mist/*.token` (0600 root) on
    hosts.
  - Rotate any secret that has been pasted into a chat or document.

## Admin access

- **Separate admin accounts** for each admin, with SSH keys only, `sudo`, and
  not in `mist-users`. Researcher accounts never have `sudo` or `docker`.
- **Password SSH disabled** on both hosts. The shared `utmist` and `utmist-tt`
  passwords are retired once admin accounts exist.
- **Clean up existing host state** after checking with whoever created it:
  - leftover local accounts (`dev`, `terrymist`, `labadmin`, `labmember`)
  - the `ollama` service and group
  - Docker
- **Device files stay `0666`** (D13). This is safe only because researcher
  logins can't run code and unmanaged GPU consumers are removed.

## Starting fresh

Before M1 goes live:

1. Stop and remove the previous platform's API, namespaces (`mist-team-*`),
   ConfigMaps and Jobs.
2. Keep the old storage folders untouched until a human confirms they can be
   deleted.
3. Reinstall k3s on the direct link.

## Adding a node

1. **Connect it to the direct-link network.** For three or more nodes, use a
   10 GbE switch with all nodes on `192.168.240.0/24`. A second direct cable is
   not automatically the same L2 network: an alternative needs an explicitly
   designed bridge or routed subnet and routing/firewall tests before use.
2. **Install Ubuntu** with the drivers for its accelerators.
3. **K3s executor:** join it as a k3s agent and install its device plugin. Node
   executor: install `mist-noded` and `mistd provision`.
4. **Register it in Mist:** add it to a partition (new or existing) and set its
   reserved host share.
5. **Run the executor conformance suite** on it, then mark it `up`.

## Work by milestone

- **M1:**
  - 10 GbE link and wired LAN
  - fresh k3s
  - Postgres
  - deploy `mistd` and the portal
  - gateway host setup
  - admin accounts and secret rotation
  - remove the old platform
  - job/host firewall enforcement, authorization lease watchdog and cleanup reporter
  - basic monitoring: disk headroom, stale directory/provisioning generations,
    failed cleanup, quarantined nodes, scheduler leadership and launch timeouts
  - pin k3s/kernel/runtime/plugins and record tested feature support
- **M3:** backup target, retention and restore exercises; richer monitoring UI.

## Open questions

- Monitoring and alerting stack.
- Whether the NVIDIA box's `sdb` is free for scratch and backups.
- A stable login hostname.

## Job network boundary (M1)

Deployment owns host firewall rules; Executors owns job namespace/NetworkPolicy
configuration. Both must pass the same conformance checks before a node is up.

- Tag job traffic by its network interface/source before NAT. Filter both host
  INPUT and routed/FORWARD paths; a pod's own host is not implicitly trusted.
  Deny new job connections to every host address (direct-link, LAN, Tailscale,
  pod gateway), Kubernetes service/pod CIDRs, RFC1918, CGNAT and link-local ranges.
  Include locally bound public addresses, if added later. Allow only approved
  DNS resolvers on TCP/UDP 53 and public internet egress; allow established
  response traffic. Deny unsolicited inbound traffic to jobs.
- Host-origin NFS mounts, control-plane traffic, cleanup reporting and admin SSH
  are separate trusted flows. Never open NFS from the job network to make a
  mount work; mounts are performed by the host. Block job access to host runtime
  sockets and Unix sockets through the filesystem too.
- M1 job networking is IPv4-only; disable IPv6 in job namespaces/routing. Enable
  it only after equivalent IPv6/link-local rules pass tests. Startup must fail
  closed if policy installation fails; rule reload and k3s restart must not
  create an allow-all interval. Pin/test the actual CNI and host firewall ordering.
- Test from CPU jobs on the storage host and GPU jobs on the control host: own
  host and peer SSH/NFS/API/Postgres/kubelet ports, service IP aliases, pod peers,
  LAN/Tailscale peers, DNS, internet HTTPS, and IPv6 bypass attempts. Also verify
  legitimate host NFS mounts and admin access remain functional.

Use authenticated TLS for relay/provisioner/cleanup traffic on the direct link;
per-host bearer tokens authorize operations but do not encrypt them. Expose the
production portal through a stable Tailscale HTTPS name with a trusted certificate.
Record the selected hostname/certificate provisioning in deployment configuration.
Restrict internal API listeners to trusted hosts; user-facing proxy routes never
expose `/internal`. A clean restart must restore policies before admitting jobs.
