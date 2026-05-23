#!/bin/bash
set -e

# Check GBrain container is responding
if ! docker exec akb48-gbrain-1 gbrain status 2>/dev/null; then
    echo "ERROR: GBrain is not responding"
    exit 1
fi

# Check akb48 systemd service is active
if ! systemctl is-active --quiet akb48; then
    echo "ERROR: akb48 service is not active"
    exit 1
fi

echo "All systems operational"
exit 0
