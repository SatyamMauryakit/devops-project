#!/bin/bash
# user_data.sh.tpl
# Rendered by Terraform (templatefile) and run once at first boot via
# EC2 user-data. Installs Docker, clones the public repo, writes the
# production .env files (from Terraform variables — never from git),
# and starts the stack.
#
# Logs: /var/log/cloud-init-output.log on the instance.

set -euxo pipefail

dnf update -y
dnf install -y docker git

systemctl enable docker
systemctl start docker
usermod -aG docker ec2-user

mkdir -p /usr/local/lib/docker/cli-plugins
curl -SL "https://github.com/docker/compose/releases/download/v2.29.7/docker-compose-linux-x86_64" \
  -o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

# 1GB swap file: t3.micro has only 1GB RAM, which can OOM during the
# frontend's npm/vite build step. Safe to keep even after the build.
if [ ! -f /swapfile ]; then
  dd if=/dev/zero of=/swapfile bs=1M count=1024
  chmod 600 /swapfile
  mkswap /swapfile
  swapon /swapfile
  echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi

APP_DIR=/opt/devops-sentinel
rm -rf "$APP_DIR"
git clone --depth 1 "${github_repo_url}" "$APP_DIR"
cd "$APP_DIR"

# Root .env, used by docker-compose.yml for Mongo credentials.
cat > .env <<EOF
MONGO_INITDB_ROOT_USERNAME=${mongo_root_username}
MONGO_INITDB_ROOT_PASSWORD=${mongo_root_password}
EOF

chown -R ec2-user:ec2-user "$APP_DIR"

# Build and start. Runs as root here (user-data context); ec2-user can
# manage it afterwards via `cd /opt/devops-sentinel && docker compose ...`
# since it was added to the docker group above.
docker compose up -d --build
