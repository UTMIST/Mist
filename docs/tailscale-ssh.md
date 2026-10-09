# Tailscale and SSH setup

Verified against both hosts on **October 9, 2026**. Tailscale **1.102.4** is
running on both. Both use Ubuntu OpenSSH on port **22**, with Tailscale's built-in
SSH handler **disabled**. The private portal uses the existing Tailscale network.

## Machine addresses

| Machine | Linux user | Tailscale IP | LAN IP | SSH command |
|---|---|---|---|---|
| NVIDIA/main | `utmist` | `100.73.139.66` | `10.0.0.175` | `ssh utmist@100.73.139.66` |
| QuietBox original | `utmist-tt` | `100.95.175.37` | `10.0.0.112` | `ssh utmist-tt@100.95.175.37` |

Main's observed MagicDNS name is `utmist.tail459b5e.ts.net`. Its OS hostname has
mixed-case letters; Kubernetes normalizes the node name to `utmist-z1opa08`.
Use the IP commands above when DNS or shared-device name resolution is uncertain.

## What each login grants

- **Tailscale access:** reach a shared/authorized machine over the private network.
- **Linux SSH login:** administer that machine with its local account/password or
  an authorized SSH key. This password is unrelated to Google/Tailscale login.
- **Mist login:** use the website/CLI with a Mist account and assigned team role.

Researchers can use http://100.73.139.66:8088 without SSH. Machine sharing does
not create a Mist account or automatically permit administrator SSH.

## Check the existing setup

Run locally on either machine:

```bash
tailscale version
tailscale status
tailscale ip -4
systemctl is-active tailscaled ssh
```

From an authorized administrator device, connect with the table's SSH command.
An SSH key connection to QuietBox was checked during this documentation audit.
Both hosts' OpenSSH configurations enable public-key and password authentication.
Passwords are intentionally absent from this repository.

## Set up a fresh Ubuntu host

Run these steps from its local console or an existing administrator session.
For an already connected host, verify its current tailnet before changing login.

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo systemctl enable --now tailscaled
sudo tailscale up
```

Open the displayed authentication link and join the intended existing tailnet.
Use its administrator console to manage machine sharing or network access.
Do not log out an existing infrastructure node merely to repeat installation.

Install Ubuntu's SSH server and choose regular SSH over Tailscale:

```bash
sudo apt update
sudo apt install openssh-server
sudo /usr/sbin/sshd -t
sudo systemctl enable --now ssh
sudo tailscale set --ssh=false
tailscale ip -4
```

Disabling the built-in SSH handler makes Ubuntu OpenSSH handle authentication.
Tailscale SSH has its own policy and can reject connections even when ordinary
network traffic is allowed. Changing handlers may interrupt a session that uses
Tailscale SSH; perform the switch from a local console when applicable.

## SSH keys and convenient aliases

On your administrator laptop, use an existing key or create one:

```bash
ssh-keygen -t ed25519
ssh-copy-id -i ~/.ssh/id_ed25519.pub utmist@100.73.139.66
ssh-copy-id -i ~/.ssh/id_ed25519.pub utmist-tt@100.95.175.37
```

`ssh-copy-id` requires an existing allowed login and adds only the public key.
Alternatively, a host administrator can install the `.pub` contents in that
Linux user's `~/.ssh/authorized_keys`. Keep private keys on their owner device.

Optional entries in your laptop's `~/.ssh/config`:

```sshconfig
Host mist-main
    HostName 100.73.139.66
    User utmist
    IdentityFile ~/.ssh/id_ed25519
    IdentitiesOnly yes

Host mist-quietbox
    HostName 100.95.175.37
    User utmist-tt
    IdentityFile ~/.ssh/id_ed25519
    IdentitiesOnly yes
```

Connect with `ssh mist-main` or `ssh mist-quietbox`. Adjust the IdentityFile when
using a different key. A successful network connection still needs valid Linux
credentials and access permitted by the host rules.

## Current host restrictions

The installed `mist-portal-firewall.service` permits ordinary Tailscale sources
to main's TCP **8088**, denying direct SSH/backend/cluster access. Infrastructure
IPs and the recorded administrator device **100.99.194.52** retain broader access.
The script contains matching IPv6 entries. Adding an administrator device needs
an explicit update to [the host allowlist](../deploy/private/portal-firewall.sh),
plus any applicable tailnet policy. LAN rules are separate.

A plain SSH public key does not bypass this firewall. Access from an external
researcher's device has not been verified by this host-side documentation audit.

The GCP jump-host, port-2025 and manual Discord-reservation instructions from the
old guide are superseded for this installation. Its plaintext password has been
removed from current documentation; Git history still contains that old value.
Retire that credential if it remains valid. No passwords were changed by this audit.

## Troubleshooting

| Symptom | Check |
|---|---|
| Timeout or host unreachable | Tailscale connectivity, machine online, host allowlist and tailnet access |
| `tailnet policy does not permit you to SSH` | Built-in Tailscale SSH handler/policy; current hosts use regular OpenSSH |
| `Permission denied` | Correct Linux user, authorized key or Linux password |
| Browser loads but cannot sign in | Mist account/session and trusted browser origin; SSH credentials are unrelated |
| SSH works but worker cannot join | k3s currently uses LAN `10.0.0.175:6443`; tailnet membership alone does not supply that route |

The current website is private HTTP over Tailscale. Public hosting, Serve/Funnel
and HTTPS setup were excluded from this release. Tailscale subscription limits
should be checked in the account console; machine count and user-seat count are
different and no fixed seat limit is asserted here.

References: [Tailscale Linux installation](https://tailscale.com/docs/install/linux),
[Tailscale SSH](https://tailscale.com/docs/features/tailscale-ssh),
[Ubuntu OpenSSH](https://documentation.ubuntu.com/server/how-to/security/openssh-server/).
