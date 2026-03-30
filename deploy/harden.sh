#!/usr/bin/env bash
set -euo pipefail

# Harden a fresh VPS for running wally-tunnel-server.
# Run after setup.sh. Safe to re-run — each step is idempotent.
#
# What this does:
#   - SSH: key-only auth, no root password, no forwarding
#   - Firewall: allow only SSH (22), HTTP (80), HTTPS (443)
#   - fail2ban: auto-ban after 3 failed SSH attempts
#   - Kernel: SYN flood protection, disable ICMP redirects, restrict ptrace
#   - Cleanup: disable unnecessary services

echo "=== Hardening server ==="

# 1. SSH hardening
echo "[1/5] Hardening SSH..."
cat > /etc/ssh/sshd_config.d/99-hardened.conf << 'SSH'
PermitRootLogin prohibit-password
PasswordAuthentication no
PermitEmptyPasswords no
PubkeyAuthentication yes
MaxAuthTries 3
X11Forwarding no
AllowTcpForwarding no
AllowAgentForwarding no
ClientAliveInterval 300
ClientAliveCountMax 2
SSH

sshd -t 2>/dev/null || /usr/sbin/sshd -t && systemctl restart ssh
echo "    SSH: key-only auth, no forwarding, max 3 attempts"

# 2. Firewall
echo "[2/5] Configuring firewall..."
if ! command -v ufw &> /dev/null; then
    apt-get update -qq && apt-get install -y -qq ufw
fi
ufw --force reset > /dev/null 2>&1
ufw default deny incoming > /dev/null 2>&1
ufw default allow outgoing > /dev/null 2>&1
ufw allow 22/tcp comment "SSH" > /dev/null 2>&1
ufw allow 80/tcp comment "HTTP" > /dev/null 2>&1
ufw allow 443/tcp comment "HTTPS" > /dev/null 2>&1
ufw --force enable > /dev/null 2>&1
echo "    Firewall: only 22, 80, 443 open"

# 3. fail2ban
echo "[3/5] Installing fail2ban..."
if ! command -v fail2ban-client &> /dev/null; then
    apt-get update -qq && apt-get install -y -qq fail2ban > /dev/null 2>&1
fi
cat > /etc/fail2ban/jail.local << 'F2B'
[DEFAULT]
bantime = 1h
findtime = 10m
maxretry = 3

[sshd]
enabled = true
port = 22
mode = aggressive
F2B
systemctl enable fail2ban > /dev/null 2>&1
systemctl restart fail2ban
echo "    fail2ban: ban after 3 failures, 1h ban time"

# 4. Kernel hardening
echo "[4/5] Applying kernel hardening..."
cat > /etc/sysctl.d/99-hardened.conf << 'SYSCTL'
net.ipv4.ip_forward = 0
net.ipv6.conf.all.forwarding = 0
net.ipv4.tcp_syncookies = 1
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.default.send_redirects = 0
net.ipv4.conf.all.accept_source_route = 0
net.ipv6.conf.all.accept_source_route = 0
net.ipv4.conf.all.log_martians = 1
net.ipv4.icmp_echo_ignore_broadcasts = 1
kernel.unprivileged_bpf_disabled = 1
kernel.yama.ptrace_scope = 2
kernel.kptr_restrict = 2
SYSCTL
sysctl --system > /dev/null 2>&1
echo "    Kernel: SYN cookies, no redirects, restricted ptrace/BPF"

# 5. Disable unnecessary services
echo "[5/5] Disabling unnecessary services..."
systemctl disable --now atd.service 2>/dev/null || true
echo "    Disabled: atd"

echo ""
echo "=== Hardening complete ==="
echo ""
echo "Summary:"
echo "  - SSH: key-only, no forwarding, 3 max attempts"
echo "  - Firewall: deny all except 22/80/443"
echo "  - fail2ban: aggressive SSH jail (3 attempts, 1h ban)"
echo "  - Kernel: SYN flood protection, no ICMP redirects"
echo ""
echo "See SECURITY.md for additional recommendations."
