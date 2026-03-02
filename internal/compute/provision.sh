#!/bin/bash
# provision.sh — Crimata OS bootstrap
# Usage: provision.sh <slug> <password> <db_password> <anthropic_api_key>

set -euo pipefail

SLUG=$1
PASSWORD=$2
DB_PASSWORD=$3
ANTHROPIC_API_KEY=${4:-}
OS_REPO="https://github.com/adgundersen/os"

log() { echo "[crimata] $1"; }

# ── 1. System dependencies ──────────────────────────────────────────────────
log "Installing system dependencies..."
apt-get update -qq
apt-get install -y -qq \
    nginx \
    postgresql postgresql-contrib \
    gcc make \
    libmicrohttpd-dev \
    libpam-dev \
    libsystemd-dev \
    nodejs npm \
    git

# ── 2. Linux user (PAM auth uses real OS users) ─────────────────────────────
log "Creating user $SLUG..."
useradd --create-home --shell /bin/bash "$SLUG" || true
echo "$SLUG:$PASSWORD" | chpasswd

# ── 3. Fetch OS source ──────────────────────────────────────────────────────
log "Fetching OS..."
git clone --depth 1 "$OS_REPO" /tmp/crimata-os

# ── 4. Build and install core binaries ─────────────────────────────────────
log "Building crimata-auth..."
mkdir -p /opt/crimata/bin
make -C /tmp/crimata-os/auth OUT=/opt/crimata/bin/crimata-auth

log "Building crimata-dock..."
make -C /tmp/crimata-os/dock OUT=/opt/crimata/bin/crimata-dock

# ── 5. Install web desktop UI ───────────────────────────────────────────────
log "Installing UI..."
mkdir -p /opt/crimata/ui
cp -r /tmp/crimata-os/ui/* /opt/crimata/ui/

# ── 6. Install apps ─────────────────────────────────────────────────────────
log "Installing crimata-contacts..."
mkdir -p /usr/lib/crimata-contacts
cp -r /tmp/crimata-os/apps/contacts/* /usr/lib/crimata-contacts/
cd /usr/lib/crimata-contacts
npm install --silent
npm run build

# ── 7. Install crimata-agent ────────────────────────────────────────────────
log "Installing crimata-agent..."
mkdir -p /usr/lib/crimata-agent
cp -r /tmp/crimata-os/agent/* /usr/lib/crimata-agent/
cd /usr/lib/crimata-agent
npm install --silent
npm run build

# ── 8. Postgres setup ───────────────────────────────────────────────────────
log "Configuring Postgres..."
systemctl enable postgresql
systemctl start postgresql

su -c "psql -tc \"SELECT 1 FROM pg_roles WHERE rolname='crimata'\" | grep -q 1 || \
       psql -c \"CREATE USER crimata WITH PASSWORD '$DB_PASSWORD';\"" postgres

su -c "psql -tc \"SELECT 1 FROM pg_database WHERE datname='crimata_contacts'\" | grep -q 1 || \
       psql -c \"CREATE DATABASE crimata_contacts OWNER crimata;\"" postgres

DATABASE_URL="postgresql://crimata:$DB_PASSWORD@localhost/crimata_contacts" \
    node /usr/lib/crimata-contacts/dist/migrate.js

# ── 9. systemd units ────────────────────────────────────────────────────────
log "Installing systemd units..."

cat > /etc/systemd/system/crimata-auth.service << EOF
[Unit]
Description=Crimata Auth
After=network.target

[Service]
ExecStart=/opt/crimata/bin/crimata-auth
Restart=always

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/crimata-dock.service << EOF
[Unit]
Description=Crimata Dock
After=network.target

[Service]
ExecStart=/opt/crimata/bin/crimata-dock
Restart=always

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/crimata-contacts.service << EOF
[Unit]
Description=Crimata Contacts
After=network.target postgresql.service

[Service]
ExecStart=node /usr/lib/crimata-contacts/dist/index.js
Restart=always
Environment=PORT=3001
Environment=DATABASE_URL=postgresql://crimata:$DB_PASSWORD@localhost/crimata_contacts

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/crimata-agent.service << EOF
[Unit]
Description=Crimata Agent
After=network.target crimata-dock.service

[Service]
ExecStart=node /usr/lib/crimata-agent/dist/index.js
Restart=always
Environment=ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable crimata-auth crimata-dock crimata-contacts crimata-agent
systemctl start  crimata-auth crimata-dock crimata-contacts crimata-agent

# ── 10. Nginx ───────────────────────────────────────────────────────────────
log "Configuring nginx..."

cat > /etc/nginx/sites-available/crimata << EOF
server {
    listen 80;
    server_name $SLUG.crimata.com;

    root /opt/crimata/ui;
    index index.html;

    # Static UI
    location / {
        try_files \$uri \$uri/ /index.html;
    }

    # Auth service (strips /auth/ prefix → sends bare path to port 7700)
    location /auth/ {
        proxy_pass http://localhost:7700/;
    }

    # Dock service
    location /dock/ {
        proxy_pass http://localhost:7701/;
    }

    # Agent WebSocket
    location /ws {
        proxy_pass http://localhost:7702/ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600;
    }

    # Agent events (for server-side services to emit events)
    location /events {
        proxy_pass http://localhost:7702/events;
    }

    # App: contacts
    location /apps/contacts/ {
        proxy_pass http://localhost:3001/contacts/;
    }

    # UI component files (served statically from /opt/crimata/ui/components/)
    location /components/ {
        alias /opt/crimata/ui/components/;
        add_header Cache-Control "no-cache";
    }
}
EOF

ln -sf /etc/nginx/sites-available/crimata /etc/nginx/sites-enabled/crimata
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl enable nginx
systemctl restart nginx

# ── 11. Cleanup ──────────────────────────────────────────────────────────────
rm -rf /tmp/crimata-os

log "Done. $SLUG.crimata.com is live."
