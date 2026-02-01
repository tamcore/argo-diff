#!/bin/sh
set -e

# Create argo-diff user if it doesn't exist
if ! getent passwd argo-diff > /dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin argo-diff
fi

# Create config directory
mkdir -p /etc/argo-diff

# Set permissions
chown -R argo-diff:argo-diff /etc/argo-diff

# Reload systemd
systemctl daemon-reload

echo "argo-diff installed successfully."
echo "Configure /etc/argo-diff/config and then run: systemctl enable --now argo-diff"
