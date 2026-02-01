#!/bin/sh
set -e

# Stop and disable service if running
if systemctl is-active --quiet argo-diff; then
    systemctl stop argo-diff
fi

if systemctl is-enabled --quiet argo-diff; then
    systemctl disable argo-diff
fi

echo "argo-diff service stopped and disabled."
