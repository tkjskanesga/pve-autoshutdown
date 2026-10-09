# Automation Shutdown

A web red button that powers off all Proxmox VMs in pipeline fashion, with realtime logs over WebSocket. Single Go binary backend (also serves the frontend), Vite + React + HeroUI frontend.

## How it works

1. Open the web UI, sign in with `WEB_PASSWORD`.
2. One big shutdown button. Click it and a confirmation shows how many running VMs are ready to run (already filtered by the `TARGET_TAGS` and `EXCLUDE_VMIDS` env).
3. Click Yes and the backend fans shutdown commands out to every target (2 seconds apart, `INTER_VM_DELAY_SEC`), then watches all of them in parallel: each VM is waited on until it stops; if one is still alive past `FORCE_AFTER_SEC` (300), it gets a force `stop` (`overrule-shutdown=1`) until its status is dead.
4. Every step streams to the browser over WebSocket, including force-stops.

## ENV

See `.env.example`. The important ones:

| Key | Meaning |
|---|---|
| `PVE_HOSTS` | One or more hosts, comma separated, tried in order (failover) |
| `PVE_VERIFY_SSL` | `false` for self-signed homelab certs |
| `PVE_TOKEN` | `user@realm!id=secret`, needs `VM.PowerMgmt` on `/vms` plus cluster read |
| `TARGET_TAGS` | Empty = all VMs; comma list = only guests matching one of them (OR) |
| `EXCLUDE_VMIDS` | VMIDs that must never be powered off, comma separated |
| `DRY_RUN` | `true` = log only, no action |
| `WEB_PASSWORD` | WebUI login password (required) |

## Proxmox API token

Create the token under Datacenter → Permissions → API Tokens, then grant it rights.
A token created with Privilege Separation starts with zero permissions: API calls
succeed but return empty lists (`{"data":[]}`). Grant at least:

```
pveum acl modify / -token 'USER@REALM!TOKENID' -role PVEAuditor
pveum acl modify /vms -token 'USER@REALM!TOKENID' -role PVEVMAdmin
```

Example:

```
pveum acl modify / -token 'automationshutdown@pve!testing' -role PVEAuditor
pveum acl modify /vms -token 'automationshutdown@pve!testing' -role PVEVMAdmin
```

`PVEAuditor` on `/` allows cluster-wide inventory reads, `PVEVMAdmin` on `/vms`
allows shutdown and stop (`VM.PowerMgmt`). Verify with:

```
curl -sk -H "Authorization: PVEAPIToken=USER@REALM!TOKENID=SECRET" \
  'https://PVE-HOST:8006/api2/json/cluster/resources?type=vm'
```

It must list VMs instead of `{"data":[]}`.

## Run locally

```bash
cp .env.example .env   # fill it in first
go run .               # backend on :8080
```

Frontend dev (Go acts as proxy, single port):

```bash
FRONTEND_MODE=proxy go run .   # :8080
cd frontend && npm install && npm run dev   # :5173
```

Open `http://localhost:8080`.

## Docker

```bash
docker build -t automationshutdown .
docker run -p 8080:8080 --env-file .env automationshutdown
```

## K3s

```bash
kubectl create secret generic automationshutdown-secret \
  --from-literal=pve-token='user@pve!id=secret' \
  --from-literal=web-password='change-me'
kubectl apply -f k8s/app.yaml
```

Adjust the Ingress host and `PVE_HOSTS` in the ConfigMap.

## API

- `POST /api/login {password}`, `POST /api/logout`, `GET /api/me`
- `GET /api/vms/preview` -> `{total_running, targets[], tags_filter, dry_run}`
- `POST /api/shutdown-all` -> `{job_id, total}` (409 while a job runs)
- `GET /api/jobs/{id}`, `DELETE /api/jobs/{id}` (cancel)
- `WS /ws?jobId=` -> events `{ts, job_id, vmid, node, type, event, message}`
- `GET /healthz`
