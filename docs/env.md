# Environment and Credential Setup Guide

This document describes every credential, secret, and environment variable
used by DevOps Sentinel, where to get each one, where to store it, and how to
rotate it.

Never commit `.env` files, `kubeconfig`, Terraform state, or private keys to
git. The `.gitignore` file already excludes these patterns.

---

## Table of Contents

1. [Credential Overview](#1-credential-overview)
2. [Local Development (.env Files)](#2-local-development-env-files)
3. [Kubernetes Secrets](#3-kubernetes-secrets)
4. [GitHub Repository Secrets](#4-github-repository-secrets)
5. [AWS Credentials](#5-aws-credentials)
6. [Slack Setup](#6-slack-setup)
7. [WhatsApp / Meta Cloud API Setup](#7-whatsapp--meta-cloud-api-setup)
8. [LLM Provider Setup](#8-llm-provider-setup)
9. [Temporal Server Credentials](#9-temporal-server-credentials)
10. [Kill Switch](#10-kill-switch)
11. [Verification Checklist](#11-verification-checklist)
12. [Rotation Procedures](#12-rotation-procedures)

---

## 1. Credential Overview

All credentials fall into one of five storage locations. A single credential
may be needed in more than one location (for example, the Slack webhook URL is
used in local .env files, Kubernetes Secrets, and GitHub Secrets).

| Storage location | What goes there | Who reads it |
|---|---|---|
| `.env` file (local) | Dev-only secrets for docker-compose and Go processes | docker-compose, local Go binaries |
| Kubernetes Secret | Cluster-runtime secrets injected as env vars | Pods in the sentinel namespace |
| GitHub repository secret | CI pipeline secrets | GitHub Actions workflows |
| AWS IAM / SSO | Cloud infrastructure access | Terraform, aws CLI, kubectl (for EKS) |
| Meta Developer Console | WhatsApp Cloud API credentials | WhatsApp API calls |

### Credential inventory

| Name | Type | Where stored | Section |
|---|---|---|---|
| MONGO_INITDB_ROOT_USERNAME | Non-secret default | root .env, K8s Secret | 2, 3 |
| MONGO_INITDB_ROOT_PASSWORD | Secret | root .env, K8s Secret | 2, 3 |
| MONGO_URI (assembled) | Secret (built from parts) | backend .env, K8s Deployment env | 2, 3 |
| SLACK_WEBHOOK_URL | Secret | notifier .env, temporal .env, agent .env, K8s Secret, GitHub Secret | 2, 3, 4, 6 |
| SLACK_SIGNING_SECRET | Secret | agent .env, K8s Secret | 2, 3, 6 |
| WHATSAPP_ACCESS_TOKEN | Secret (expires 24h on test tier) | notifier .env, temporal .env, K8s Secret | 2, 3, 7 |
| WHATSAPP_PHONE_NUMBER_ID | Non-secret ID | notifier .env, temporal .env, K8s Secret | 2, 3, 7 |
| WHATSAPP_RECIPIENT_NUMBER | Non-secret phone number | notifier .env, temporal .env, K8s Secret | 2, 3, 7 |
| WHATSAPP_APP_SECRET | Secret | agent .env, K8s Secret | 2, 3, 7 |
| WHATSAPP_VERIFY_TOKEN | Non-secret (you choose it) | agent .env, K8s Secret, Meta Console | 2, 3, 7 |
| WHATSAPP_TEMPLATE_NAME | Non-secret config | notifier .env, temporal .env, K8s Secret | 2, 3, 7 |
| WHATSAPP_TEMPLATE_LANG | Non-secret config | notifier .env, temporal .env, K8s Secret | 2, 3, 7 |
| GROQ_API_KEY | Secret | agent .env, K8s Secret | 2, 3, 8 |
| POSTGRES_USER | Non-secret default | temporal .env | 2, 9 |
| POSTGRES_PASSWORD | Secret | temporal .env | 2, 9 |
| POSTGRES_DB | Non-secret default | temporal .env | 2, 9 |

---

## 2. Local Development (.env Files)

### How .env files work

Each service that runs locally (via docker-compose or `go run`) reads its
configuration from environment variables. The `.env.example` file in each
directory lists every variable that service needs with placeholder values.

To set up local development, copy each `.env.example` to `.env` in the same
directory and replace the placeholder values with real ones.

The `.gitignore` file excludes all `.env` files from git. Verify this after
initializing the repo:

```bash
git init
git check-ignore -v .env backend/.env notifier/.env temporal/.env agent/.env
```

Each line should print the `.gitignore` rule that matches.

### Step-by-step: create all .env files

#### 2.1 Root .env (docker-compose)

Used by: `docker-compose.yml` for variable substitution.

```bash
cp .env.example .env
```

Edit `.env` and set:

| Variable | What to set | Example |
|---|---|---|
| MONGO_INITDB_ROOT_USERNAME | Any username for local MongoDB | `admin` |
| MONGO_INITDB_ROOT_PASSWORD | Any password for local MongoDB | a random 16+ character string |

Generate a random password:

```bash
openssl rand -base64 24
```

#### 2.2 Backend .env

Used by: the Express API when running with `npm run dev` outside Docker.

```bash
cp backend/.env.example backend/.env
```

Edit `backend/.env`:

| Variable | What to set |
|---|---|
| MONGO_URI | `mongodb://admin:<password>@localhost:27017/sentinel?authSource=admin` — replace `<password>` with the value from the root .env |
| PORT | `3000` (default) |
| NODE_ENV | `development` |

#### 2.3 Notifier .env

Used by: the Go notifier service when running outside Docker.

```bash
cp notifier/.env.example notifier/.env
```

Edit `notifier/.env`:

| Variable | What to set | Source |
|---|---|---|
| SLACK_WEBHOOK_URL | Your Slack incoming webhook URL | Section 6 |
| WHATSAPP_ACCESS_TOKEN | Meta Cloud API access token | Section 7 |
| WHATSAPP_PHONE_NUMBER_ID | FROM phone number ID | Section 7 |
| WHATSAPP_RECIPIENT_NUMBER | Your phone in E.164 format | Section 7 |
| WHATSAPP_TEMPLATE_NAME | `sentinel_alert` (or your template name) | Section 7 |
| WHATSAPP_TEMPLATE_LANG | `en_US` | Section 7 |
| PORT | `8080` (default) | |
| LOG_LEVEL | `info` (default) | |

#### 2.4 Temporal .env

Used by: the Temporal worker when running with `go run .` outside Docker.

```bash
cp temporal/.env.example temporal/.env
```

Edit `temporal/.env`:

| Variable | What to set |
|---|---|
| POSTGRES_USER | `temporal` (default) |
| POSTGRES_PASSWORD | Any password for local Temporal Postgres |
| POSTGRES_DB | `temporal` (default) |
| TEMPORAL_HOST | `localhost` (default) |
| TEMPORAL_PORT | `7233` (default) |
| LOKI_URL | `http://localhost:3100` (default) |
| PROMETHEUS_URL | `http://localhost:9090` (default) |
| SLACK_WEBHOOK_URL | Same value as notifier/.env |
| WHATSAPP_ACCESS_TOKEN | Same value as notifier/.env |
| WHATSAPP_PHONE_NUMBER_ID | Same value as notifier/.env |
| WHATSAPP_RECIPIENT_NUMBER | Same value as notifier/.env |
| WHATSAPP_TEMPLATE_NAME | `sentinel_alert` (default) |
| WHATSAPP_TEMPLATE_LANG | `en_US` (default) |

#### 2.5 Agent .env

Used by: the AI diagnosis agent when running with `go run .` outside Docker.

```bash
cp agent/.env.example agent/.env
```

Edit `agent/.env`:

| Variable | What to set | Source |
|---|---|---|
| PORT | `8081` (default) | |
| LOG_LEVEL | `info` (default) | |
| TEMPORAL_ADDR | `localhost:7233` (default) | |
| TEMPORAL_NAMESPACE | `default` (default) | |
| TEMPORAL_TASK_QUEUE | `sentinel-incidents` (default) | |
| LOKI_URL | `http://localhost:3100` (default) | |
| PROMETHEUS_URL | `http://localhost:9090` (default) | |
| LLM_PROVIDER | `ollama` or `groq` | Section 8 |
| OLLAMA_BASE_URL | `http://localhost:11434` (default) | Section 8 |
| OLLAMA_MODEL | `llama3.1:8b` (default) | Section 8 |
| GROQ_API_KEY | Your Groq API key (only if LLM_PROVIDER=groq) | Section 8 |
| GROQ_MODEL | `llama3-8b-8192` (default) | |
| OTEL_EXPORTER_OTLP_ENDPOINT | `http://localhost:4317` (default) | |
| OTEL_SERVICE_NAME | `devops-sentinel-agent` (default) | |
| SLACK_WEBHOOK_URL | Same value as notifier/.env | Section 6 |
| SLACK_SIGNING_SECRET | Your Slack app signing secret | Section 6 |
| WHATSAPP_APP_SECRET | Your Meta app secret | Section 7 |
| WHATSAPP_VERIFY_TOKEN | Any string you choose (must match Meta console) | Section 7 |
| KUBECONFIG | Path to kubeconfig (for local kubectl) | Section 5 |
| MONGO_URI | `mongodb://localhost:27017` (default for local) | |
| BACKEND_HEALTH_URL | `http://localhost:3000/health` (default for local) | |

#### 2.6 Frontend

The frontend has no runtime environment variables. Its configuration is baked
into the nginx config at build time. No `.env` file is needed.

---

## 3. Kubernetes Secrets

When running on a kind or EKS cluster, secrets are stored as Kubernetes Secret
resources. They are never stored in git. The manifest files in `k8s/` and
`agent/k8s/` contain placeholder values only.

### Step-by-step: create all K8s secrets

Run these commands after your cluster is running (`kubectl get nodes` works).

#### 3.1 MongoDB credentials

```bash
kubectl create secret generic mongo-credentials \
  --namespace sentinel \
  --from-literal=MONGO_INITDB_ROOT_USERNAME="admin" \
  --from-literal=MONGO_INITDB_ROOT_PASSWORD="<your-mongo-password>" \
  --dry-run=client -o yaml | kubectl apply -f -
```

Replace `<your-mongo-password>` with a real password. Use the same value as
in your root `.env` file if you want local and cluster to match.

#### 3.2 Notifier secrets

```bash
kubectl create secret generic notifier-secrets \
  --namespace sentinel \
  --from-literal=SLACK_WEBHOOK_URL="<your-slack-webhook-url>" \
  --from-literal=WHATSAPP_ACCESS_TOKEN="<your-whatsapp-token>" \
  --from-literal=WHATSAPP_PHONE_NUMBER_ID="<your-phone-number-id>" \
  --from-literal=WHATSAPP_RECIPIENT_NUMBER="<your-recipient-number>" \
  --dry-run=client -o yaml | kubectl apply -f -
```

#### 3.3 Agent secrets

```bash
kubectl create secret generic agent-secrets \
  --namespace sentinel \
  --from-literal=GROQ_API_KEY="<your-groq-api-key>" \
  --from-literal=SLACK_SIGNING_SECRET="<your-slack-signing-secret>" \
  --from-literal=WHATSAPP_APP_SECRET="<your-meta-app-secret>" \
  --from-literal=WHATSAPP_VERIFY_TOKEN="<your-verify-token>" \
  --dry-run=client -o yaml | kubectl apply -f -
```

If using Ollama (no Groq key needed), set GROQ_API_KEY to any placeholder
string. The agent only reads it when `LLM_PROVIDER=groq`.

#### 3.4 Verify secrets exist (names only, no values)

```bash
kubectl get secret -n sentinel
```

Expected output:

```
NAME               TYPE       DATA   AGE
agent-secrets      Opaque     4      ...
mongo-credentials  Opaque     2      ...
notifier-secrets   Opaque     4      ...
```

To verify a specific secret has the right keys without printing values:

```bash
kubectl get secret notifier-secrets -n sentinel -o jsonpath='{.data.*}' | head -1
```

This prints the base64-encoded value of the first key. Do not decode it in
a shared terminal. To verify only the key names:

```bash
kubectl get secret notifier-secrets -n sentinel -o jsonpath='{.data}' | jq 'keys'
```

---

## 4. GitHub Repository Secrets

The CI pipeline (`.github/workflows/ci.yml`) uses two secrets:

| Secret name | Required for | How to set |
|---|---|---|
| `GITHUB_TOKEN` | Pushing images to GHCR | Automatic — GitHub provides this. No setup needed. |
| `SLACK_WEBHOOK_URL` | Slack notifications on pipeline success/failure | Manual setup below |

### Set the Slack webhook URL in GitHub

Prerequisites: the GitHub CLI (`gh`) must be installed and authenticated.

```bash
# Verify gh is authenticated
gh auth status

# Set the secret
gh secret set SLACK_WEBHOOK_URL
# Paste the webhook URL when prompted. The value is not displayed.

# Verify it exists (name only, no value)
gh secret list
```

Expected output includes:

```
SLACK_WEBHOOK_URL   Updated 2026-10-17
```

### If gh CLI is not available

Use the GitHub web UI:

1. Go to your repository on GitHub.
2. Settings > Secrets and variables > Actions.
3. Click "New repository secret".
4. Name: `SLACK_WEBHOOK_URL`
5. Value: paste your webhook URL.
6. Click "Add secret".

---

## 5. AWS Credentials

Required for: Terraform infrastructure (Step 4), EKS cluster access, ECR
image pushes.

### Option A: IAM user with access keys (simpler, less secure)

```bash
# Configure the AWS CLI
aws configure

# You will be prompted for:
#   AWS Access Key ID     — from IAM > Users > your-user > Security credentials
#   AWS Secret Access Key — from the same page
#   Default region name    — e.g. us-east-1
#   Default output format — json
```

Verify:

```bash
aws sts get-caller-identity
```

This should print your user ARN and account ID. If it returns
`InvalidClientTokenId`, your keys are expired or wrong. Rotate them in the
IAM console and re-run `aws configure`.

### Option B: AWS SSO (recommended for teams)

```bash
# Configure SSO
aws configure sso

# Follow the prompts for:
#   SSO start URL      — from your AWS SSO portal
#   SSO region          — e.g. us-east-1
#   SSO account ID     — your AWS account
#   SSO role           — e.g. AdministratorAccess or PowerUserAccess

# After authentication, set the default profile:
export AWS_PROFILE=<profile-name>
```

Verify:

```bash
aws sts get-caller-identity
```

### Security check: root user must not have access keys

```bash
aws iam list-access-keys --user-name root
```

If this returns any access keys, delete them immediately:

```bash
aws iam delete-access-key \
  --user-name root \
  --access-key-id <key-id>
```

The root user should use SSO or a hardware MFA token only, never programmatic
access keys.

### ECR login (for pushing images to AWS ECR)

```bash
aws ecr get-login-password --region us-east-1 | \
  docker login --username AWS --password-stdin \
  <account-id>.dkr.ecr.us-east-1.amazonaws.com
```

Replace `<account-id>` with your AWS account ID (visible in the output of
`aws sts get-caller-identity`).

---

## 6. Slack Setup

### 6.1 Create a Slack App

1. Go to https://api.slack.com/apps.
2. Click "Create New App" > "From scratch".
3. Name it "DevOps Sentinel".
4. Select your workspace.
5. Click "Create App".

### 6.2 Enable Incoming Webhooks

Used by: notifier, Temporal worker, agent, CI pipeline.

1. In the left sidebar, click "Incoming Webhooks".
2. Toggle "On".
3. Click "Add New Webhook to Workspace".
4. Select a channel (e.g. `#alerts`).
5. Click "Allow".
6. Copy the webhook URL. It looks like:
   `https://hooks.slack.com/services/T.../B.../...`

Store this URL in:

| Location | How |
|---|---|
| notifier/.env | `SLACK_WEBHOOK_URL=<url>` |
| temporal/.env | `SLACK_WEBHOOK_URL=<url>` |
| agent/.env | `SLACK_WEBHOOK_URL=<url>` |
| K8s Secret | `kubectl create secret ...` (see Section 3.2) |
| GitHub Secret | `gh secret set SLACK_WEBHOOK_URL` (see Section 4) |

### 6.3 Get the Signing Secret

Used by: the agent to verify that incoming button-click webhooks are
genuinely from Slack (HMAC-SHA256 signature verification).

1. In your Slack App settings, click "Basic Information" in the left sidebar.
2. Scroll to "App Credentials".
3. Find "Signing Secret".
4. Click "Show" and copy the value.

Store this value in:

| Location | How |
|---|---|
| agent/.env | `SLACK_SIGNING_SECRET=<secret>` |
| K8s Secret | Part of `agent-secrets` (see Section 3.3) |

### 6.4 Enable Interactive Components

Used by: the agent to receive Approve/Reject button clicks.

1. In your Slack App settings, click "Interactivity & Shortcuts".
2. Toggle "Interactivity" on.
3. Set "Request URL" to your agent's public URL + `/approval/slack`.
   - For local dev with ngrok: `https://<ngrok-id>.ngrok.io/approval/slack`
   - For production: `https://<your-domain>/approval/slack`
4. Click "Save Changes".

### 6.5 Rotation

To rotate the Slack signing secret:

1. In Slack App settings > Basic Information > App Credentials.
2. Click "Rotate" next to Signing Secret.
3. Update `SLACK_SIGNING_SECRET` in agent/.env and the K8s Secret:
   ```bash
   kubectl create secret generic agent-secrets \
     --namespace sentinel \
     --from-literal=SLACK_SIGNING_SECRET="<new-secret>" \
     --dry-run=client -o yaml | kubectl apply -f -
   ```
4. Restart the agent: `kubectl rollout restart deployment/agent -n sentinel`.

To rotate the webhook URL: delete the old webhook in Slack settings, create
a new one, and update all locations listed in section 6.2.

---

## 7. WhatsApp / Meta Cloud API Setup

### 7.1 Create a Meta App

1. Go to https://developers.facebook.com.
2. Log in with a Facebook account.
3. Click "My Apps" > "Create App".
4. Select "Business" type.
5. Name it "DevOps Sentinel".
6. Provide a contact email.
7. Click "Create App".

### 7.2 Add WhatsApp to the app

1. In the app dashboard, scroll to "Add Product".
2. Find "WhatsApp" and click "Set Up".

### 7.3 Get the access token

1. In the left sidebar, click "WhatsApp" > "API Setup".
2. Find "Temporary access token".
3. Copy the token.

Important: this token expires every 24 hours. For persistent access, create
a System User in Meta Business Manager and generate a permanent token with
the `whatsapp_business_messaging` permission.

Store the token in:

| Location | How |
|---|---|
| notifier/.env | `WHATSAPP_ACCESS_TOKEN=<token>` |
| temporal/.env | `WHATSAPP_ACCESS_TOKEN=<token>` |
| K8s Secret | Part of `notifier-secrets` (see Section 3.2) |

### 7.4 Get the Phone Number ID

1. In "WhatsApp" > "API Setup", find "Phone Number ID".
2. Copy the value (a 15-digit number).

Store it in:

| Location | How |
|---|---|
| notifier/.env | `WHATSAPP_PHONE_NUMBER_ID=<id>` |
| temporal/.env | `WHATSAPP_PHONE_NUMBER_ID=<id>` |
| K8s Secret | Part of `notifier-secrets` (see Section 3.2) |

### 7.5 Add a test recipient

1. In "WhatsApp" > "API Setup", find "To" field.
2. Enter your phone number in E.164 format (e.g. `+12025550123`).
3. Click "Send message". You receive a code via WhatsApp.
4. Enter the code in the portal to verify the number.

Store it in:

| Location | How |
|---|---|
| notifier/.env | `WHATSAPP_RECIPIENT_NUMBER=<number>` |
| temporal/.env | `WHATSAPP_RECIPIENT_NUMBER=<number>` |
| K8s Secret | Part of `notifier-secrets` (see Section 3.2) |

### 7.6 Get the App Secret

Used by: the agent to verify that incoming WhatsApp webhooks are from Meta
(HMAC-SHA256 signature verification).

1. In your Meta App settings, click "Basic Settings" in the left sidebar.
2. Find "App Secret".
3. Click "Show" and copy the value.

Store it in:

| Location | How |
|---|---|
| agent/.env | `WHATSAPP_APP_SECRET=<secret>` |
| K8s Secret | Part of `agent-secrets` (see Section 3.3) |

### 7.7 Set the webhook verify token

This is a string you choose. It is used when Meta first registers your webhook
URL. It is not a secret in the cryptographic sense, but it prevents others
from registering a fake webhook.

1. Choose any string, e.g. `sentinel-verify-2026`.
2. Store it in:

| Location | How |
|---|---|
| agent/.env | `WHATSAPP_VERIFY_TOKEN=<your-token>` |
| K8s Secret | Part of `agent-secrets` (see Section 3.3) |

3. In Meta Developer Console > WhatsApp > Configuration:
   - Set "Webhook URL" to `https://<your-domain>/approval/whatsapp`
   - Set "Verify token" to the same string.
   - Click "Verify and Save".
   - Subscribe to the "messages" webhook field.

### 7.8 Create the message template

The WhatsApp Cloud API enforces a 24-hour customer service window. Outside
this window, only pre-approved template messages can be sent. All automated
alert notifications use templates.

1. Go to Meta Business Manager (business.facebook.com).
2. Account tools > Message templates.
3. Click "Create template".
4. Category: Utility.
5. Name: `sentinel_alert`.
6. Language: English (en_US).
7. Body text: `DevOps Sentinel alert: {{1}}`
8. Submit for review. Test templates are approved in seconds.

Store the template name and language in:

| Location | Variable |
|---|---|
| notifier/.env | `WHATSAPP_TEMPLATE_NAME=sentinel_alert` |
| notifier/.env | `WHATSAPP_TEMPLATE_LANG=en_US` |
| temporal/.env | Same values |
| K8s Secret | Same values (part of notifier-secrets) |

### 7.9 Production requirements

Sending messages to numbers outside your verified test recipients requires
Meta Business Verification. This involves submitting business documents
(registration certificate, utility bill, or similar) through Meta Business
Manager. Approval typically takes 1-5 business days.

### 7.10 Rotation

| Credential | How to rotate |
|---|---|
| WHATSAPP_ACCESS_TOKEN (test) | Regenerate in Meta console. Expires every 24h automatically. |
| WHATSAPP_ACCESS_TOKEN (permanent) | In Meta Business Manager > System Users, generate a new token. Revoke the old one. |
| WHATSAPP_APP_SECRET | In Meta App settings > Basic Settings, reset the App Secret. Update agent/.env and K8s Secret. |

---

## 8. LLM Provider Setup

The AI agent uses an LLM to diagnose incidents. Two providers are supported.
Set `LLM_PROVIDER` to `ollama` or `groq` to choose.

### Option A: Ollama (local, fully free)

Ollama runs on your host machine. No API key is needed.

```bash
# Start Ollama
docker run -d --name ollama -p 11434:11434 ollama/ollama:0.3.6

# Pull the model (one-time, ~4.7 GB download)
docker exec ollama ollama pull llama3.1:8b

# Verify
curl http://localhost:11434/api/tags | jq '.models[].name'
```

Set in agent/.env:

```
LLM_PROVIDER=ollama
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama3.1:8b
```

For Kubernetes: Ollama runs on your host, not in the cluster. Point the agent
at the host IP reachable from inside kind:

```bash
HOST_IP=$(docker inspect sentinel-control-plane \
  --format '{{.NetworkSettings.Networks.kind.Gateway}}')

kubectl set env deployment/agent -n sentinel \
  OLLAMA_BASE_URL="http://${HOST_IP}:11434"
```

No credentials to rotate. To change the model: pull a new one and update
`OLLAMA_MODEL`.

### Option B: Groq (cloud, free tier)

Groq offers 6,000 requests/day and 500,000 tokens/day for free on
`llama3-8b-8192`.

1. Go to https://console.groq.com.
2. Create a free account.
3. Go to API Keys.
4. Click "Create API Key".
5. Copy the key (starts with `gsk_`).

Set in agent/.env:

```
LLM_PROVIDER=groq
GROQ_API_KEY=<your-key>
GROQ_MODEL=llama3-8b-8192
```

For Kubernetes: store the key in the `agent-secrets` Secret (see Section 3.3)
and set the env var on the deployment:

```bash
kubectl set env deployment/agent -n sentinel LLM_PROVIDER=groq
```

### Rotation

To rotate the Groq API key:

1. In the Groq console, create a new key.
2. Delete the old key.
3. Update agent/.env and the K8s Secret:
   ```bash
   kubectl create secret generic agent-secrets \
     --namespace sentinel \
     --from-literal=GROQ_API_KEY="<new-key>" \
     --dry-run=client -o yaml | kubectl apply -f -
   ```
4. Restart the agent: `kubectl rollout restart deployment/agent -n sentinel`.

---

## 9. Temporal Server Credentials

The Temporal server uses its own Postgres database with separate credentials.
These are not shared with the application MongoDB.

Set in temporal/.env:

| Variable | Value |
|---|---|
| POSTGRES_USER | `temporal` (any username) |
| POSTGRES_PASSWORD | Any password (generate with `openssl rand -base64 24`) |
| POSTGRES_DB | `temporal` (any database name) |

These are local-only credentials. The Temporal docker-compose file reads
them from `temporal/.env` and passes them to the Postgres container.

No rotation needed for local development. For production Temporal, use
AWS RDS with Secrets Manager rotation.

---

## 10. Kill Switch

The kill switch is a ConfigMap key that disables all automated remediation
immediately without restarting the agent.

### Default: disabled

The ConfigMap file (`agent/k8s/configmap-killswitch.yaml`) should have:

```yaml
remediation.enabled: "false"
```

Keep it `false` until you have tested the full approval flow in Step 9.

### Enable remediation after testing

```bash
kubectl patch configmap agent-config -n sentinel \
  --type merge \
  -p '{"data":{"remediation.enabled":"true"}}'
```

### Disable remediation (emergency)

```bash
kubectl patch configmap agent-config -n sentinel \
  --type merge \
  -p '{"data":{"remediation.enabled":"false"}}'
```

The agent reads this value on every remediation attempt. Changes take effect
within seconds. If the ConfigMap is missing or the key is absent, the kill
switch defaults to disabled (fail-closed).

---

## 11. Verification Checklist

Run these checks after setting up all credentials.

### 11.1 .env files exist and are gitignored

```bash
ls -la .env backend/.env notifier/.env temporal/.env agent/.env
```

Expected: all five files exist.

```bash
git check-ignore -v .env backend/.env notifier/.env temporal/.env agent/.env
```

Expected: each line prints the `.gitignore` rule that matches.

### 11.2 No empty values in .env files

```bash
for f in .env backend/.env notifier/.env temporal/.env agent/.env; do
  echo "=== $f ==="
  grep -n '=$' "$f" || echo "  (no empty values)"
done
```

Expected: "(no empty values)" for every file. If any line has `=` at the end
with nothing after it, that variable is empty and needs a value.

### 11.3 K8s secrets exist (requires running cluster)

```bash
kubectl get secret -n sentinel
```

Expected: `mongo-credentials`, `notifier-secrets`, `agent-secrets` listed.

### 11.4 GitHub secret exists (requires gh CLI)

```bash
gh secret list
```

Expected: `SLACK_WEBHOOK_URL` in the list.

### 11.5 AWS identity works

```bash
aws sts get-caller-identity
```

Expected: valid JSON with your account ID and ARN. No error.

### 11.6 Root user has no access keys

```bash
aws iam list-access-keys --user-name root
```

Expected: empty list or "NoSuchEntity" error (meaning no keys exist).

### 11.7 Kill switch is disabled

```bash
kubectl get configmap agent-config -n sentinel \
  -o jsonpath='{.data.remediation\.enabled}'
```

Expected: `false`.

### 11.8 No secrets in git

After your first commit:

```bash
# Install gitleaks if not present
go install github.com/gitleaks/gitleaks/v8@latest

# Scan the repo
gitleaks detect --source . --verbose
```

Expected: "no leaks found" or exit code 0.

### 11.9 Slack webhook works

```bash
# Replace with your actual webhook URL (do not share it)
curl -s -X POST "<your-slack-webhook-url>" \
  -H "Content-Type: application/json" \
  -d '{"text":"DevOps Sentinel test message"}'
```

Expected: `ok`.

### 11.10 WhatsApp API works

```bash
# Replace placeholders with your actual values
curl -s -X POST \
  "https://graph.facebook.com/v19.0/<phone-number-id>/messages" \
  -H "Authorization: Bearer <access-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "messaging_product": "whatsapp",
    "to": "<recipient-number>",
    "type": "template",
    "template": {
      "name": "sentinel_alert",
      "language": {"code": "en_US"},
      "components": [{
        "type": "body",
        "parameters": [{"type": "text", "text": "Test from DevOps Sentinel"}]
      }]
    }
  }'
```

Expected: JSON response with `"messaging_product": "whatsapp"` and a
`message_status` field.

---

## 12. Rotation Procedures

### Summary of rotation steps for each credential

| Credential | Frequency | How |
|---|---|---|
| MONGO_INITDB_ROOT_PASSWORD | Quarterly or on team change | Generate new password, update root .env and K8s Secret, restart mongo and backend |
| SLACK_WEBHOOK_URL | On compromise or team change | Delete old webhook in Slack settings, create new one, update all locations (Section 6.2) |
| SLACK_SIGNING_SECRET | On compromise | Rotate in Slack App settings, update agent/.env and K8s Secret (Section 6.5) |
| WHATSAPP_ACCESS_TOKEN (test) | Every 24 hours automatically | Regenerate in Meta console if expired (Section 7.3) |
| WHATSAPP_ACCESS_TOKEN (permanent) | On compromise | Create new System User token, revoke old one (Section 7.10) |
| WHATSAPP_APP_SECRET | On compromise | Reset in Meta App settings (Section 7.10) |
| GROQ_API_KEY | On compromise | Create new key, delete old key (Section 8) |
| AWS access keys | On compromise or team change | Create new key, update `aws configure`, delete old key in IAM console |
| POSTGRES_PASSWORD (Temporal) | On compromise | Update temporal/.env, recreate Temporal volume |

### Emergency: rotate everything at once

If you suspect a credential leak:

1. Set the kill switch to `false` immediately:
   ```bash
   kubectl patch configmap agent-config -n sentinel \
     --type merge -p '{"data":{"remediation.enabled":"false"}}'
   ```

2. Rotate the MongoDB password.
3. Rotate the Slack webhook URL and signing secret.
4. Rotate the WhatsApp access token and app secret.
5. Rotate the Groq API key (if used).
6. Rotate the AWS access keys.
7. Update all .env files and K8s Secrets.
8. Restart all affected services.
9. Run gitleaks to confirm no secrets remain in the repo:
   ```bash
   gitleaks detect --source . --verbose
   ```
10. If a secret was committed to git history, use `git filter-repo` or BFG
    Repo-Cleaner to remove it from history, then force-push. Anyone who
    already cloned the repo must re-clone.
