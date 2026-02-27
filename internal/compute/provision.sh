#!/bin/bash
# provision.sh — Crimata OS bootstrap
# Usage: provision.sh <slug> <password> <db_password>

set -euo pipefail

SLUG=$1
PASSWORD=$2
DB_PASSWORD=$3
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

# ── 7. Postgres setup ───────────────────────────────────────────────────────
log "Configuring Postgres..."
systemctl enable postgresql
systemctl start postgresql

su -c "psql -tc \"SELECT 1 FROM pg_roles WHERE rolname='crimata'\" | grep -q 1 || \
       psql -c \"CREATE USER crimata WITH PASSWORD '$DB_PASSWORD';\"" postgres

su -c "psql -tc \"SELECT 1 FROM pg_database WHERE datname='crimata_contacts'\" | grep -q 1 || \
       psql -c \"CREATE DATABASE crimata_contacts OWNER crimata;\"" postgres

DATABASE_URL="postgresql://crimata:$DB_PASSWORD@localhost/crimata_contacts" \
    node /usr/lib/crimata-contacts/dist/migrate.js

# ── 8. systemd units ────────────────────────────────────────────────────────
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

systemctl daemon-reload
systemctl enable crimata-auth crimata-dock crimata-contacts
systemctl start crimata-auth crimata-dock crimata-contacts

# ── 9. Nginx ────────────────────────────────────────────────────────────────
log "Configuring nginx..."

cat > /etc/nginx/sites-available/crimata << EOF
server {
    listen 80;
    server_name $SLUG.crimata.com;

    root /opt/crimata/ui;
    index index.html;

    location / {
        try_files \$uri \$uri/ /index.html;
    }

    location /auth {
        proxy_pass http://localhost:7700;
    }

    location /dock/ {
        proxy_pass http://localhost:7701/;
    }

    location /apps/contacts/ {
        proxy_pass http://localhost:3001/contacts/;
    }
}
EOF

ln -sf /etc/nginx/sites-available/crimata /etc/nginx/sites-enabled/crimata
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl enable nginx
systemctl restart nginx

# ── 10. Cleanup ─────────────────────────────────────────────────────────────
rm -rf /tmp/crimata-os

log "Done. $SLUG.crimata.com is live."
