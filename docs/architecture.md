# Architecture

Current private department pilot, October 9, 2026. See
[department rollout](department-rollout.md) for permissions, resource/storage
rules, the API contract and operating limits.

```mermaid
flowchart LR
    Web[Browser / CLI] --> Portal[Nginx private Tailscale portal]
    Portal --> API[Go API + single admission controller]
    API --> Auth[Better Auth + SQLite PVC]
    API --> State[Team policies + durable queued Jobs]
    State --> K8s[k3s scheduler and device operators]
    K8s --> Main[CPU / NVIDIA main node]
    K8s --> TT[QuietBox Tenstorrent node]
    Main --> Store[Scoped NFS team filesystems on QuietBox]
    TT --> Store
```

The API authenticates and applies team policy before persisting a suspended Job.
Its single controller admits work fairly; Kubernetes/containerd place and run the
containers using NVIDIA device resources or TT DRA claims. The shared store has
one bounded filesystem per team, read-only input mounts and job-specific outputs.
SSH is for machine administration, not job execution. The current pilot has no
separate Redis scheduler, cloud load balancer or new monitoring product.

<details>
<summary>Historical architecture sketch — July 6, 2025</summary>

```mermaid
---
config:
  layout: dagre
---
flowchart LR
 subgraph Ingress_Layer["Ingress Layer"]
        APIGW["API Gateway"]
  end
 subgraph Control_Plane["Control Plane"]
        Auth["Auth & Authorization Service"]
        Queue["Job Queue"]
        Scheduler["Scheduler Service"]
        StateDB["State & Metadata"]
  end
 subgraph Compute_Plane["Compute Plane"]
        Balance["Load Balancer"]
        server1["Server #1"]
        server2["Server #2"]
  end
 subgraph Observability["Observability"]
        Metrics["Metrics"]
        Logging["Logging"]
        Alerts["Alerts"]
  end
    APIGW --> Auth
    Auth --> Queue
    Queue --> Scheduler
    Scheduler --> Balance
    Balance --> server1 & server2
    server1 --> APIGW & Observability
    server2 --> APIGW & Observability
```

Architecture as of 7/6/2025

</details>
