# Runtime environment

Defines what a job sees from the inside: software, filesystem, environment
variables, caches, outputs and signals. Every executor must provide exactly this
(see [executors.md](executors.md)). Overview:
[ARCHITECTURE.md](../../../ARCHITECTURE.md). Related decisions: D8, D11, D12, D13.

## Software

Jobs run on a **runtime profile** for each partition. Under the K3s executor that
is a container image. Under the node executor it is the host's installed
software. Researchers never choose or build images.

| Profile | Contents |
|---|---|
| `gpu` | Ubuntu 22.04 (matches the hosts); CUDA 12.x user-space libraries, cuDNN, NCCL (driver injected from the host); Python 3.10 + `uv` + Miniforge (`conda`/`mamba`); `git`, `git-lfs`, build tools (`gcc`, `make`, `cmake`); `tmux`, `vim`, `nano`, `htop`, `nvtop`; `curl`, `wget`, `rsync`, `unzip`, `zstd`, `jq`; `huggingface_hub` CLI, `awscli` |
| `cpu` | Same as `gpu` without the CUDA stack |
| `tt` (M4) | Same base + `tt-metal`/`ttnn` runtime, `tt-smi`, hugepages support |

- **Versions.** Profiles are versioned by date, for example `gpu:2026.11`. A
  partition pins one version as its default. When a new version ships, the
  previous one stays selectable (`--runtime gpu:2026.11`) for one term, because
  compiled packages in researchers' environments can break across upgrades.
  Upgrades are announced.
- **Shared extras** (large toolkits, datasets of general interest, modules) go in
  `/mist/software`, which admins manage and jobs mount read-only.
- **Research environments belong to researchers.** They create them in their
  folder (`uv venv`, `conda create -p ./env`), usually from an interactive job,
  so the environment matches the profile it runs on.
- **No root and no `apt`.** System packages are requested through the profile
  owner.

## Filesystem inside a job

| Path | What | Writable |
|---|---|---|
| `/mist/teams/<team>/` | Charged team's folder: your folder, teammates' (read), `shared/`, `.cache/` | per permissions |
| `/mist/software/` | Shared tools | no |
| `/scratch` (= `$TMPDIR`), `/tmp` | Node-local, deleted at job end | yes |
| `/dev/shm` | Shared memory, half the memory request | yes |
| everything else | Runtime profile | no |

Other teams' folders don't exist inside a job.

## Environment variables

| Variable | Value |
|---|---|
| `HOME` | `/mist/teams/<team>/<user>` |
| `USER`, `LOGNAME` | username |
| `SHELL` | `/bin/bash` |
| `PWD` (start dir) | job `workdir` (the submit folder) |
| `TMPDIR` | `/scratch` |
| `MIST_JOB_ID`, `MIST_JOB_NAME`, `MIST_USER`, `MIST_TEAM`, `MIST_PARTITION`, `MIST_QOS`, `MIST_NODE` | job facts |
| `MIST_CPUS`, `MIST_MEMORY_BYTES`, `MIST_GPUS`, `MIST_TT_BOARDS` | allocation |
| `MIST_TIME_LIMIT_SECONDS`, `MIST_DEADLINE` (Unix time) | time limit |
| `MIST_SUBMIT_DIR`, `MIST_SCRATCH` | paths |
| `OMP_NUM_THREADS`, `MKL_NUM_THREADS` | `MIST_CPUS` |
| `CUDA_VISIBLE_DEVICES` | allocated GPUs, numbered from 0 |
| `PYTHONUNBUFFERED` | `1`, so output appears live in the `.out` file |
| `HF_HOME` | `/mist/teams/<team>/.cache/huggingface` (team-shared) |
| `HF_TOKEN_PATH` | `$HOME/.cache/huggingface/token` (**private**) |
| `TORCH_HOME` | `/mist/teams/<team>/.cache/torch` (team-shared) |
| user `--env` values | as given; they override the variables above except `MIST_*`, `HOME` and `USER` |

The login session's environment is **not** copied into jobs. Jobs get only the
variables above plus `--env` values, so they behave the same however they were
submitted.

**How caches land.** Tools without an explicit mapping follow `$HOME` and XDG
defaults, so their caches land privately in `$HOME/.cache`, and config in
`$HOME/.config`. Only the three large, shareable caches above are mapped to the
team.

## Start-up behaviour

`mist-job-entry` does this at the start of every job:

1. sets `umask 0007` (team-readable and team-writable files, nothing for others)
2. ensures `$HOME/.cache`, `.config`, `.local` and `.ssh` exist with mode `0700`
3. opens the output file and writes a header
4. starts the script (through `bash`, from `workdir`), the command, or the
   interactive holder

## Output and results

| What | Where |
|---|---|
| stdout + stderr | `mist-<id>.out` in the submit folder (or `--output`), written live, appended by Mist with start and end lines |
| Mist messages | same file: node, start, cancellation, time limit, out-of-memory with the limit, exit code, elapsed time, peak memory |
| checkpoints, results | wherever the code writes; relative paths resolve inside the team folder |
| scratch contents | deleted at job end, with no exceptions |

Example tail of an output file:

```text
[mist] job 412 ended 2026-11-02T19:04:11Z on utmist-z1opa08
[mist] state OUT_OF_MEMORY: exceeded memory limit 32 GiB (peak 32.0 GiB)
[mist] exit code 137, elapsed 3h12m, request more with --mem
```

## Signals and time limits

- **At the time limit, or on `mist cancel`,** the job's processes receive
  **SIGTERM**, then **SIGKILL** after the grace period (default 60 s, set with
  `--signal-grace`).
- **The deadline is known in advance.** `MIST_DEADLINE` lets code plan its final
  checkpoint without waiting for the signal.

## Network

Outbound internet works (pip, Hugging Face, wandb, git). Cluster-internal
addresses, the LAN and Tailscale peers are unreachable. Nothing can connect *in*
to a job until port forwarding arrives (M5).

## What researchers see

This section is the source for user-facing documentation.

- **Your job starts** in the folder you submitted from, with `HOME` set to your
  team folder. Activate your environment in the script
  (`source .venv/bin/activate`).
- **Output** appears live in `mist-<id>.out`. Watch it with `tail -f` or
  `mist logs -f`.
- **Shared caches.** Hugging Face and PyTorch downloads are shared with your
  team, so each model downloads once per team. Your HF token stays private.
- **Other caches and settings** (pip, uv, wandb and the rest) stay private in
  your folder's hidden directories automatically.
- **`$TMPDIR` / `/scratch`** is fast local disk that is deleted when the job ends,
  even after a crash or cancel. Save checkpoints in your folder.
- **Running out of memory or time** is stated plainly at the end of the output
  file.
- **To checkpoint before the time limit,** handle SIGTERM: you get 60 s by
  default (`--signal-grace` changes it).
- **Downloads.** Run them with `mist run -p cpu -- <command>`, or interactively
  with `mist shell -p cpu`.
- **No `sudo` or `apt`.** Ask for system packages to be added to the runtime.

## Work by milestone

- **M1:** the `gpu` and `cpu` images, image versioning and pre-pulling on nodes,
  `/mist/software` skeleton, environment variables, and this contract tested by
  the executor conformance suite.
- **M4:** the `tt` image.
- **M5:** host profiles matching the images on nodes run by the node executor.

## Open questions

- Owner.
- Whether to offer environment modules (Lmod) in `/mist/software`.
- Image registry: a local registry on the control node, or importing images
  straight into each node's containerd.
