# automationshutdown

Tombol merah web untuk mematikan semua VM Proxmox secara berurutan, dengan log realtime via WebSocket. Backend Go satu binary (juga serve frontend), frontend Vite + React + HeroUI.

## Cara kerja

1. Buka web, login dengan `WEB_PASSWORD`.
2. Satu tombol shutdown besar. Klik, muncul konfirmasi berisi jumlah VM running yang siap dieksekusi (sudah difilter tag `TARGET_TAGS` dan `EXCLUDE_VMIDS`).
3. Klik Yes, backend menyebar perintah `shutdown` ke semua target (jeda 2 detik antar VM, `INTER_VM_DELAY_SEC`), lalu memantau semuanya paralel: tiap VM ditunggu sampai mati; kalau lewat `FORCE_AFTER_SEC` (300) masih hidup, dikirim `stop` paksa (`overrule-shutdown=1`) sampai statusnya mati.
4. Semua langkah di-stream ke browser via WebSocket, termasuk paksa-stop.

## ENV

Lihat `.env.example`. Yang penting:

| Key | Arti |
|---|---|
| `PVE_HOSTS` | Satu atau banyak host, koma, dicoba urut (failover) |
| `PVE_VERIFY_SSL` | `false` untuk self-signed homelab |
| `PVE_TOKEN` | `user@realm!id=secret`, butuh `VM.PowerMgmt` di `/vms` + baca cluster |
| `TARGET_TAGS` | Kosong = semua VM; isi koma = yang match salah satu (OR) |
| `EXCLUDE_VMIDS` | VMID yang tidak boleh dimatikan, koma |
| `DRY_RUN` | `true` = log saja tanpa aksi |
| `WEB_PASSWORD` | Password login WebUI (wajib) |

## Jalan lokal

```bash
cp .env.example .env   # isi dulu
go run .               # backend :8080
```

Dev frontend (Go jadi proxy, satu port):

```bash
FRONTEND_MODE=proxy go run .   # :8080
cd frontend && npm install && npm run dev   # :5173
```

Buka `http://localhost:8080`.

## Docker

```bash
docker build -t automationshutdown .
docker run -p 8080:8080 --env-file .env automationshutdown
```

## K3s

```bash
kubectl create secret generic automationshutdown-secret \
  --from-literal=pve-token='user@pve!id=secret' \
  --from-literal=web-password='ganti-ini'
kubectl apply -f k8s/app.yaml
```

Sesuaikan host di Ingress dan `PVE_HOSTS` di ConfigMap.

## API

- `POST /api/login {password}`, `POST /api/logout`, `GET /api/me`
- `GET /api/vms/preview` -> `{total_running, targets[], tags_filter, dry_run}`
- `POST /api/shutdown-all` -> `{job_id, total}` (409 kalau ada job jalan)
- `GET /api/jobs/{id}`, `DELETE /api/jobs/{id}` (cancel)
- `WS /ws?jobId=` -> event `{ts, job_id, vmid, node, type, event, message}`
- `GET /healthz`
