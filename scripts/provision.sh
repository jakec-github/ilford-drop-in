#!/usr/bin/env bash
# Provision a fresh Ubuntu droplet for the drop-in server (ADR 0002).
# Idempotent: safe to rerun. Run as root on the droplet:
#   scp scripts/provision.sh root@<ip>: && ssh root@<ip> ./provision.sh
set -euo pipefail

# Swap: the 512 MiB droplet needs it to survive image pulls without the OOM
# killer intervening.
if ! swapon --show --noheadings | grep -q '^/swapfile'; then
  echo "Creating 2G swap file"
  fallocate -l 2G /swapfile
  chmod 600 /swapfile
  mkswap /swapfile
  swapon /swapfile
fi
if ! grep -q '^/swapfile' /etc/fstab; then
  echo '/swapfile none swap sw 0 0' >>/etc/fstab
fi

# Docker (engine + compose plugin) from Docker's own repo.
if ! command -v docker >/dev/null; then
  echo "Installing Docker"
  curl -fsSL https://get.docker.com | sh
fi

# Firewall. Note: published container ports bypass ufw (Docker writes its own
# iptables rules), but the only published ports are Caddy's 80/443, which are
# open here anyway.
ufw allow OpenSSH
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable

# Deploy directory: compose files arrive via the deploy workflow, the three
# config files (drop_in_config.prod.yaml, oauthClientWeb.prod.json,
# serviceAccount.prod.json) are scp'd manually — see docs/deployment.md.
mkdir -p /opt/dropin

echo "Provisioning complete."
