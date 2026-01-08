# Quick guide — how to SSH into the Tenstorrent “quiet box” (shared access)

Below is a practical, copy-pasteable guide you can share with your team so people can connect to the Tenstorrent quiet box through the GCP SSH jump proxy. It covers key generation, how to share your public key, the SSH `config` entries (exactly what you asked to include), connection steps, the temporary manual reservation process via Discord, rules about containers, troubleshooting, and good safety practices.

---

# 1) What we’re doing (short)

We expose the quiet box only behind an SSH jump host on GCP. Users get access by giving us their SSH **public key**; access is time-limited until a compute reservation system exists. People must work inside their own container while using the quiet box. Reservations are handled manually through a Discord channel for now — see section 4.

---

# 2) How to create and send your SSH public key

1. Generate an SSH key pair locally (if you don’t already have one). Recommended: ed25519 (modern, compact, secure).

```bash
# create a new key (press enter to accept default file and optionally add a passphrase)
ssh-keygen -t ed25519 -C "your.email@domain.com"
```

2. Print your public key and copy it:

```bash
cat ~/.ssh/id_ed25519.pub
```

3. Send **only** the public key string (the `.pub` contents) to the admins which are Ambrose Ling and Kenny Cui (e.g., paste into the Discord message or via whatever secure channel the team uses). Example public key looks like:

```
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user@host
```

(Do **not** send your private key — file `~/.ssh/id_ed25519`.)

If you already use `~/.ssh/id_rsa` / other keys, you can send whichever public key you prefer — ed25519 is preferred.

---

# 3) Add this to your `~/.ssh/config`

Put the following in your SSH config file (`~/.ssh/config`) **exactly** as shown (or append if entries already exist):

```
Host mist-jump
   HostName 34.170.88.115
   User utorontomist

Host mist
    Hostname localhost
    User utmist-tt
    Port 2025
    ProxyJump mist-jump
```

Notes:

* After the admins add your public key to the jump host / target server, you’ll connect with:

```bash
ssh mist
```

Login in with server password: `welovett2025!`

* The `ProxyJump` causes your SSH client to first connect to `mist-jump` (the GCP host), then forward into the quiet box on port `2025` as user `utmist-tt`.

---

# 4) Manual reservation (Discord workflow)

Because there is no automated reservation system yet, we use a Discord channel to manage access. Suggested workflow to keep things fair and safe:

1. **Channel name**: `#quietbox-reservations` (or similar).
2. **How to claim time**: Post a short message like:

```
Claim: @your-name — Project: <short name> — From: 2025-10-29 14:00 EDT — To: 2025-10-29 16:00 EDT
```

3. **Required fields**: who, project, start & end times (use local timezone, include timezone), and container image or name you’ll use. Until there is clear demand of machine usage times we will stick to this. If we see that multiple projects need to use the same machine 
4. **Admins confirm**: An admin reacts/acknowledges the claim (e.g., ✅). Until a claim is acknowledged, you should not start work.
5. **During your slot**: If you finish early, post an “ended” message so others can update the slot. If you need to extend, post in the channel and wait for confirmation.
6. **Enforcement**: If someone is using the box outside their claimed time, ping the channel owner/admin to resolve.

Suggested message template to claim:

```
Claiming quietbox:
- User: @ambrose
- Project: example-ml
- Start: 2025-10-29 14:00 EDT
- End:   2025-10-29 16:00 EDT
- Container Name: 
```

---

# 5) Rules while using the quiet box (must-follow)

* **Work only inside your container.** Do not run experiments on the host OS. Use Docker / Podman / the approved container runtime.
* **No background or persistent processes** outside your container. Kill any long-running jobs when your slot ends.
* **Follow agreed resource limits.** If admins set GPU/CPU limits, stay within them.
* **Clean up**: remove temporary large files from shared storage after your run, or move them to a location designated for your project.
* **Be respectful**: If another user needs time urgently, coordinate in the Discord channel.
* **Security**: never share private keys or credentials in Discord or public chat.

---

# 6) Example container usage patterns

Generic Docker example (adjust to your image and options):

```bash
# pull image
docker pull ghcr.io/tenstorrent/tt-metal/tt-metalium/ubuntu-22.04-dev-amd64:latest

# run container with GPU access and mount a workspace
docker run -it \
  --name your_name:project_name\
  -v /dev/hugepages-1G:/dev/hugepages-1G \
  --device /dev/tenstorrent \
  -v $(pwd):/home/utmist-tt/UTMIST \
  -v /mnt:/mnt \
  ghcr.io/tenstorrent/tt-metal/tt-metalium/ubuntu-22.04-dev-amd64:latest \
  /bin/bash
```

Inside the container you run your jobs. `--rm` ensures the container is removed when you exit.

If your setup uses a different runtime (Podman, singularity), follow the project's standard container instructions.

---

# 7) Connection checklist & verification

Before connecting:

* You’ve sent your public key to admins and they confirmed it was installed.
* Your `~/.ssh/config` contains the `mist-jump` and `mist` entries shown above.

To connect:

```bash
ssh mist
```

If you want verbose logs (helpful for troubleshooting):

```bash
ssh -vvv mist
```

Quick verification steps:

* `ssh -G mist` prints the resolved config for debugging.
* Confirm you land inside the correct user (prompt or `whoami`).
* Once connected, check you’re inside a container (e.g., `cat /proc/1/cgroup` or ask admins about the container launch process).

---

# 8) Common troubleshooting

* **Permission denied (publickey)**

  * Ensure the admin actually installed your `~/.pub` key on the target account.
  * Confirm you’re using the right private key (`ssh -i ~/.ssh/id_ed25519 mist` to force it).
  * Check file permissions: `~/.ssh` should be `700`, private key `600`.
* **ProxyJump failing / connection to jump host refused**

  * Confirm `mist-jump` HostName IP is reachable: `ssh -vvv mist-jump`.
  * Ensure your local firewall or corporate network allows outbound SSH (port 22).
* **Port 2025 connection refused**

  * That implies the service on the jump host isn’t forwarding correctly yet; ask admins to confirm the tunnel on GCP and the quiet box service are up.
* **Agent forwarding issues**

  * If you rely on agent forwarding, make sure you started `ssh-agent` and added keys with `ssh-add`.
* **If you get unexpected host key warnings**

  * Verify fingerprints with an admin before accepting. Do **not** blindly accept host key changes.

Use `ssh -vvv mist` and share the logs with admins if you need help.

---

# 9) Session end & cleanup checklist

Before you finish:

* Stop any running jobs inside your container (`Ctrl+C` or `pkill` inside container).
* `exit` the container and ensure the container is stopped/removed if required.
* Log out of the SSH session (`exit` or `logout`).
* Post in Discord that your slot ended, or update the reservation message.
