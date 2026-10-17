# DevOps Sentinel

A MERN application operated by an AI agent with a full DevOps pipeline.
Step 1 covers the core application and local development stack.

## Repository layout

```
.
├── backend/          Express API (Node.js 20, Mongoose)
├── frontend/         React + Vite SPA served by nginx
├── .github/
│   └── workflows/
│       └── ci.yml    GitHub Actions CI/CD pipeline
├── docker-compose.yml
├── .env.example      Root-level environment variable template
└── README.md
```

## Prerequisites

- Docker Desktop 4.x or newer (includes Compose V2)
- Git
- A GitHub account (for CI and GHCR image publishing)

Node.js is not required on the host for running the application.
It is only needed if you want to run the backend or frontend outside Docker.

## Quick start

### 1. Clone the repository

```bash
git clone https://github.com/<your-org>/devops-sentinel.git
cd devops-sentinel
```

### 2. Create the environment file

```bash
cp .env.example .env
```

Open `.env` and set `MONGO_INITDB_ROOT_USERNAME` and `MONGO_INITDB_ROOT_PASSWORD`
to values of your choice. These are used only for local development.
Never commit `.env`.

### 3. Start the stack

```bash
docker compose up --build
```

The first build takes 2-4 minutes because it downloads base images and
installs npm dependencies. Subsequent starts use the layer cache.

Wait until you see all three services report healthy:

```
devops-sentinel-mongo-1     | ...ready for start up
devops-sentinel-backend-1   | [startup] HTTP server listening on port 3000.
devops-sentinel-frontend-1  | ...start worker process ...
```

### 4. Open the application

Navigate to http://localhost in your browser.

## Stopping the stack

```bash
# Stop containers, keep data volume intact.
docker compose down

# Stop containers AND delete the MongoDB data volume (data is lost).
docker compose down -v
```

## API reference

| Method | Path         | Description                        |
|--------|--------------|------------------------------------|
| GET    | /health      | Server and database liveness check |
| GET    | /api/items   | Return all items, newest first     |
| POST   | /api/items   | Create a new item                  |

POST body: `{ "title": "your task title" }`

## Verification commands

Run these after `docker compose up --build` and all services are healthy.

### Health check

```bash
curl -s http://localhost/health | jq .
```

Expected response (HTTP 200):

```json
{
  "status": "ok",
  "db": "connected",
  "timestamp": "2026-10-17T10:00:00.000Z"
}
```

### Create an item

```bash
curl -s -X POST http://localhost/api/items \
  -H "Content-Type: application/json" \
  -d '{"title": "My first task"}' | jq .
```

Expected response (HTTP 201):

```json
{
  "item": {
    "_id": "...",
    "title": "My first task",
    "createdAt": "...",
    "updatedAt": "..."
  }
}
```

### List items

```bash
curl -s http://localhost/api/items | jq .
```

Expected response (HTTP 200):

```json
{
  "items": [
    {
      "_id": "...",
      "title": "My first task",
      "createdAt": "...",
      "updatedAt": "..."
    }
  ]
}
```

### Validation error

```bash
curl -s -X POST http://localhost/api/items \
  -H "Content-Type: application/json" \
  -d '{"title": ""}' | jq .
```

Expected response (HTTP 400):

```json
{
  "error": {
    "message": "title is required and must be a non-empty string"
  }
}
```

## CI pipeline

The GitHub Actions workflow at `.github/workflows/ci.yml` runs on every push
and pull request.

On every run:
- Builds the backend Docker image.
- Builds the frontend Docker image.
- Uses GitHub Actions cache to speed up subsequent builds.

On push to `main` only:
- Pushes both images to GitHub Container Registry (GHCR).
- Tags each image with `:latest` and `:<7-character commit SHA>`.

No secrets need to be configured. The workflow uses the built-in
`GITHUB_TOKEN` for GHCR authentication.

For private repositories, enable "Read and write permissions" for Actions
under: Settings > Actions > General > Workflow permissions.

## Troubleshooting

### "Cannot connect to the Docker daemon"

Docker Desktop is not running. Start it and retry.

### Health check shows "db": "disconnected"

MongoDB may still be starting. Wait 30 seconds and retry.
Check the mongo container logs: `docker compose logs mongo`

### Port 80 already in use

Another process is using port 80 on your machine.
Stop that process, or edit `docker-compose.yml` to change the frontend
port mapping from `"80:80"` to `"8080:80"`, then access the app on
http://localhost:8080.

### Build fails with "npm ci" error

Delete the lock file, regenerate it locally, and commit it:

```bash
cd backend && npm install && cd ..
cd frontend && npm install && cd ..
```

Then rebuild: `docker compose up --build`

### Mongosh healthcheck fails on startup

This is normal for the first 10-30 seconds while MongoDB initialises its
data files. Docker will retry automatically. If the container never becomes
healthy, check logs: `docker compose logs mongo`

---

## Step 2: Kubernetes on kind (local cluster)

### Prerequisites

| Tool | Minimum version | Install guide |
|---|---|---|
| Docker Desktop | 4.x | https://docs.docker.com/get-docker/ |
| kind | 0.23 | https://kind.sigs.k8s.io/docs/user/quick-start/#installation |
| kubectl | 1.30 | https://kubernetes.io/docs/tasks/tools/ |

kubectl is bundled with Docker Desktop. Verify with `kubectl version --client`.

### Directory layout (Step 2 additions)

```
k8s/
  base/                  Kustomize base — environment-independent manifests
    namespace.yaml
    mongo/               StatefulSet, headless Service, Secret
    backend/             Deployment, Service, HPA, PDB, ConfigMap
    frontend/            Deployment, Service
    kustomization.yaml
  overlays/
    local/               Kustomize overlay — kind-specific patches and Ingress
      ingress.yaml
      kustomization.yaml
kind/
  kind-config.yaml       Cluster definition (port mappings, node labels)
  setup.sh               One-shot cluster bootstrap script
scripts/
  validate-manifests.sh  Schema validation with kubeconform
```

### Liveness vs. readiness probes

Both probe types call the same `/health` endpoint on the backend, but they
mean different things to Kubernetes.

**Liveness probe**
Answers the question: "is this container still alive?"
If the probe fails, Kubernetes kills the container and restarts it.
Use it to detect a deadlock or an unrecoverable crash that the process
cannot clean up on its own.
A high `failureThreshold` (3) and long `periodSeconds` (20) avoids
restarting a pod that is simply waiting for MongoDB to finish starting.

**Readiness probe**
Answers the question: "is this container ready to serve traffic?"
If the probe fails, Kubernetes removes the pod from the Service's endpoint
list. Traffic stops going to it. The pod is not restarted.
Use it to hold traffic back during startup, a slow database reconnect, or a
temporary dependency outage. The pod recovers on its own once the dependency
comes back.

**Rule of thumb:** if a container should be killed, use liveness.
If it should be taken out of rotation temporarily, use readiness.

### Start the cluster

```bash
# Step 1 must be complete: images must exist in the local Docker daemon.
# If you have not run the Step 1 stack yet:
docker compose up --build
docker compose down

# Bootstrap the kind cluster (creates cluster, loads images, installs
# ingress controller and metrics-server, applies manifests).
bash kind/setup.sh
```

The script is idempotent. Running it a second time skips cluster creation
if the cluster already exists.

### Verify the cluster is up

```bash
kubectl get nodes
```
Expected: one node named `sentinel-control-plane` with status `Ready`.

### Access the application

After `setup.sh` completes the application is available at http://localhost.
No port-forward is needed because the kind cluster maps host port 80 to the
ingress controller.

If port 80 is in use on your machine, edit `kind/kind-config.yaml` before
running setup.sh, changing `hostPort: 80` to `hostPort: 8080`. Then access
the app at http://localhost:8080.

### Manual port-forward (alternative to ingress)

Use this if the ingress controller is not ready or you want to test a single
service directly.

```bash
# Backend API directly (bypasses ingress and frontend proxy)
kubectl port-forward -n sentinel svc/backend 3000:3000

# Frontend directly
kubectl port-forward -n sentinel svc/frontend 8080:80

# MongoDB (for a database client like mongosh or Compass)
kubectl port-forward -n sentinel svc/mongo 27017:27017
```

### Validate manifests (no cluster required)

```bash
bash scripts/validate-manifests.sh
```

Expected last line: `[PASS] All validation checks passed.`

To also run a server-side dry run against the live kind cluster:

```bash
LIVE=true bash scripts/validate-manifests.sh
```

### Verification checklist

**Check 1 - All pods are Running and Ready**
```bash
kubectl get pods -n sentinel
```
Expected: all pods show `Running` and `READY` column shows all containers ready
(e.g. `1/1` or `2/2`). mongo-0 may take up to 60 seconds on first start.

**Check 2 - Health endpoint via ingress**
```bash
curl -s http://localhost/health | jq .
```
Expected:
```json
{ "status": "ok", "db": "connected", "timestamp": "..." }
```

**Check 3 - Create an item via ingress**
```bash
curl -s -X POST http://localhost/api/items \
  -H "Content-Type: application/json" \
  -d '{"title": "k8s task"}' | jq .
```
Expected: HTTP 201 with an `item` object.

**Check 4 - List items via ingress**
```bash
curl -s http://localhost/api/items | jq .items
```
Expected: array containing the item created in Check 3.

**Check 5 - HPA is visible**
```bash
kubectl get hpa -n sentinel
```
Expected: one row for `backend`. The TARGETS column shows current CPU
utilization. It may show `<unknown>` for the first 60-90 seconds while
metrics-server collects its first sample.

**Check 6 - PodDisruptionBudget is present**
```bash
kubectl get pdb -n sentinel
```
Expected: one row for `backend` with `MIN AVAILABLE: 1`.

**Check 7 - Containers run as non-root**
```bash
kubectl exec -n sentinel \
  $(kubectl get pod -n sentinel -l app.kubernetes.io/name=backend -o name | head -1) \
  -- id
```
Expected: output contains `uid=1001`.

```bash
kubectl exec -n sentinel \
  $(kubectl get pod -n sentinel -l app.kubernetes.io/name=frontend -o name | head -1) \
  -- id
```
Expected: output contains `uid=101`.

**Check 8 - Data survives a pod restart**
```bash
# Note the item count before restart.
curl -s http://localhost/api/items | jq '.items | length'

# Delete the mongo pod. The StatefulSet recreates it and reattaches the PVC.
kubectl delete pod -n sentinel mongo-0

# Wait for mongo-0 to return to Running.
kubectl wait --namespace sentinel --for=condition=ready pod/mongo-0 --timeout=90s

# Item count must be the same.
curl -s http://localhost/api/items | jq '.items | length'
```
Expected: same count before and after the restart.

**Check 9 - Liveness and readiness probes pass**
```bash
kubectl describe pod -n sentinel \
  $(kubectl get pod -n sentinel -l app.kubernetes.io/name=backend -o name | head -1) \
  | grep -A5 "Liveness\|Readiness"
```
Expected: both probes show `Success` and zero failures.

### Stop and clean up

```bash
# Remove only the application resources (cluster stays up).
kubectl delete -k k8s/overlays/local

# Delete the entire cluster (all data is lost).
kind delete cluster --name sentinel
```

### Troubleshooting

**Pods stuck in Pending**
The PVC for MongoDB has not been provisioned. Check:
```bash
kubectl describe pvc -n sentinel
kubectl get storageclass
```
kind ships with a `standard` StorageClass backed by local host paths. If
it is missing, re-run `setup.sh`.

**ImagePullBackOff on backend or frontend**
The image was not loaded into the kind node. Re-run:
```bash
kind load docker-image devops-project-1-backend:latest --name sentinel
kind load docker-image devops-project-1-frontend:latest --name sentinel
kubectl rollout restart deployment/backend deployment/frontend -n sentinel
```

**HPA shows `<unknown>` CPU after several minutes**
metrics-server may not be patched correctly. Check:
```bash
kubectl logs -n kube-system deployment/metrics-server | tail -20
```
If you see TLS errors, re-apply the patch from `setup.sh`:
```bash
kubectl patch deployment metrics-server -n kube-system --type=json \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'
```

**Ingress returns 404**
The ingress controller may still be starting. Wait 30 seconds and retry.
Check its status:
```bash
kubectl get pods -n ingress-nginx
```

---

## Step 3: Observability

### Architecture

```
Backend (Node.js)
  |-- OTel SDK (traces + metrics) --> OTel Collector :4317 (gRPC)
  |-- stdout JSON logs             --> Promtail --> Loki

OTel Collector
  |-- traces  --> Tempo  :4317
  |-- metrics --> Prometheus scrape endpoint :8889
  |-- logs    --> Loki   :3100

Grafana
  |-- datasource: Prometheus (metrics + exemplars)
  |-- datasource: Loki       (logs, links to Tempo)
  |-- datasource: Tempo      (traces, links to Loki)
  |-- dashboard:  DevOps Sentinel (request rate, error rate, p95 latency,
                                   pod CPU, pod memory, recent error logs)
```

### Directory layout (Step 3 additions)

```
observability/
  kustomization.yaml
  otelcol/        configmap, deployment, service
  prometheus/     clusterrole, configmap, deployment, service
  loki/           configmap, deployment, service
  promtail/       configmap, daemonset (ClusterRole + ClusterRoleBinding)
  tempo/          configmap, deployment, service
  grafana/        datasources-configmap, dashboard-configmap,
                  deployment, service
backend/src/
  tracing.js      OTel SDK init (must be first import in index.js)
  logger.js       Structured JSON logger with trace_id / span_id injection
```

### Frontend OpenTelemetry

Browser tracing requires a CORS-enabled OTLP/HTTP endpoint. The OTel
Collector does not expose CORS headers by default and adding them requires
an additional nginx proxy layer. The signal value at this stage is low
(page-load spans only). This is noted as a future enhancement once the
backend trace data is proven useful.

### Deploy the observability stack

The observability stack is included in the local overlay. A fresh cluster
setup installs everything automatically:

```bash
bash kind/setup.sh
```

To apply only the observability changes to an existing cluster:

```bash
kubectl apply -k k8s/overlays/local
```

### Access Grafana

Grafana is not exposed through the ingress (it would conflict with the `/`
frontend route). Access it with a port-forward:

```bash
kubectl port-forward -n sentinel svc/grafana 3000:3000
```

Open http://localhost:3000 in your browser. Anonymous viewer access is
enabled — no login required for local development.

Navigate to Dashboards > DevOps Sentinel to see the provisioned dashboard.

### Generate traffic and see traces

Open a second terminal and send a burst of requests:

```bash
# Create 10 items in quick succession.
for i in $(seq 1 10); do
  curl -s -X POST http://localhost/api/items \
    -H "Content-Type: application/json" \
    -d "{\"title\": \"load test item ${i}\"}" > /dev/null
done

# List items a few times.
for i in $(seq 1 5); do
  curl -s http://localhost/api/items > /dev/null
done
```

Then in Grafana:

1. Open Explore > select the Tempo datasource.
2. Use "Search" mode, set Service Name = `devops-sentinel-backend`, click Run Query.
3. Click any trace row to open the trace waterfall. You will see spans for
   the HTTP handler, the Express route, and the Mongoose query.
4. Click the Loki button next to a span to jump to the log lines for that
   trace. The log line includes `"trace_id"` matching the trace you selected.

### Verification checklist

**Check 1 - All observability pods are Running**
```bash
kubectl get pods -n sentinel
```
Expected: `otelcol-*`, `prometheus-*`, `loki-*`, `promtail-*`, `tempo-*`,
and `grafana-*` all show `Running` and `READY 1/1`.

**Check 2 - OTel Collector is receiving spans**
```bash
kubectl logs -n sentinel deployment/otelcol | grep "TracesExporter"
```
Expected: lines showing spans exported to Tempo after you send a request.

**Check 3 - A trace appears for a POST request**
```bash
# Send one POST and note the response item _id.
curl -s -X POST http://localhost/api/items \
  -H "Content-Type: application/json" \
  -d '{"title": "trace verification"}' | jq ._id
```
Then in Grafana: Explore > Tempo > Search > Service Name:
`devops-sentinel-backend`. The POST span should appear within 15 seconds.

**Check 4 - A log line links to its trace ID**
```bash
kubectl logs -n sentinel deployment/backend | grep "item created" | tail -3
```
Expected: JSON lines containing both `"trace_id"` and `"span_id"` fields
with non-empty 32-character hex values.

```bash
# Example of a correctly instrumented log line:
# {"timestamp":"...","level":"info","service.name":"devops-sentinel-backend",
#  "trace_id":"4bf92f3577b34da6a3ce929d0e0e4736",
#  "span_id":"00f067aa0ba902b7","message":"item created","item_id":"..."}
```

**Check 5 - Dashboard shows non-zero request rate**

In Grafana, open the DevOps Sentinel dashboard.
Generate traffic (the loop in the previous section), then wait 30 seconds
for the next scrape. The "Request Rate" panel should show a non-zero value.

**Check 6 - Loki receives logs**
```bash
kubectl port-forward -n sentinel svc/loki 3100:3100 &
curl -s "http://localhost:3100/loki/api/v1/labels" | jq .
```
Expected: JSON with a `data` array containing `"app"`, `"namespace"`,
`"pod"`, and `"trace_id"` label names.
Kill the port-forward: `kill %1`

**Check 7 - Prometheus scrape targets are up**
```bash
kubectl port-forward -n sentinel svc/prometheus 9090:9090 &
curl -s "http://localhost:9090/api/v1/targets" | \
  jq '.data.activeTargets[] | {job: .labels.job, health: .health}'
```
Expected: `otelcol-app` and `otelcol-self` targets both showing
`"health": "up"`.
Kill the port-forward: `kill %1`

**Check 8 - Log-to-trace correlation in Grafana**

1. Open Grafana > Explore > select Loki datasource.
2. Query: `{namespace="sentinel", app="backend"} | json`
3. Find a log line that contains `trace_id`.
4. Click the Tempo icon next to the `trace_id` value.
5. Grafana opens Tempo and shows the trace for that log line.

### Rollback / cleanup

```bash
# Remove the observability stack only (application stack stays up).
kubectl delete -k observability/

# Remove everything including the application stack.
kubectl delete -k k8s/overlays/local

# Remove the entire cluster.
kind delete cluster --name sentinel
```

### Troubleshooting

**OTel Collector pod is in CrashLoopBackOff**
```bash
kubectl logs -n sentinel deployment/otelcol
```
The most common cause is the health_check extension failing to bind.
Check the config: `kubectl get configmap otelcol-config -n sentinel -o yaml`

**Grafana shows "Data source connected but no labels received" for Loki**
Loki takes 30-60 seconds to initialise its index on first start.
Wait and refresh. Check: `kubectl logs -n sentinel deployment/loki | tail -20`

**Tempo shows no traces**
Confirm the backend is sending to the collector:
```bash
kubectl logs -n sentinel deployment/backend | grep "OpenTelemetry SDK started"
```
If that line is missing, tracing.js failed to start. Check:
```bash
kubectl logs -n sentinel deployment/backend | head -20
```

**trace_id is empty in log lines**
This means the logger ran outside an active OTel span. Confirm that
`tracing.js` is the first import in `index.js` and that the OTel SDK
started without error before any HTTP requests arrived.

---

## Step 6: Notifications

### Architecture

```
Prometheus (firing alert)
  --> Alertmanager :9093
       --> POST http://notifier:8080/webhook
            |--> Slack  (Block Kit incoming webhook)
            \--> WhatsApp (Meta Cloud API template message)

GitHub Actions (push to main)
  --> slackapi/slack-github-action
       --> Slack (Block Kit: success or failure message)
```

### Directory layout (Step 6 additions)

```
notifier/
  main.go          Go HTTP service: /webhook, /test, /health
  go.mod           stdlib only, no external dependencies
  Dockerfile       golang:1.22.4-alpine build + distroless:nonroot runtime
  .env.example     All required environment variables documented

observability/
  prometheus-rules/configmap.yaml   5 alert rules
  alertmanager/configmap.yaml       Routing and inhibition config
  alertmanager/deployment.yaml
  alertmanager/service.yaml

k8s/base/notifier/
  secret.yaml      Placeholder Secret (never commit real values)
  deployment.yaml
  service.yaml

.github/workflows/ci.yml            CHANGED: added build-notifier and notify jobs
```

---

### Secrets list

These are the names of all secrets required by Step 6. Values are never in
any file in this repository.

| Secret name | Where to set it | Description |
|---|---|---|
| `SLACK_WEBHOOK_URL` | GitHub repo secrets AND Kubernetes Secret `notifier-secrets` | Slack incoming webhook URL |
| `WHATSAPP_ACCESS_TOKEN` | Kubernetes Secret `notifier-secrets` | Meta Cloud API bearer token |
| `WHATSAPP_PHONE_NUMBER_ID` | Kubernetes Secret `notifier-secrets` | FROM phone number ID (not the number itself) |
| `WHATSAPP_RECIPIENT_NUMBER` | Kubernetes Secret `notifier-secrets` | TO phone number in E.164 format |

---

### Slack setup (5 minutes)

1. Go to https://api.slack.com/apps and click "Create New App" > "From scratch".
2. Name it "DevOps Sentinel", pick your workspace.
3. In the left sidebar click "Incoming Webhooks", toggle it on.
4. Click "Add New Webhook to Workspace", pick the channel (e.g. `#alerts`).
5. Copy the webhook URL (starts with `https://hooks.slack.com/services/...`).
6. Add it to GitHub: Settings > Secrets and variables > Actions > New secret
   Name: `SLACK_WEBHOOK_URL`, Value: the URL you copied.
7. Add it to Kubernetes:
   ```bash
   kubectl create secret generic notifier-secrets \
     --namespace sentinel \
     --from-literal=SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..." \
     --from-literal=WHATSAPP_ACCESS_TOKEN="placeholder" \
     --from-literal=WHATSAPP_PHONE_NUMBER_ID="placeholder" \
     --from-literal=WHATSAPP_RECIPIENT_NUMBER="placeholder" \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

---

### WhatsApp setup (Meta Cloud API test number)

#### Why templates are required

The Meta WhatsApp Cloud API enforces a 24-hour customer service window.
If the recipient has not sent a message to the test number within the last
24 hours, the API rejects free-form text with error code 131047. Pre-approved
template messages bypass this restriction and can be sent at any time.

Always use template messages for automated alerts. Reserve free-form text
only for conversational flows where the user initiates the session.

#### Step-by-step test setup

1. Go to https://developers.facebook.com and log in with a Facebook account.
2. Click "My Apps" > "Create App" > "Business" type.
3. Name it "DevOps Sentinel", provide a contact email.
4. In the app dashboard, find "WhatsApp" in the product list and click "Set up".
5. You land on the "API Setup" page. Note:
   - "Temporary access token" (expires every 24 hours — see note on permanent tokens below).
   - "Phone number ID" of the test number (format: a 15-digit number).
6. Under "To", enter your personal WhatsApp number in E.164 format (+country-code-number).
   Click "Send message" to verify it as a test recipient. You receive a code via WhatsApp.
   Enter the code in the portal.
7. Create a message template:
   - Go to WhatsApp Manager (business.facebook.com) > Account tools > Message templates.
   - Click "Create template" > Category: "Utility" > Name: `sentinel_alert`.
   - Body text: `DevOps Sentinel alert: {{1}}`
   - Language: English (en_US).
   - Submit for review. Test templates are approved in seconds.
8. Add the secrets to Kubernetes:
   ```bash
   kubectl create secret generic notifier-secrets \
     --namespace sentinel \
     --from-literal=SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..." \
     --from-literal=WHATSAPP_ACCESS_TOKEN="your-24h-token" \
     --from-literal=WHATSAPP_PHONE_NUMBER_ID="123456789012345" \
     --from-literal=WHATSAPP_RECIPIENT_NUMBER="+12025550123" \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

#### Permanent access token (production)

The test token expires every 24 hours. For persistent operation:

1. In Meta Business Manager, go to System Users > Add > Admin.
2. Assign the system user to the WhatsApp app with
   `whatsapp_business_messaging` permission.
3. Click "Generate token" > select the app > choose "Never" expiry.
4. Use this token in place of the temporary one.

#### Production requirements

Sending messages to numbers outside your verified test recipients requires
Meta Business Verification. This involves submitting business documents
(registration certificate, utility bill, or similar) through Meta Business
Manager. Approval typically takes 1-5 business days.

---

### Deploy the notifier

Build and load the notifier image, then apply the manifests:

```bash
# Build the notifier image
docker build -t devops-sentinel-notifier:latest ./notifier

# Load it into the kind cluster
kind load docker-image devops-sentinel-notifier:latest --name sentinel

# Apply the updated manifests
kubectl apply -k k8s/overlays/local
```

---

### Terminal commands: apply and verify

```bash
# Apply observability updates (alertmanager + rules)
kubectl apply -k k8s/overlays/local

# Reload Prometheus config without restart (picks up new rule_files)
kubectl port-forward -n sentinel svc/prometheus 9090:9090 &
curl -s -X POST http://localhost:9090/-/reload
kill %1

# Check Prometheus loaded the rules
kubectl port-forward -n sentinel svc/prometheus 9090:9090 &
curl -s http://localhost:9090/api/v1/rules | jq '.data.groups[].name'
kill %1
# Expected: "sentinel.backend" in the output

# Check Alertmanager is up
kubectl get pods -n sentinel -l app.kubernetes.io/name=alertmanager

# Check notifier is up
kubectl get pods -n sentinel -l app.kubernetes.io/name=notifier
```

---

### Verification checklist

**Check 1 - Send a test notification manually**

```bash
kubectl port-forward -n sentinel svc/notifier 8080:8080 &
curl -s -X POST http://localhost:8080/test
kill %1
```

Expected:
- `{"status":"test sent"}` in the terminal.
- A message appears in your Slack channel within 5 seconds.
- If WhatsApp secrets are configured: a WhatsApp message arrives within 30 seconds.

**Check 2 - Prometheus alert rules are loaded**

```bash
kubectl port-forward -n sentinel svc/prometheus 9090:9090 &
curl -s "http://localhost:9090/api/v1/rules" | \
  jq '.data.groups[].rules[].name'
kill %1
```

Expected output includes:
```
"HighErrorRate"
"HighLatencyP95"
"PodCrashLooping"
"BackendDown"
"BackendHealthDegraded"
```

**Check 3 - Alertmanager UI is reachable**

```bash
kubectl port-forward -n sentinel svc/alertmanager 9093:9093 &
```

Open http://localhost:9093 in a browser. Expected: Alertmanager web UI.

```bash
kill %1
```

**Check 4 - Trigger a real alert by stopping the backend**

```bash
# Scale backend to zero replicas
kubectl scale deployment backend -n sentinel --replicas=0

# Wait 60 seconds for the alert to fire (BackendDown has for: 0m)
sleep 65

# Check Alertmanager shows the alert
kubectl port-forward -n sentinel svc/alertmanager 9093:9093 &
curl -s http://localhost:9093/api/v2/alerts | jq '.[].labels.alertname'
kill %1
```

Expected: `"BackendDown"` in the output.
Expected: A Slack message arrives with title `[FIRING] BackendDown — critical`.
Expected (if WhatsApp configured): A WhatsApp template message arrives.

```bash
# Restore the backend
kubectl scale deployment backend -n sentinel --replicas=2

# Wait for resolved notification (Alertmanager sends after resolve_timeout: 5m)
```

**Check 5 - Alertmanager routes to the notifier**

```bash
kubectl logs -n sentinel deployment/alertmanager | grep -i "notifier"
kubectl logs -n sentinel deployment/notifier | grep "webhook received"
```

Expected: notifier log line with `"status":"firing"` and `"alerts":1`.

**Check 6 - GitHub Actions Slack notification**

Push any commit to main:
```bash
git add .
git commit -m "chore: test slack ci notification"
git push origin main
```

Expected: A Slack message arrives after all build jobs finish.
Check the Actions tab for the "Notify Slack" job status.

**Check 7 - Notifier logs include structured JSON**

```bash
kubectl logs -n sentinel deployment/notifier | tail -5
```

Expected: JSON lines with `"level"`, `"message"`, and `"time"` fields.

---

### Rollback / cleanup

```bash
# Restore backend if scaled to zero during testing
kubectl scale deployment backend -n sentinel --replicas=2

# Remove the notifier secret (does not delete the placeholder in git)
kubectl delete secret notifier-secrets -n sentinel

# Remove alertmanager and notifier without touching the application stack
kubectl delete deployment alertmanager notifier -n sentinel
kubectl delete service alertmanager notifier -n sentinel
kubectl delete configmap alertmanager-config prometheus-rules -n sentinel
```

### Troubleshooting

**Notifier pod is in CrashLoopBackOff**

The secret values are still placeholders. The notifier starts successfully
even with placeholder values (it logs a warning and disables the channel).
If it is crashing, check:
```bash
kubectl logs -n sentinel deployment/notifier
```

**Slack messages are not arriving**

- Verify the webhook URL is correct: `kubectl get secret notifier-secrets -n sentinel -o jsonpath='{.data.SLACK_WEBHOOK_URL}' | base64 -d`
- Test the webhook directly: `curl -s -X POST <webhook-url> -H 'Content-Type: application/json' -d '{"text":"test"}'`
- Expected response: `ok`

**WhatsApp returns error 131047**

The recipient has not messaged the test number in the last 24 hours.
Ask the recipient to send any message to the test number to reopen the
customer service window. For automated alerts, always use template messages
instead of free-form text.

**WhatsApp returns error 131030**

The recipient number is not in the verified test recipients list.
Add it in: Meta Developer Console > WhatsApp > API Setup > To (add number).

**Prometheus rules not loading**

The rules ConfigMap must be mounted at `/etc/prometheus/rules/`. Verify:
```bash
kubectl exec -n sentinel deployment/prometheus -- ls /etc/prometheus/rules/
```
Expected: `sentinel.rules.yaml`

If missing, the rules volume mount was not applied. Re-apply the manifests:
```bash
kubectl apply -k k8s/overlays/local
kubectl rollout restart deployment/prometheus -n sentinel
```

---

## Step 7: Temporal Workflow Engine

### Why Temporal runs in a separate Compose file

The Temporal server and its Postgres database are infrastructure, not the
application. Keeping them in `temporal/docker-compose.temporal.yml` means:

- `docker compose down` on the app stack does not destroy workflow history.
  An in-flight incident workflow survives an application restart.
- The Temporal Postgres database has its own credentials and lifecycle,
  independent of the application MongoDB.
- Temporal can stay running across multiple app code changes during
  development without losing state.

### Workflow determinism — the most important concept in this step

Temporal replays a workflow's execution history to rebuild its state after a
worker restarts or crashes. Replay means the workflow function runs again from
the beginning, but all recorded activity results are replayed from history
instead of re-executing.

For replay to produce exactly the same sequence of SDK calls, workflow code
must be fully deterministic: given the same history, it must always make the
same decisions in the same order.

**Never do these things inside a workflow function:**

| Forbidden | Reason | Use instead |
|---|---|---|
| `time.Now()` | Returns a different value on replay | `workflow.Now(ctx)` |
| `time.Sleep()` | Not controlled by the SDK scheduler | `workflow.Sleep(ctx, d)` |
| `time.After()`, `time.NewTimer()` | Same as above | `workflow.NewTimer(ctx, d)` |
| `math/rand`, `crypto/rand` | Non-deterministic | `workflow.SideEffect(ctx, f)` |
| HTTP calls, file reads, DB queries | Any I/O | Move to an activity |
| `go func()` | Not controlled by the SDK | `workflow.Go(ctx, f)` |
| `select {}` with real channels | Not controlled by the SDK | `workflow.GetSignalChannel` + `workflow.NewSelector` |
| Global mutable state | Changes between replays | Pass as workflow input |

Everything that is not a pure computation or a Temporal SDK call belongs in
an activity. Activities are the boundary between deterministic workflow logic
and the real world.

### Directory layout

```
temporal/
  docker-compose.temporal.yml   Temporal server + Postgres + Web UI
  dynamicconfig.yaml            Temporal server settings (mounted as a volume)
  .env.example                  Postgres credentials and service URLs

  worker/
    main.go                     Worker entry point (connect, register, start)
    go.mod                      go.temporal.io/sdk v1.27.1
    workflow_test.go            7 workflow tests + 2 unit tests

    workflow/
      incident.go               IncidentWorkflow (deterministic)
      export_test.go            Test-only exports of unexported helpers

    activity/
      loki.go                   FetchLogs — queries Loki HTTP API
      prometheus.go             FetchMetrics — queries Prometheus HTTP API
      notify.go                 SendNotification — Slack + WhatsApp fan-out
```

### Workflow lifecycle

```
Start: IncidentWorkflow(IncidentInput)
  |
  +--> [parallel] FetchLogs (Loki)
  +--> [parallel] FetchMetrics (Prometheus)
  |
  +--> SendNotification (initial alert + evidence summary)
  |
  +--> Wait up to 30 min for signal "decision"
  |       |
  |       +--> approve  --> SendNotification(approved) --> return result
  |       +--> reject   --> SendNotification(rejected) --> return result
  |       +--> ignore   --> SendNotification(ignored)  --> return result
  |
  +--> [timeout] SendNotification (reminder)
  |
  +--> Wait up to 15 min for signal "decision"
  |       |
  |       +--> (same branches as above)
  |
  +--> [timeout] SendNotification (escalation) --> return result{decision: "timeout"}
```

### Setup

#### Prerequisites

| Tool | Version | Install |
|---|---|---|
| Docker Desktop | 4.x | https://docs.docker.com/get-docker/ |
| Go | 1.22 | https://go.dev/dl/ |
| Temporal CLI | latest | https://docs.temporal.io/cli#install |

Install the Temporal CLI:
```bash
# macOS / Linux
curl -sSf https://temporal.download/cli.sh | sh

# Windows (PowerShell)
# Download from https://github.com/temporalio/cli/releases and add to PATH
```

#### Start the Temporal server

```bash
cd temporal
cp .env.example .env          # edit POSTGRES_PASSWORD to a real value
docker compose -f docker-compose.temporal.yml up -d

# Verify the server is healthy (wait ~30 seconds on first start)
docker compose -f docker-compose.temporal.yml ps
```

Expected: `temporal` and `temporal-postgres` both show `healthy`.

Open the Web UI: http://localhost:8088

#### Install Go dependencies and run tests (no server needed)

```bash
cd temporal/worker
go mod tidy                   # downloads all dependencies and generates go.sum
go test ./... -v -count=1
```

Expected: 9 tests pass (`PASS`), no failures.

#### Start the worker

In a separate terminal, with the observability stack running from Step 3:

```bash
cd temporal/worker
go run . \
  -loki-url http://localhost:3100 \
  -prometheus-url http://localhost:9090 \
  -slack-webhook "$SLACK_WEBHOOK_URL"
```

Expected log line:
```json
{"time":"...","level":"INFO","message":"starting worker","taskQueue":"sentinel-incidents","namespace":"default"}
```

The worker appears in the Temporal Web UI under Workers.

### Start a workflow manually

Open a second terminal after the worker is running:

```bash
temporal workflow start \
  --type IncidentWorkflow \
  --task-queue sentinel-incidents \
  --namespace default \
  --workflow-id "incident-manual-001" \
  --input '{
    "AlertName":   "BackendDown",
    "Severity":    "critical",
    "Summary":     "Manual test incident",
    "Description": "This is a manually triggered test.",
    "StartsAt":    "2026-10-17T10:00:00Z"
  }'
```

Expected: workflow appears in the Web UI at http://localhost:8088 with status
`Running`.

### Send a decision signal

```bash
# Approve the proposed remediation
temporal workflow signal \
  --workflow-id "incident-manual-001" \
  --namespace default \
  --name "decision" \
  --input '"approve"'
```

Expected: the workflow moves to `Completed` status in the Web UI. The result
field shows `decision: approve`.

Other valid signal values:
```bash
--input '"reject"'
--input '"ignore"'
```

### Verification checklist

**Check 1 - Temporal server is healthy**
```bash
docker compose -f temporal/docker-compose.temporal.yml ps
```
Expected: `temporal` shows `(healthy)`.

**Check 2 - All tests pass**
```bash
cd temporal/worker && go test ./... -v -count=1
```
Expected: `ok github.com/devops-sentinel/temporal-worker` with 9 passed tests.

**Check 3 - Workflow starts and appears in Web UI**

Run the `temporal workflow start` command above.
Open http://localhost:8088 and confirm the workflow is `Running`.

**Check 4 - Signal is delivered and workflow completes**

Run the `temporal workflow signal` command above.
In the Web UI, click the workflow run. Expected:
- Event history shows `WorkflowExecutionSignaled` with `decision` name.
- Final event is `WorkflowExecutionCompleted`.
- Result JSON shows `"Decision": "approve"`.

**Check 5 - Worker kill and resume (state durability test)**

This is the key property of Temporal: workflows survive worker restarts.

```bash
# Terminal 1: start a workflow that will wait for a signal
temporal workflow start \
  --type IncidentWorkflow \
  --task-queue sentinel-incidents \
  --namespace default \
  --workflow-id "incident-durability-test" \
  --input '{"AlertName":"BackendDown","Severity":"critical","Summary":"Durability test","Description":"Test","StartsAt":"2026-10-17T10:00:00Z"}'

# Terminal 2: watch the workflow status
temporal workflow describe \
  --workflow-id "incident-durability-test" \
  --namespace default

# Kill the worker process (Ctrl-C in its terminal, or kill its PID)
# The workflow remains Running in Temporal — it is stored in Postgres.

# Verify it is still Running despite no worker
temporal workflow describe \
  --workflow-id "incident-durability-test" \
  --namespace default
# Status: Running

# Restart the worker
cd temporal/worker && go run .

# Send the signal — the restarted worker picks up the workflow from where it left off
temporal workflow signal \
  --workflow-id "incident-durability-test" \
  --namespace default \
  --name "decision" \
  --input '"approve"'

# Confirm completion
temporal workflow describe \
  --workflow-id "incident-durability-test" \
  --namespace default
# Status: Completed
```

Expected: the workflow completes successfully after the worker restart.
The Web UI event history shows the full replay — the activities that ran
before the worker was killed are shown as replayed, not re-executed.

**Check 6 - Workflow history shows activity retries**

Trigger an activity failure by stopping Loki before starting a workflow:

```bash
kubectl scale deployment loki -n sentinel --replicas=0   # if using kind
# or: docker compose stop loki                           # if using docker compose

temporal workflow start \
  --type IncidentWorkflow \
  --task-queue sentinel-incidents \
  --namespace default \
  --workflow-id "incident-retry-test" \
  --input '{"AlertName":"BackendDown","Severity":"critical","Summary":"Retry test","Description":"Loki is down","StartsAt":"2026-10-17T10:00:00Z"}'
```

In the Web UI, open the workflow run. Expected:
- `ActivityTaskScheduled` for `FetchLogs`.
- `ActivityTaskTimedOut` (or `ActivityTaskFailed`) followed by retry attempts.
- After restoring Loki, the activity eventually succeeds and the workflow continues.

```bash
kubectl scale deployment loki -n sentinel --replicas=1   # restore
```

### Rollback / cleanup

```bash
# Terminate a running workflow
temporal workflow terminate \
  --workflow-id "incident-manual-001" \
  --namespace default \
  --reason "manual cleanup"

# Stop the Temporal server (preserves state in the named volume)
docker compose -f temporal/docker-compose.temporal.yml stop

# Destroy the Temporal server AND all workflow history
docker compose -f temporal/docker-compose.temporal.yml down -v
```

### Troubleshooting

**Worker cannot connect to Temporal**

```bash
docker compose -f temporal/docker-compose.temporal.yml ps
```

If `temporal` is not healthy, check its logs:
```bash
docker compose -f temporal/docker-compose.temporal.yml logs temporal | tail -30
```

The most common cause on first start is Postgres not being ready before
Temporal starts. The `depends_on: condition: service_healthy` block handles
this; if it fails, run `docker compose ... restart temporal`.

**`go mod tidy` fails with network errors**

The Go proxy (`proxy.golang.org`) is unreachable. Set the direct proxy:
```bash
GOPROXY=direct go mod tidy
```

**Tests fail with `workflow type not registered`**

The test environment in `SetupTest` must register the workflow before
`ExecuteWorkflow` is called. This is done automatically in the suite because
`env.ExecuteWorkflow` accepts the function directly. If you see this error,
check that you are passing `wf.IncidentWorkflow` (the function), not a string.

**Web UI shows `No workflows found`**

The namespace `default` is created automatically by the auto-setup image.
If the UI shows no namespace, wait 30 seconds for the schema migration to
complete and refresh the page.

---

## Step 8: AI Diagnosis Agent

### Architecture

```
Alertmanager
  --> POST http://agent:8081/webhook
        |
        +--> evidence.Collector
        |       |--> Loki:    last 15 min error logs (redacted)
        |       \--> Prometheus: metrics snapshot
        |
        +--> llm.Provider.Diagnose(evidence)
        |       |--> Ollama  (local, fully free)
        |       \--> Groq    (cloud, free tier)
        |       Validates JSON output. Retries once. Falls back to no_action.
        |
        \--> temporal.ExecuteWorkflow("IncidentWorkflow", input+diagnosis)
                  |
                  +--> Step 7 workflow takes over:
                       notifies Slack/WhatsApp with diagnosis
                       waits for human signal: approve | reject | ignore
```

### LLM provider trade-offs

| | Ollama | Groq |
|---|---|---|
| Cost | Free forever | Free tier: 6,000 req/day |
| Data privacy | Stays on your machine | Sent to Groq's servers |
| Latency | 5-30s (depends on hardware) | < 1s |
| Infrastructure | Needs 4+ GB RAM, Docker | None — API key only |
| Best for | Regulated/air-gapped envs | Fast demos, development |
| Model | llama3.1:8b (or any Ollama model) | llama3-8b-8192 |
| Setup | Pull model once, runs forever | Create account, get key |

The provider is selected at runtime via `LLM_PROVIDER=ollama` or `LLM_PROVIDER=groq`.
No code change is needed to switch.

### Directory layout

```
agent/
  main.go                         Entry point: OTel, Temporal, HTTP server
  go.mod                          go.temporal.io/sdk, go.opentelemetry.io/otel
  Dockerfile                      golang:1.22.4-alpine build + distroless:nonroot
  .env.example

  internal/
    config/config.go              Load all config from environment variables
    redact/
      redact.go                   Strip secrets before sending to LLM
      redact_test.go              8 test cases
    llm/
      provider.go                 Provider interface, Diagnosis struct, BuildPrompt
      validate.go                 ParseAndValidate: JSON schema + enum checks
      validate_test.go            9 test cases
      ollama.go                   Ollama implementation
      groq.go                     Groq implementation
    evidence/
      collector.go                Fetch + redact logs (Loki) and metrics (Prometheus)
    webhook/
      handler.go                  POST /webhook handler

  k8s/
    secret.yaml                   GROQ_API_KEY placeholder
    deployment.yaml               runAsUser 65532, readOnlyRootFilesystem
    service.yaml                  ClusterIP :8081
```

### Allowed actions (fixed allowlist)

The LLM can only propose one of these four values. Any other value fails
validation and triggers the `no_action` fallback:

- `restart_deployment` — delete the pods so the Deployment recreates them
- `scale_deployment` — increase or decrease the replica count
- `rollback_deployment` — roll back to the previous ReplicaSet
- `no_action` — human review required; agent does nothing

The agent does NOT execute any action. It only proposes. Execution is Step 9.

### Setup

#### Option A: Ollama (local, no account needed)

```bash
# Start Ollama (outside the kind cluster, on your host)
docker run -d --name ollama -p 11434:11434 ollama/ollama:0.3.6

# Pull the model (one-time, ~4.7 GB download)
docker exec ollama ollama pull llama3.1:8b

# Verify Ollama is ready
curl http://localhost:11434/api/tags | jq '.models[].name'
# Expected: "llama3.1:8b" in the list
```

#### Option B: Groq (cloud, free tier)

```bash
# 1. Create a free account at https://console.groq.com
# 2. Generate an API key
# 3. Add it to the Kubernetes Secret:
kubectl create secret generic agent-secrets \
  --namespace sentinel \
  --from-literal=GROQ_API_KEY="gsk_your_key_here" \
  --dry-run=client -o yaml | kubectl apply -f -

# 4. Patch the deployment to use Groq
kubectl set env deployment/agent \
  -n sentinel \
  LLM_PROVIDER=groq
```

#### Build and deploy the agent

```bash
# Build the image
docker build -t devops-sentinel-agent:latest ./agent

# Load into kind
kind load docker-image devops-sentinel-agent:latest --name sentinel

# For Ollama: the agent deployment references http://ollama:11434.
# Ollama runs on your host, not in kind. Use a host-gateway trick:
# Patch the deployment to use the host IP reachable from inside kind.
HOST_IP=$(docker inspect sentinel-control-plane \
  --format '{{.NetworkSettings.Networks.kind.Gateway}}')

kubectl set env deployment/agent \
  -n sentinel \
  OLLAMA_BASE_URL="http://${HOST_IP}:11434"

# Apply manifests
kubectl apply -k k8s/overlays/local

# Verify the agent is running
kubectl get pods -n sentinel -l app.kubernetes.io/name=agent
```

#### Run tests (no LLM or cluster needed)

```bash
cd agent
go mod tidy
go test ./... -v -count=1
```

Expected: all tests in `internal/redact` and `internal/llm` pass.

---

### Terminal commands: simulate an alert with curl

This sends an Alertmanager-formatted payload directly to the agent,
bypassing Prometheus and Alertmanager for a quick integration test.

```bash
# Port-forward the agent service
kubectl port-forward -n sentinel svc/agent 8081:8081 &

# Send a simulated BackendDown alert
curl -s -X POST http://localhost:8081/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "version": "4",
    "groupKey": "{}:{alertname=\"BackendDown\"}",
    "status": "firing",
    "receiver": "agent-webhook",
    "groupLabels": {"alertname": "BackendDown"},
    "commonLabels": {
      "alertname": "BackendDown",
      "severity": "critical",
      "service": "devops-sentinel-backend"
    },
    "commonAnnotations": {
      "summary": "Backend deployment has zero ready replicas",
      "description": "All backend pods are unready. The application is unavailable."
    },
    "externalURL": "http://alertmanager:9093",
    "alerts": [
      {
        "status": "firing",
        "labels": {
          "alertname": "BackendDown",
          "severity": "critical",
          "service": "devops-sentinel-backend"
        },
        "annotations": {
          "summary": "Backend deployment has zero ready replicas",
          "description": "All backend pods are unready. The application is unavailable."
        },
        "startsAt": "2026-10-17T10:00:00Z",
        "endsAt": "0001-01-01T00:00:00Z"
      }
    ]
  }'

kill %1
```

Expected: HTTP 200 response. Check agent logs for the diagnosis:

```bash
kubectl logs -n sentinel deployment/agent | tail -20
```

Expected log lines (in order):
1. `"message":"webhook received","status":"firing","alert_count":1`
2. `"message":"evidence collected","alertName":"BackendDown","logLines":<n>`
3. `"message":"diagnosis complete","alertName":"BackendDown","confidence":"...","proposedAction":"..."`
4. `"message":"IncidentWorkflow started","alertName":"BackendDown","workflowID":"incident-BackendDown-..."`

---

### Verification checklist

**Check 1 - Tests pass**
```bash
cd agent && go test ./... -v -count=1
```
Expected: `ok` for `internal/redact` and `internal/llm` packages.

**Check 2 - Agent health endpoint**
```bash
kubectl port-forward -n sentinel svc/agent 8081:8081 &
curl -s http://localhost:8081/health | jq .
kill %1
```
Expected: `{"status":"ok","service":"devops-sentinel-agent"}`

**Check 3 - Simulate alert and receive diagnosis in Slack**

Run the curl simulation above. Wait up to 120 seconds (LLM inference time).

Expected in Slack:
- A message with title `[critical] BackendDown — action required`
- Body contains the LLM-generated summary, root cause, confidence, and proposed action
- Body contains `evidence_used` with real log lines or metric values

**Check 4 - Diagnosis cites real log lines**

```bash
kubectl logs -n sentinel deployment/agent | grep "evidence collected"
```

If `logLines` is greater than 0, the LLM received real log data.
Check the Slack message: `evidence_used` should contain exact lines from
the backend logs, not invented text.

If `logLines` is 0, the backend has not logged any errors recently.
Trigger some by sending invalid requests:
```bash
for i in $(seq 1 5); do
  curl -s -X POST http://localhost/api/items \
    -H "Content-Type: application/json" \
    -d '{"title": ""}' > /dev/null
done
```
Then re-run the alert simulation.

**Check 5 - Temporal workflow was started**

```bash
temporal workflow list --namespace default
```

Expected: a workflow with ID `incident-BackendDown-<timestamp>` in `Running` status.

Open the Temporal Web UI at http://localhost:8088. Click the workflow.
The event history should show the `IncidentWorkflow` waiting for the
`decision` signal after sending the diagnosis notification.

**Check 6 - Approve the proposed action**

```bash
temporal workflow signal \
  --namespace default \
  --workflow-id "incident-BackendDown-<timestamp-from-check-5>" \
  --name "decision" \
  --input '"approve"'
```

Expected: workflow moves to `Completed`. A follow-up Slack message arrives:
`[APPROVED] BackendDown — remediation will proceed`.

**Check 7 - Redaction is applied (no secrets in LLM prompt)**

```bash
kubectl logs -n sentinel deployment/agent | grep -i "token\|password\|secret\|bearer"
```

Expected: no log lines containing real secret values. Redacted values appear
as `[REDACTED]` if a log line happened to contain one.

**Check 8 - Fallback triggers on LLM failure**

Stop Ollama and re-send the alert:
```bash
docker stop ollama

kubectl port-forward -n sentinel svc/agent 8081:8081 &
# send the curl simulation from above
kill %1
```

Expected agent log:
```
"message":"ollama generate failed","error":"..."
"message":"diagnosis complete","proposedAction":"no_action","confidence":"low"
```

The workflow still starts with the fallback diagnosis. No panic, no crash.

```bash
docker start ollama  # restore
```

---

### Rollback / cleanup

```bash
# Remove the agent from the cluster
kubectl delete deployment agent -n sentinel
kubectl delete service agent -n sentinel
kubectl delete secret agent-secrets -n sentinel

# Stop Ollama
docker stop ollama

# Restore Alertmanager to route directly to the notifier (revert Step 8 change)
# Edit observability/alertmanager/configmap.yaml:
#   url: "http://notifier:8080/webhook"
kubectl apply -k k8s/overlays/local
```

### Troubleshooting

**Agent pod is not starting**

```bash
kubectl describe pod -n sentinel -l app.kubernetes.io/name=agent
kubectl logs -n sentinel deployment/agent
```

If the error is `connect to Temporal: connection refused`, the Temporal server
is not running. Start it:
```bash
cd temporal && docker compose -f docker-compose.temporal.yml up -d
```

**LLM returns invalid JSON**

The agent retries once and falls back to `no_action`. Check the logs:
```bash
kubectl logs -n sentinel deployment/agent | grep "invalid"
```

If this happens consistently, the model is too small for structured JSON output.
Switch to a larger model (`llama3-70b-8192` on Groq, or `llama3.1:70b` on Ollama
if you have sufficient RAM) or try Groq which enforces JSON output at the API level.

**Ollama is unreachable from inside kind**

The agent deployment uses `OLLAMA_BASE_URL=http://ollama:11434` by default,
which assumes an Ollama Service exists in the cluster. For a host-side Ollama:
```bash
HOST_IP=$(docker inspect sentinel-control-plane \
  --format '{{.NetworkSettings.Networks.kind.Gateway}}')
kubectl set env deployment/agent -n sentinel \
  OLLAMA_BASE_URL="http://${HOST_IP}:11434"
```

**No log lines in evidence (logLines: 0)**

Loki may not have ingested logs yet after a fresh cluster start. Wait 60
seconds for Promtail to discover and ship the pod logs. Then trigger errors
as described in Check 4 above.

---

## Step 9: Human-Approved Remediation

### Architecture

```
Slack button click  --> POST /approval/slack   (HMAC-SHA256 verified)
WhatsApp reply      --> POST /approval/whatsapp (HMAC-SHA256 verified)
                              |
                              v
                    TemporalDispatcher
                      |- enforce one-decision-per-incident
                      |- send signal to IncidentWorkflow
                      |
                      +--> approved?
                              |
                              v
                         KillSwitch check  (ConfigMap agent-config)
                         RateLimiter check (15 min per service)
                         Executor.Execute  (allowlist: restart/scale/rollback/no_action)
                              |- captureState (replicas, image tag) -> rollback plan
                              |- PATCH Deployment (no cluster-admin)
                              |
                              v
                         HealthChecker.VerifyRecovery
                              |- poll /health every 10s for 3 min
                              |- auto-rollback + notify if not recovered
                              |
                              v
                         audit.Logger.Write  (MongoDB or /var/log/sentinel/audit.jsonl)
```

### Directory layout (Step 9 additions)

```
agent/
  main.go                      CHANGED  TemporalDispatcher, new routes, k8s client
  internal/
    config/config.go            CHANGED  new env vars for approval + mongo
    approval/
      slack.go                  Slack interactive webhook + HMAC-SHA256 verify
      slack_test.go             5 tests
      whatsapp.go               WhatsApp incoming message + verify token
      whatsapp_test.go          6 tests
      export_test.go            test-only exports
    ratelimit/ratelimit.go      In-memory per-service rate limiter (15 min window)
    killswitch/killswitch.go    ConfigMap-backed kill switch (fails closed)
    audit/audit.go              Append-only audit log (MongoDB + file fallback)
    remediation/
      executor.go               Allowlist enforcer + Kubernetes action executor
      executor_test.go          8 tests (allowlist, kill switch, state capture)
      healthcheck.go            Post-action health poller + auto-rollback
  k8s/
    rbac.yaml                   ServiceAccount + Role + RoleBinding (namespace-scoped)
    configmap-killswitch.yaml   agent-config ConfigMap with remediation.enabled
    deployment.yaml             CHANGED  serviceAccountName + new env vars
```

---

### New secrets required

Add these to the existing `agent-secrets` Kubernetes Secret:

```bash
kubectl create secret generic agent-secrets \
  --namespace sentinel \
  --from-literal=GROQ_API_KEY="gsk_..." \
  --from-literal=SLACK_SIGNING_SECRET="your-slack-signing-secret" \
  --from-literal=WHATSAPP_APP_SECRET="your-meta-app-secret" \
  --from-literal=WHATSAPP_VERIFY_TOKEN="any-random-string-you-choose" \
  --dry-run=client -o yaml | kubectl apply -f -
```

GitHub repository secrets needed (for CI notifications):

| Secret name | Where to find it |
|---|---|
| `SLACK_SIGNING_SECRET` | Slack App > Basic Information > App Credentials |
| `WHATSAPP_APP_SECRET` | Meta App > Basic Settings > App Secret |
| `WHATSAPP_VERIFY_TOKEN` | Any string you choose when registering the webhook |

---

### Slack interactive messages setup

Slack button callbacks require a public HTTPS URL for the agent.
For local development, use ngrok or a similar tunnel.

```bash
# Install ngrok: https://ngrok.com/download (free tier sufficient)
ngrok http 8081
# Note the https URL, e.g. https://abc123.ngrok.io
```

In your Slack App settings:
1. Go to "Interactivity & Shortcuts".
2. Toggle "Interactivity" on.
3. Set "Request URL" to: `https://<your-ngrok-url>/approval/slack`
4. Save.

The agent's approval messages (sent via the notify activity) must include
Block Kit action buttons with `action_id` values of the form
`approve_<workflowID>` and `reject_<workflowID>`. The `BuildApprovalBlocks`
function in `internal/approval/slack.go` constructs these.

---

### WhatsApp reply setup

1. In Meta Developer Console > Your App > WhatsApp > Configuration:
   - Set "Webhook URL" to: `https://<your-ngrok-url>/approval/whatsapp`
   - Set "Verify token" to the same value as `WHATSAPP_VERIFY_TOKEN`.
   - Click "Verify and Save".
   - Subscribe to the "messages" webhook field.

2. To approve an incident, send this WhatsApp message to the test number:
   ```
   approve incident-BackendDown-1726560000
   ```
   Or to reject:
   ```
   reject incident-BackendDown-1726560000
   ```
   The workflow ID is included in the original alert notification.

---

### Kill switch

To disable all automated remediation immediately (without restarting the agent):

```bash
kubectl patch configmap agent-config -n sentinel \
  --type merge \
  -p '{"data":{"remediation.enabled":"false"}}'
```

To re-enable:

```bash
kubectl patch configmap agent-config -n sentinel \
  --type merge \
  -p '{"data":{"remediation.enabled":"true"}}'
```

The agent reads this ConfigMap on every remediation attempt. Changes take
effect within seconds. If the ConfigMap is missing or the key is absent,
the kill switch defaults to disabled (no remediation). This is fail-closed.

---

### Deploy

```bash
# Build and load the updated agent image
docker build -t devops-sentinel-agent:latest ./agent
kind load docker-image devops-sentinel-agent:latest --name sentinel

# Apply all manifests (includes RBAC and ConfigMap)
kubectl apply -k k8s/overlays/local

# Verify the RBAC was created
kubectl get serviceaccount devops-sentinel-agent -n sentinel
kubectl get role devops-sentinel-agent -n sentinel
kubectl get rolebinding devops-sentinel-agent -n sentinel

# Verify the kill switch ConfigMap
kubectl get configmap agent-config -n sentinel -o jsonpath='{.data}'
```

---

### Terminal commands: run tests

```bash
cd agent
go mod tidy
go test ./internal/approval/... -v -count=1
go test ./internal/remediation/... -v -count=1
```

Expected: all tests in `approval` and `remediation` packages pass.

---

### Verification checklist

**Check 1 - Tests pass**
```bash
cd agent && go test ./... -v -count=1
```
Expected: all tests pass including approval and remediation packages.

**Check 2 - RBAC is correct (no cluster-admin)**
```bash
kubectl auth can-i patch deployments \
  --namespace sentinel \
  --as system:serviceaccount:sentinel:devops-sentinel-agent
# Expected: yes

kubectl auth can-i delete deployments \
  --namespace sentinel \
  --as system:serviceaccount:sentinel:devops-sentinel-agent
# Expected: no

kubectl auth can-i get secrets \
  --namespace sentinel \
  --as system:serviceaccount:sentinel:devops-sentinel-agent
# Expected: no
```

**Check 3 - Break the backend deliberately**
```bash
# Scale backend to zero replicas to trigger BackendDown alert
kubectl scale deployment backend -n sentinel --replicas=0

# Wait for Alertmanager to fire the alert (~60 seconds)
kubectl port-forward -n sentinel svc/alertmanager 9093:9093 &
curl -s http://localhost:9093/api/v2/alerts | jq '.[].labels.alertname'
kill %1
# Expected: "BackendDown"
```

**Check 4 - Approve in Slack and confirm pod recovers**

In Slack, click the "Approve" button on the incident message.
(Or use the ngrok URL to simulate: see the curl example below.)

```bash
# Monitor the backend deployment
kubectl get deployment backend -n sentinel -w
```

Expected within 30 seconds:
- Deployment shows `2/2` ready replicas.
- Agent log shows `"message":"executor: action applied successfully"`.
- Slack message updates to: "Decision recorded: APPROVE by <your-username>".

**Check 5 - Confirm audit record written**
```bash
# If MongoDB is running:
kubectl exec -n sentinel mongo-0 -- mongosh sentinel \
  --eval 'db.audit_records.find({},{_id:0}).sort({timestamp:-1}).limit(1).pretty()'

# Expected fields: workflow_id, decision, action, result, approved_by, rollback_plan
```

**Check 6 - Reject a second proposal and confirm nothing changes**
```bash
# Trigger a second alert while the backend is still running
kubectl port-forward -n sentinel svc/agent 8081:8081 &
curl -s -X POST http://localhost:8081/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "version":"4","status":"firing","receiver":"test",
    "groupLabels":{"alertname":"HighErrorRate"},
    "commonLabels":{"alertname":"HighErrorRate","severity":"warning"},
    "commonAnnotations":{"summary":"Error rate above threshold"},
    "externalURL":"","alerts":[{
      "status":"firing",
      "labels":{"alertname":"HighErrorRate","severity":"warning"},
      "annotations":{"summary":"Error rate above threshold"},
      "startsAt":"2026-10-17T11:00:00Z","endsAt":"0001-01-01T00:00:00Z"
    }]
  }'
kill %1
```

In Slack, click "Reject" on the new incident message.

Expected:
- No change to the backend deployment (`kubectl get deployment backend -n sentinel`).
- Agent log shows `"message":"dispatching decision to Temporal","decision":"reject"`.
- Audit record shows `"result":"rejected"`.

**Check 7 - Kill switch blocks remediation**
```bash
# Disable remediation
kubectl patch configmap agent-config -n sentinel \
  --type merge -p '{"data":{"remediation.enabled":"false"}}'

# Trigger an alert and approve it
# ... (same curl as Check 3/6)

# In Slack, click Approve.

# Expected agent log:
kubectl logs -n sentinel deployment/agent | grep "kill switch"
# Expected: "kill switch: remediation is DISABLED by ConfigMap"

# Re-enable
kubectl patch configmap agent-config -n sentinel \
  --type merge -p '{"data":{"remediation.enabled":"true"}}'
```

**Check 8 - Rate limit blocks a second approval within 15 minutes**
```bash
# After a successful remediation, trigger another alert immediately
# and approve it. The rate limiter should block it.
kubectl logs -n sentinel deployment/agent | grep "rate limit"
# Expected: "executor: rate limit active"
```

**Check 9 - Auto-rollback triggers when service does not recover**

This requires temporarily breaking the image after applying a restart.

```bash
# Set a bad image before approving a restart
kubectl set image deployment/backend backend=nonexistent:v999 -n sentinel

# Trigger and approve a restart_deployment action
# The executor will restart the deployment but the new pods will
# fail to pull the image. After 3 minutes, auto-rollback fires.

# Watch for the rollback
kubectl logs -n sentinel deployment/agent | grep "rolling back"
# Expected: "executor: rolling back deployment"
```

Restore the correct image manually:
```bash
kubectl set image deployment/backend \
  backend=devops-project-1-backend:latest -n sentinel
```

---

### Rollback / cleanup

```bash
# Re-enable remediation if disabled during testing
kubectl patch configmap agent-config -n sentinel \
  --type merge -p '{"data":{"remediation.enabled":"true"}}'

# Restore backend if scaled to zero
kubectl scale deployment backend -n sentinel --replicas=2

# Remove RBAC (agent loses Kubernetes access but keeps running)
kubectl delete role devops-sentinel-agent -n sentinel
kubectl delete rolebinding devops-sentinel-agent -n sentinel
kubectl delete serviceaccount devops-sentinel-agent -n sentinel
```

### Troubleshooting

**Slack button click returns "dispatch_failed"**

The agent could not reach Temporal. Check:
```bash
kubectl logs -n sentinel deployment/agent | grep "signal workflow"
```
If Temporal is not running, start it:
```bash
cd temporal && docker compose -f docker-compose.temporal.yml up -d
```

**WhatsApp message is not triggering approval**

Verify the message format: `approve incident-BackendDown-<timestamp>`.
The workflow ID must match exactly what was sent in the notification.
Check the agent logs for the parsed decision:
```bash
kubectl logs -n sentinel deployment/agent | grep "whatsapp decision"
```

**Kill switch ConfigMap not found**

The ConfigMap was not applied. Apply it:
```bash
kubectl apply -f agent/k8s/configmap-killswitch.yaml
```

**RBAC error: forbidden**

The ServiceAccount does not have the Role. Verify:
```bash
kubectl describe rolebinding devops-sentinel-agent -n sentinel
```
If missing, re-apply:
```bash
kubectl apply -f agent/k8s/rbac.yaml
```

**Duplicate approval is accepted**

The one-decision-per-incident map is in-memory and resets on agent restart.
If the agent restarted between two approvals, a second approval can get through.
This is a known limitation of the in-memory approach. A persistent store
(MongoDB or Redis) will be added in a later step.

---

## Step 10: Deploy to AWS EC2 with Terraform

Runs the core application (mongo, backend, frontend) on a single EC2
instance. Terraform provisions the instance; the instance itself clones
this repo from GitHub and builds/runs it with `docker compose` on first
boot. A GitHub Actions job then redeploys it automatically on every push
to `main`.

### Architecture

```
 you (local machine)                 AWS                      GitHub
┌──────────────────┐          ┌───────────────────┐     ┌─────────────────┐
│ AWS CLI           │  create  │ EC2 instance        │     │ Actions: CI      │
│ Terraform          ├─────────►│  (Amazon Linux 2023)│     │  build + push    │
│ (terraform/ec2/)   │ security │  - Docker            │     │  images to GHCR  │
│                    │  group   │  - git clone repo ───┼─────┤ Actions: CD      │
└──────────────────┘  + key    │  - docker compose up │◄────┤  SSH in, git pull,│
                       pair     └───────────────────┘ SSH   │  rebuild          │
                                                              └─────────────────┘
```

Nothing in the EC2 instance pulls pre-built images from GHCR for this
path — it builds from source on the box itself, using whatever is on the
`main` branch at boot time (initial provisioning) or at each push
(ongoing, via the `deploy` job).

### What gets installed, and where

**On your own machine** (to run Terraform and manage the instance):

| Tool | Minimum version | Install (Windows, winget) | Install guide |
|---|---|---|---|
| AWS CLI | v2 | `winget install Amazon.AWSCLI` | https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html |
| Terraform | >= 1.8.0 | `winget install Hashicorp.Terraform` | https://developer.hashicorp.com/terraform/install |
| Git | any recent | `winget install Git.Git` | https://git-scm.com/downloads |
| GitHub CLI (optional, for secrets) | any recent | `winget install GitHub.cli` | https://cli.github.com/ |

macOS/Linux: use `brew install awscli terraform git gh` or your distro's
package manager instead of winget.

**On the EC2 instance** — all installed automatically by
`terraform/ec2/user_data.sh.tpl` the first time it boots. You do not run
these yourself; they're listed here so it's clear what the instance ends
up with:

| Package | Purpose |
|---|---|
| `docker` (dnf package) | Runs the containers |
| Docker Compose v2 plugin (`docker-compose-linux-x86_64` binary) | `docker compose` subcommand |
| `git` (dnf package) | Clones this repo onto the instance |
| 1GB swap file at `/swapfile` | Prevents the frontend's `npm`/`vite` build from OOM-killing on a 1GB-RAM `t3.micro` |

Everything the *application* needs (Node 20, npm packages, Go toolchain
for other services) lives inside the Docker images themselves, built from
the Dockerfiles in `backend/` and `frontend/` — none of that is installed
on the host OS.

### Directory layout (Step 10 additions)

```
terraform/
  ec2/
    versions.tf                 Provider requirements (aws, tls)
    variables.tf                aws_region, instance_type, allowed_ssh_cidr,
                                 github_repo_url, mongo_root_username/password
    main.tf                     Key pair, security group, EC2 instance
    outputs.tf                  public_ip, ssh_private_key (sensitive), ssh_command
    user_data.sh.tpl            Boot script: installs Docker, clones repo, runs it
    terraform.tfvars.example    Copy to terraform.tfvars (gitignored) and fill in
    README.md                   Full command reference for this module
deploy/
  iam-policy-ec2-deploy.json   Least-privilege IAM policy for the deploying user
  README.md                    IAM user setup (one-time, console-only step)
```

### Setup

#### 1. Create a least-privilege IAM user (one-time, AWS Console)

Full console steps are in `deploy/README.md` section 1. Summary: create a
user, attach a custom policy made from `deploy/iam-policy-ec2-deploy.json`
(EC2 instance/security-group/key-pair actions only, no broad admin access),
then create an access key for it.

#### 2. Configure the AWS CLI

```bash
aws configure
# AWS Access Key ID:     <from step 1>
# AWS Secret Access Key: <from step 1>
# Default region name:   us-east-1
# Default output format: json
```

Verify:

```bash
aws sts get-caller-identity
```

#### 3. Make sure your GitHub repo is public

The instance clones over plain HTTPS with no credentials. If you need a
private repo instead, see the note in `terraform/ec2/README.md`
prerequisites about passing a deploy token.

#### 4. Configure Terraform variables

```bash
cd terraform/ec2
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars` (this file is gitignored — it never reaches git):

| Variable | What to set |
|---|---|
| `allowed_ssh_cidr` | Your IP + `/32`. Get it with `curl -s https://checkip.amazonaws.com` |
| `github_repo_url` | `https://github.com/<you>/<your-repo>.git` |
| `instance_type` | `t3.micro` unless your account allows larger Free Tier types — AWS rejects non-Free-Tier types on some accounts with `InvalidParameterCombination` |
| `mongo_root_password` | Generate with `openssl rand -hex 24` — **use hex, not base64**; a `/` or `+` in a base64 password breaks the raw MongoDB connection string this project builds |

#### 5. Apply

```bash
terraform init
terraform plan     # review: should show 4 resources to add
terraform apply    # type "yes"
```

This creates a billable AWS resource. Review the plan before confirming.

#### 6. Watch the first boot

```bash
PUBLIC_IP=$(terraform output -raw public_ip)
terraform output -raw ssh_private_key > sentinel-deploy-key.pem
chmod 400 sentinel-deploy-key.pem   # Windows: icacls sentinel-deploy-key.pem /inheritance:r /grant:r "%USERNAME%:R"

# Wait ~10-20s for SSH, then tail the boot log:
ssh -i sentinel-deploy-key.pem ec2-user@$PUBLIC_IP "tail -f /var/log/cloud-init-output.log"
# Ctrl-C once you see "Container devops-sentinel-frontend-1  Started"
```

First boot takes 3-5 minutes: `dnf update` and Docker install (~1-2 min),
then `git clone` and `docker compose up --build` (image builds, ~1-2 min).

#### 7. Verify

```bash
curl -s http://$PUBLIC_IP/health
curl -s -X POST http://$PUBLIC_IP/api/items -H "Content-Type: application/json" -d '{"title":"hello from ec2"}'
curl -s http://$PUBLIC_IP/api/items
```

#### 8. Wire up automatic deploy-on-push (CD)

```bash
cd ../..   # repo root
gh secret set EC2_HOST --body "$(cd terraform/ec2 && terraform output -raw public_ip)"
gh secret set EC2_USER --body "ec2-user"
gh secret set EC2_SSH_KEY < terraform/ec2/sentinel-deploy-key.pem
```

Then, one manual step in the GitHub web UI (this is a repo security
setting and can't be scripted around): go to **Settings → Actions →
General → Workflow permissions** and select **"Read and write
permissions"**. Without this, the CI job's push to GHCR fails with a
permissions error, and the `deploy` job never runs.

From here, every push to `main` that passes CI automatically runs
`git fetch && git reset --hard origin/main && docker compose up -d --build`
on the instance (see the `deploy` job in `.github/workflows/ci.yml`).

### Verification checklist

```bash
# Instance is running
aws ec2 describe-instances --instance-ids "$(cd terraform/ec2 && terraform output -raw instance_id)" \
  --query 'Reservations[0].Instances[0].State.Name' --output text
# Expected: running

# App responds
curl -s http://$PUBLIC_IP/health
# Expected: {"status":"ok","db":"connected",...}

# CD secrets exist (requires gh CLI)
gh secret list
# Expected: EC2_HOST, EC2_USER, EC2_SSH_KEY present

# Latest pipeline run succeeded
gh run list --limit 1
```

### Updating the instance's own configuration

Changing `mongo_root_password`, `instance_type`, or `github_repo_url` in
`terraform.tfvars` and re-running `terraform apply` **replaces the
instance** (`user_data_replace_on_change = true`), which wipes the Mongo
data volume since it's a fresh instance. For routine code updates, don't
touch Terraform — let the CD pipeline (step 8 above) or a manual
`ssh ... "cd /opt/devops-sentinel && git pull && docker compose up -d --build"`
handle it; that updates the code in place without losing data.

### Rollback / cleanup

```bash
# Stop the app, keep the instance
ssh -i terraform/ec2/sentinel-deploy-key.pem ec2-user@$PUBLIC_IP \
  "cd /opt/devops-sentinel && docker compose down"

# Destroy everything Terraform created (instance, key pair, security group)
cd terraform/ec2 && terraform destroy
```

### Troubleshooting

**`terraform apply` fails with `InvalidParameterCombination: The specified
instance type is not eligible for Free Tier`**

Your AWS account has Free Tier restrictions enforced. Set `instance_type`
to `t3.micro` (or `t2.micro`) in `terraform.tfvars` and re-apply.

**Boot log shows `No space left on device` during `dnf update` or the
Docker build**

The default root volume is too small for OS updates plus image builds.
`terraform/ec2/main.tf` already sets `root_block_device { volume_size = 24 }`
— if you lowered it, raise it back and run
`terraform apply -replace="aws_instance.app"` to get a clean instance
rather than resizing in place (resizing the EBS volume alone doesn't grow
the filesystem on an already-booted instance).

**Backend container logs show `"error":"Password contains unescaped
characters"`**

Your `mongo_root_password` contains a character (`/`, `+`, `@`, `:`, `%`)
that isn't valid unescaped in a MongoDB connection URI, which this project
builds by directly interpolating the password. Regenerate with
`openssl rand -hex 24` (hex only, no special characters) and re-apply.

**Docker build fails with `apk add --no-cache curl=<version>` exit code 1**

An Alpine-based Dockerfile pinned an exact package version that has since
aged out of Alpine's repository index. Edit the Dockerfile to drop the
version pin (`apk add --no-cache curl`) and push the fix — the instance
(or the CD pipeline) will pick it up on the next build.

**CI build job fails pushing to GHCR, `deploy` job shows "skipped"**

The repository's default `GITHUB_TOKEN` permissions are read-only. Fix it
in **Settings → Actions → General → Workflow permissions → "Read and
write permissions"**, then re-run the failed jobs from the Actions tab
(or `gh run rerun --failed`).

**GitHub push is rejected with `GH013: Repository rule violations ...
Push cannot contain secrets`**

Secret scanning matched a pattern in a file being pushed — often a
`.env.example` placeholder that looks structurally like a real token
(e.g. `https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXX`).
Check the flagged file; if it's genuinely just a placeholder, replace it
with text that can't match a real secret's shape (e.g.
`REPLACE_WITH_YOUR_SLACK_INCOMING_WEBHOOK_URL`) rather than asking GitHub
to allow it, so future pushes don't hit the same block. If it's an actual
credential, remove it from history before pushing and rotate it.
