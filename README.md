# Standard Setting API

Backend Go/Fiber untuk aplikasi Standard Setting. MongoDB menyimpan akun, proyek, butir, putaran/respons, kontak terpisah, evaluasi, batch dampak dan audit; Redis menyimpan refresh session serta counter login atomik. Base path API: `/api/v1`, alamat fallback `http://localhost:8080`; konfigurasi lokal pengguna memakai `http://127.0.0.1:18081`.

## Lokal

```bash
docker compose up -d --wait
cp .env.example .env
set -a
source .env
set +a
go run ./cmd/migrate -command status
go run ./cmd/migrate -command up
go run ./cmd/server
```

Jangan menimpa `.env` yang telah dikonfigurasi. `main.go` membaca environment proses, bukan file `.env` secara otomatis. Bila MongoDB/Redis sudah tersedia, gunakan service tersebut; MongoDB wajib replica set. Compose sekarang menyiapkan single-node rs0. Untuk container lama baca [prosedur tanpa menghapus volume](docs/MIGRATION.md#replica-set-untuk-transaksi-p1). Untuk Redis yang sudah memakai 6379, compose cukup dijalankan untuk service `mongodb`.

Migration `up` menerapkan versi 1–3 secara aditif dan tidak membuat akun. `go run ./cmd/migrate -command seed` menambahkan bootstrap `superadmin` dan 36 butir contoh, tanpa menimpa dokumen existing. Startup server hanya bootstrap admin secara default; butir demo hanya dibuat bila `SEED_DEMO_ITEMS=true` (default false). Flag false tidak menghapus contoh existing. Password awal mengikuti `INITIAL_PASSWORD`, fallback `12345*`; first login wajib menggantinya. Redis tidak memerlukan migration schema.

## Dokumen

- [Indeks dokumentasi BE](docs/README.md)
- [Dokumentasi fitur dan flow API](docs/FITUR-DAN-ALUR-API.md)
- [OpenAPI 3.0.3](docs/openapi.yaml)
- [Struktur aplikasi](docs/STRUKTUR-APLIKASI.md) dan [model data](docs/MODEL-DATA.md)
- [Migration](docs/MIGRATION.md)
- [Kontak, kandidat, revisi, snapshot dan audit](docs/FEATURES-2026-10-19.md)
- [Laporan, dampak dan evaluasi](../docs/features/reporting-impact-evaluation.md)
- [Runbook instalasi/HTTPS/backup/restore](../deployment/OPERATIONS.md)
- [Kondisi implementasi/known issues](docs/KNOWN-ISSUES.md)
- [QA historis MD](docs/qa/QA-2026-09-16.md) dan [PDF](docs/qa/QA-roles-2026-09-16.pdf)
- [README aplikasi](../README.md) dan [panduan FE](../frontend-ss/docs/PANDUAN-FE.md)

## Verifikasi

```bash
go test ./...
go vet ./...
QA_TEST_MONGO_URI='mongodb://localhost:27018/?replicaSet=ssqa&directConnection=true' go test ./internal/service -count=1 -v
MIGRATION_TEST_MONGO_URI=mongodb://localhost:27017 go test ./internal/migration -count=1 -v
```

Integration test membuat lalu menghapus database `standard-setting-migration-test-<timestamp>`, tidak database utama. P1 telah diperbaiki dan diuji pada [retest 13 Oktober](docs/qa/QA-2026-10-13-RETEST.md); ini tidak berarti sertifikasi produksi. Test service memerlukan QA_TEST_MONGO_URI menunjuk replica set QA dan membuat database scratch.

## Pembaruan administrasi

Versi 2 migration menyiapkan katalog dan riwayat; versi 3 menambah default legacy revisi/kontak dan indeks fitur baru tanpa merekonstruksi snapshot final. Lihat [panduan terbaru](../frontend-ss/docs/ADMIN-2026-10-14.md), [QA](docs/qa/QA-2026-10-14-ADMIN.md), dan [cara menjalankan lokal](../README.md). Backend `.env` saat ini menunjuk Mongo `ssqa`/27018 dan frontend 15177; jangan menggantinya dengan contoh rs0/27017 jika ingin memakai database lokal yang sama.
