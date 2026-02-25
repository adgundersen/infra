#!/bin/bash
# provision.sh — v0.1 bootstrap: installs nginx and serves a welcome page
# Usage: provision.sh <slug> <password> <db_password>

set -euo pipefail

SLUG=$1

log() { echo "[crimata] $1"; }

# ── 1. System dependencies ─────────────────────────────────────────────────────
log "Installing nginx..."
apt-get update -qq
apt-get install -y -qq nginx

# ── 2. Welcome page ────────────────────────────────────────────────────────────
log "Creating welcome page..."
mkdir -p /var/www/crimata

cat > /var/www/crimata/index.html << EOF
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Crimata — $SLUG</title>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #0a0a0a;
      color: #fff;
      height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      flex-direction: column;
      gap: 12px;
    }
    h1 { font-size: 2rem; font-weight: 600; }
    p { color: #888; font-size: 1rem; }
  </style>
</head>
<body>
  <h1>Welcome, $SLUG.</h1>
  <p>Your Crimata hub is ready.</p>
</body>
</html>
EOF

# ── 3. Nginx config ────────────────────────────────────────────────────────────
log "Configuring nginx..."
cat > /etc/nginx/sites-available/crimata << EOF
server {
    listen 80;
    server_name $SLUG.crimata.com;

    root /var/www/crimata;
    index index.html;

    location / {
        try_files \$uri \$uri/ /index.html;
    }
}
EOF

ln -sf /etc/nginx/sites-available/crimata /etc/nginx/sites-enabled/crimata
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl enable nginx
systemctl restart nginx

log "Provisioning complete. $SLUG.crimata.com is ready."
