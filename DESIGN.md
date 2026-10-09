# DESIGN.md (automationshutdown)

Internal ops console, satu layar satu keputusan: matikan semua VM yang cocok filter atau tidak.

Dial: ENERGY 1 / RHYTHM 1 / MOTION 1. Alat tenang untuk momen tegang, bukan landing page.

## Keputusan dan alasan (satu baris tiap keputusan)

- Palet netral HeroUI + satu aksen merah `danger` hanya di tombol shutdown dan status force-stop, agar momen destruktif langsung terbaca.
- Tanpa gradient, tanpa glass, tanpa grid dekoratif, agar terlihat seperti alat infra bukan template AI.
- Inter untuk UI (tabular-nums agar kolom VMID stabil) dan JetBrains Mono untuk VMID plus timestamp log, karena log realtime dibaca sebagai data.
- Tabel preview kolom VMID, nama, node, type, tag: hanya field yang menentukan keputusan eksekusi.
- Modal konfirmasi selalu menyebut angka target dan mode dry-run, karena aksi tidak bisa di-undo.
- Toggle light/dark ikut sistem (default system), karena operator berganti shift dan perangkat.
- Empty state jujur ("Tidak ada VM running yang cocok...") plus error state dengan tombol retry, karena preview bergantung Proxmox yang bisa down.
