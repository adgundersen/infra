#!/bin/bash
# deprovision.sh — exports customer data before EC2 is terminated
# Usage: deprovision.sh <slug> <s3_bucket>

set -euo pipefail

SLUG=$1
S3_BUCKET=$2
EXPORT_KEY="exports/$SLUG-$(date +%s).tar.gz"
CRIMATA_DIR=/opt/crimata

log() { echo "[crimata] $1"; }

# ── 1. Stop all services ───────────────────────────────────────────────────────
log "Stopping services..."
systemctl stop crimata-auth crimata-dock || true

for APP in contacts blog; do
  systemctl stop "crimata-$APP" || true
done

# ── 2. Dump Postgres ───────────────────────────────────────────────────────────
log "Dumping database..."
sudo -u postgres pg_dump crimata > /tmp/crimata.sql

# ── 3. Package data ────────────────────────────────────────────────────────────
log "Packaging data..."
tar -czf /tmp/export.tar.gz \
  /tmp/crimata.sql \
  $CRIMATA_DIR/data

# ── 4. Upload to S3 ────────────────────────────────────────────────────────────
log "Uploading to S3..."
aws s3 cp /tmp/export.tar.gz "s3://$S3_BUCKET/$EXPORT_KEY"

log "Export complete: s3://$S3_BUCKET/$EXPORT_KEY"
echo "$EXPORT_KEY"
