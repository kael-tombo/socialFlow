# Hosting SocialFlow.ai on Oracle Cloud (Always Free)

This guide walks through hosting a production SocialFlow.ai instance on
Oracle Cloud Infrastructure (OCI) using the **Always Free** tier — at zero
ongoing cost. SocialFlow.ai is a single static Go binary with the web app
embedded, backed by SQLite and local media storage, so the only real
requirements are a small Linux VM, Docker, and a persistent disk. Oracle's
Always Free ARM instance exceeds those requirements by a wide margin and,
unlike most free PaaS offerings (Render, Koyeb, Railway), keeps its disk
across reboots and redeployments — which is exactly what the SQLite database
and media library at `/data` need.

The end state of this guide is the same stack produced by the standard
deployment path: the `deploy/socialflow/` bundle running behind the bundled
Caddy proxy with automatic Let's Encrypt HTTPS. If you already know the
standard path from `DEPLOYMENT.md`, the only Oracle-specific parts are
steps 1–4 (account, instance, and firewall); step 5 onward is the normal
deploy flow.

---

## 1. What you need before starting

| Requirement | Notes |
|---|---|
| Credit or debit card | Used by Oracle for identity verification only. The Always Free resources are not charged. |
| A domain name | Any registrar works. A free subdomain from [DuckDNS](https://www.duckdns.org) also works if you do not own a domain. |
| SSH key pair | Generate with `ssh-keygen -t ed25519` if you do not have one. |
| 30–45 minutes | Mostly waiting for provisioning and certificate issuance. |

**Important — choose your home region carefully.** The home region selected
at signup is permanent for Always Free resources and cannot be changed later.
Arm capacity for free-tier accounts varies significantly between regions.
Commonly recommended choices with better A1 availability include
`us-phoenix-1`, `us-ashburn-1`, and `eu-frankfurt-1`, but any region can
work — the troubleshooting section below covers the well-known
"out of host capacity" problem and its fix.

---

## 2. Understanding the Always Free tier

Oracle's Always Free tier is not a trial. The 30-day trial with $300 in
credits expires, but the resources listed below remain free forever (as long
as you stay within their limits). Two facts matter for this deployment:

1. **If you upgrade to Pay-As-You-Go later, the Always Free limits stay
   free.** Upgrading to PAYG is the documented fix for A1 capacity problems
   and does not by itself incur charges — you only pay for resources beyond
   the Always Free allowance.
2. **Idle/unused free-tier accounts can be reclaimed.** OCI may stop idle
   Always Free instances after sustained inactivity (below roughly 20% CPU,
   network, and memory utilization over a week for A1 shapes). A regularly
   used SocialFlow.ai instance with its background jobs and health checks
   normally keeps the VM active, but an abandoned demo instance may be
   stopped.

What the Always Free allowance covers, and what this deployment uses:

| Resource | Always Free limit | Used by this guide |
|---|---|---|
| Ampere A1 Flex (ARM) | 4 OCPUs and 24 GB RAM total, splittable across up to 4 VMs | 1 VM with 4 OCPU / 24 GB |
| Block storage (boot volumes) | 200 GB total | 100 GB boot volume |
| AMD micro VMs | 2 × VM.Standard.E2.1.Micro (1/8 OCPU, 1 GB RAM) | Not used (too small for local builds) |
| Outbound data transfer | 10 TB/month | Far below the limit |
| Public IPv4 | One per instance, free | Included with the VM |

Stick to exactly one A1 VM at 4 OCPU / 24 GB and a single boot volume of
100 GB or less, and nothing in this guide will ever generate a bill. To be
safe, open **Billing & Cost Management → Budgets** and create an alert
budget (for example, $1) so you are emailed if anything ever leaves the
free allowance.

---

## 3. Creating the account and the VM

### 3.1 Sign up

1. Go to <https://www.oracle.com/cloud/free/> and click **Start for free**.
2. Fill in your details, verify your email, and complete card verification.
   The card may show a small temporary authorization hold that is reversed.
3. Choose your **home region** (permanent — see section 1).

### 3.2 Create the instance

1. In the OCI console, open the navigation menu → **Compute → Instances →
   Create instance**.
2. **Name:** `socialflow`.
3. **Image:** Ubuntu 24.04 (click *Change image* → **Ubuntu** — pick the
   plain "Canonical Ubuntu" image, aarch64 is selected automatically with
   the Ampere shape).
4. **Shape:** click *Change shape* → **Ampere** → **VM.Standard.A1.Flex**,
   then set **4 OCPU** and **24 GB** memory.
5. **Networking:** leave the defaults (new VCN, public IPv4 assigned).
6. **SSH keys:** paste your **public** key (`~/.ssh/id_ed25519.pub`), or
   upload the `.pub` file.
7. **Boot volume:** under *Block volume storage*, set the boot volume to
   **100 GB** (the default is ~47 GB; more room is useful for the media
   library, and 100 GB still fits comfortably in the 200 GB free allowance).
8. Click **Create**. Provisioning usually takes one to three minutes.

### 3.3 Open the firewall in the OCI security list

The VCN's default security list only allows inbound SSH. Add rules for
HTTP and HTTPS so Caddy can obtain certificates:

1. Open your instance → click the **Subnet** link → **Default Security
   List** (or your security group) → **Add Ingress Rules**.
2. Add two **stateless** rules — one per port — each with:
   - Source CIDR: `0.0.0.0/0`
   - IP Protocol: `TCP`
   - Destination Port Range: `80` (repeat with `443`)
3. Keep the existing port-22 rule. Optionally tighten it later to your own
   IP as Source CIDR.

---

## 4. Prepare the server

SSH in and install Docker:

```bash
ssh ubuntu@<PUBLIC_IP>

# Install Docker Engine + buildx + compose plugin
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker ubuntu
```

Log out and back in so the `docker` group applies, then verify:

```bash
docker version && docker buildx version && docker compose version
```

### 4.1 Open the OS-level firewall (Ubuntu on OCI specific)

Ubuntu images on OCI ship with iptables rules that reject everything except
SSH — these are separate from the security list in step 3.3 and survive
reboots via `netfilter-persistent`. Open ports 80 and 443:

```bash
sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 80 -j ACCEPT
sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 443 -j ACCEPT
sudo netfilter-persistent save
```

> The `-I INPUT 6` line position matches the stock Ubuntu-on-OCI ruleset,
> which rejects all traffic after the SSH accept rule. If you have modified
> the rules, insert the accepts **before** the `REJECT`/`DROP` lines
> (`sudo iptables -L INPUT --line-numbers` to inspect).

### 4.2 Point DNS at the server

Create an `A` record for your domain (for example `socialflow.example.com`)
pointing at the instance's public IP, or configure your DuckDNS subdomain
with the same IP. Verify propagation before continuing — Caddy will fail
certificate issuance if the record is not live:

```bash
dig +short socialflow.example.com   # must return the instance's public IP
```

---

## 5. Deploy SocialFlow.ai

Now the standard deployment flow from `DEPLOYMENT.md` takes over. Everything
below runs on the VM; the build happens natively on ARM, so no QEMU or
multi-arch tooling is needed.

```bash
sudo apt-get update && sudo apt-get install -y git
git clone https://github.com/kael-tombo/socialFlow.git
cd socialFlow

# The deploy script builds the frontend and needs Bun; install it if absent.
# (Skip if `bun --version` already succeeds.)
curl -fsSL https://bun.sh/install | bash && source ~/.bashrc

# Configure the environment
cp deploy/socialflow/.env.prod.example deploy/socialflow/.env
```

Edit `deploy/socialflow/.env`:

```bash
# Generate both secrets first — do not reuse the placeholders:
openssl rand -base64 32   # -> OPENPOST_JWT_SECRET
openssl rand -base64 32   # -> OPENPOST_ENCRYPTION_KEY
```

| Variable | Set it to |
|---|---|
| `OPENPOST_APP_URL` | `https://socialflow.example.com` (your real domain, `https://`) |
| `OPENPOST_PUBLIC_URL` | Same value; or leave unset to inherit `OPENPOST_APP_URL` |
| `OPENPOST_JWT_SECRET` | Output of the first `openssl rand` |
| `OPENPOST_ENCRYPTION_KEY` | Output of the second `openssl rand` |
| `SOCIALFLOW_DOMAIN` | `socialflow.example.com` (for the Caddy proxy) |
| `SOCIALFLOW_ACME_EMAIL` | Your email, for Let's Encrypt expiry notices |
| `SOCIALFLOW_BIND` | Keep `127.0.0.1` — only Caddy is exposed |
| `OPENPOST_DISABLE_REGISTRATIONS` | `false` for now (first-run registration) |

> While testing DNS and firewall changes, uncomment the staging
> `acme_ca` line in `deploy/socialflow/Caddyfile` to avoid Let's Encrypt
> rate limits; comment it back out for production issuance.

Build and start:

```bash
deploy/socialflow/deploy.sh
```

The script installs workspace dependencies, builds the frontend bundle,
embeds it into the Go server, builds the Docker image, starts the stack, and
blocks until `/api/v1/ready` passes. When it prints the success line, bring
up the HTTPS proxy:

```bash
docker compose -f deploy/socialflow/docker-compose.prod.yml --profile proxy up -d
```

Watch Caddy obtain the certificate:

```bash
docker logs -f socialflow-caddy   # look for "certificate obtained successfully"
```

Open `https://socialflow.example.com`, register the first account (this
account is the administrator), then freeze registration:

```bash
echo 'OPENPOST_DISABLE_REGISTRATIONS=true' >> deploy/socialflow/.env
docker compose -f deploy/socialflow/docker-compose.prod.yml up -d
```

---

## 6. Verification

```bash
curl -fsS http://localhost:8080/api/v1/health   # process liveness
curl -fsS http://localhost:8080/api/v1/ready    # traffic gate — must pass
curl -fsSI https://socialflow.example.com       # HTTPS + HSTS via Caddy
```

From outside the server, confirm that port 8080 is **not** reachable
(`SOCIALFLOW_BIND` keeps it on loopback; only 80/443 are public), and that
the site loads over HTTPS in a browser you have not used for login yet.

---

## 7. Backups, updates, and rollback

Backups and updates follow the standard path documented in `DEPLOYMENT.md`
and `docs/reference/operations/backups.md`; the short version:

**Back up** the named volume (SQLite database plus media) on a cron schedule:

```bash
mkdir -p ~/backups
docker run --rm -v socialflow_socialflow_data:/data -v "$HOME/backups:/backup" \
  alpine tar czf "/backup/socialflow-$(date +%F).tar.gz" -C /data db media
```

Copy snapshots off the VM periodically (for example, to your laptop with
`scp`, or to any object storage) — a backup on the same disk is not a real
backup. As an extra layer, enable a scheduled **boot volume backup policy**
in the OCI console; note that it consumes part of the 200 GB free block
storage allowance, so size your schedule accordingly.

**Update** to the latest revision (the database migrates automatically on
boot):

```bash
cd ~/socialFlow
git pull --ff-only
deploy/socialflow/deploy.sh
docker compose -f deploy/socialflow/docker-compose.prod.yml --profile proxy up -d
```

**Roll back** to a previous image by pinning the tag and skipping the build:

```bash
SOCIALFLOW_IMAGE=socialflow:<previous-tag> deploy/socialflow/deploy.sh --skip-build
```

---

## 8. Hardening checklist (recommended)

The stack is already in good shape by default: the app port stays on
loopback, Caddy terminates TLS with HSTS, and the container data is rootless
from the host's perspective. A few extra steps are worth doing on any
internet-facing VM:

- **Restrict SSH to your IP** in the security list (step 3.3), or add
  `fail2ban` (`sudo apt install fail2ban`) if your IP changes often.
- **Automatic security updates:**
  ```bash
  sudo apt-get install -y unattended-upgrades
  sudo dpkg-reconfigure -plow unattended-upgrades
  ```
- **Keep the app private:** never set `SOCIALFLOW_BIND=0.0.0.0` on this
  setup; Caddy is the only public surface.
- **Freeze registrations** as soon as the first admin account exists
  (step 5), unless you intentionally run an open instance.
- **Review the key-rotation path before you ever need it:** rotating
  `OPENPOST_ENCRYPTION_KEY` requires the keyring procedure in
  `docs/reference/configuration/environment-variables.md`.

---

## 9. Troubleshooting

**"Out of host capacity" when creating the A1 instance.** The well-known
free-tier capacity problem: the shape you asked for has no free capacity in
that availability domain for your account type. Fixes, in order of
effectiveness: (1) upgrade the account to Pay-As-You-Go — Always Free limits
remain free, and capacity opens up substantially; (2) retry at off-peak
hours (capacity frees up continuously); (3) try the other availability
domains of your region; (4) create the VM as 2 OCPU / 12 GB (still ample for
SocialFlow.ai) and scale up later.

**Caddy does not obtain a certificate.** Check, in order: DNS resolves to
the instance's public IP (`dig +short`); security list allows 80 and 443
(Caddy needs port 80 for the HTTP-01 challenge); the OS-level iptables rules
were saved with `netfilter-persistent save`. While iterating, switch Caddy
to the staging ACME endpoint to avoid hitting Let's Encrypt rate limits.

**`/api/v1/ready` fails after deploy.** Inspect the app logs:
`docker compose -f deploy/socialflow/docker-compose.prod.yml logs app`. The
most common causes are a missing or short `OPENPOST_JWT_SECRET` /
`OPENPOST_ENCRYPTION_KEY` (both must be 32+ characters) or an
`OPENPOST_APP_URL` that does not match the browser-facing origin.

**Ports blocked again after reboot.** The iptables changes in step 4.1 were
not persisted — re-run `sudo netfilter-persistent save`. If you used `ufw`
instead, enable it only after adding allow rules for 22, 80, and 443.

**Disk filling up.** Check usage with `docker system df` and
`df -h /`. Old images accumulate after updates; prune safely with
`docker image prune -f`. The media library lives in the named volume, not
the container layer, so pruning images never touches user data.
