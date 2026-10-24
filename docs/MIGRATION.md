# Migration MongoDB

Migration CLI di `cmd/migrate` menerapkan versi yang pending secara berurutan. Ledger `schema_migrations` mencatat nama dan `appliedAt` UTC. Versi tersedia:

| Versi | Nama | Perubahan |
| --- | --- | --- |
| 1 | initial_schema | Koleksi dan indeks dasar |
| 2 | admin_catalog_and_history | Katalog mapel, indeks username case-insensitive, riwayat pemilik butir yang diketahui |
| 3 | round_revisions_contacts_audit | Default legacy aditif untuk proyek, kandidat, revisi/respons serta indeks kontak/evaluasi/dampak/audit |

## Replica set untuk transaksi P1

Operasi bisnis multi-document memakai transaksi MongoDB; standalone tidak mendukungnya. Migration tidak mengubah standalone menjadi replica set. Compose lokal menyiapkan single-node `rs0` dan healthcheck menunggu primary; konfigurasi produksi terpisah ada pada [runbook](../../deployment/OPERATIONS.md).

Untuk instalasi lokal baru, jalankan `docker compose up -d --wait mongodb`. Bila container lama perlu diubah, verifikasi volume dan backup terlebih dahulu, pilih waktu penghentian layanan, lalu:

```bash
docker compose config
docker compose up -d --force-recreate --wait mongodb
docker exec standard-setting-mongodb mongosh --quiet --eval 'db.hello().isWritablePrimary'
```

Recreate harus memakai named volume yang sama. Jangan menjalankan `down -v` atau menghapus volume. Backend host untuk Compose ini memakai `mongodb://localhost:27017/?replicaSet=rs0&directConnection=true`. Lingkungan lokal pengguna yang sudah dikonfigurasi memakai `ssqa` pada 27018; jangan menggantinya atau menjalankan recreate pada database aplikasi lain. Gunakan prosedur sesuai instalasi sebenarnya.

## Perintah dan pemisahan seed

- `status` hanya membaca ledger, tidak membuat koleksi atau mengubah dokumen.
- `up` membuat koleksi/indeks yang diperlukan, menjalankan backfill aditif versi pending, lalu mencatat setiap versi yang selesai. Tidak menambahkan akun atau butir demo.
- `seed` adalah opt-in CLI untuk bootstrap `superadmin` dan `admin_bp3` serta 36 butir MAT/BIN/IPA. `$setOnInsert` mempertahankan password/first-login, butir tested dan alokasi existing. Seed tidak mencatat versi migration.

**Startup server hanya memanggil `BootstrapAdmin` secara default.** `SEED_DEMO_ITEMS=false` adalah default; 36 butir contoh hanya dibuat bila flag eksplisit true. Ini tidak menghapus contoh yang sudah ada. `migrate -command seed` tetap menambahkan demo secara eksplisit tanpa bergantung pada flag startup; jangan memakainya pada DB produksi. Password bootstrap mengikuti `INITIAL_PASSWORD`; first login wajib menggantinya. Konfigurasi produksi menolak kredensial development/contoh. Tidak ada reset password existing saat bootstrap/rerun.

Bootstrap menyiapkan dua akun global, `superadmin` dan `admin_bp3` (`admin_super`), hanya jika username belum ada tanpa membedakan kapital. ID/password/status akun existing dipertahankan; akun nonaktif tidak diaktifkan ulang. Untuk memindahkan deployment existing ke VM database/Redis terpisah tanpa menghapus data, lihat [panduan migrasi VM](../../output/deployment/PANDUAN-MIGRASI-MONGODB-REDIS-VM-TERPISAH-2026-10-23.md).

## Menjalankan dari backend-ss

File `.env` tidak dimuat otomatis; gunakan konfigurasi proses yang telah diverifikasi. Jangan menimpa file existing dengan contoh.

```bash
set -a
source .env
set +a
go run ./cmd/migrate -command status
go run ./cmd/migrate -command up
go run ./cmd/migrate -command status
# Hanya bila memang menginginkan data demo pada target yang telah dipilih:
go run ./cmd/migrate -command seed
```

| Flag/env | Default | Keterangan |
| --- | --- | --- |
| `-command` | status | status, up, seed; nilai lain ditolak |
| `-uri` / MONGO_URI | mongodb://localhost:27017 | URI target; kredensial dari environment privat, tidak dicetak |
| `-database` / MONGO_DATABASE | standard-setting | CLI menyebut nama target di output |
| `-timeout` | 30s | Deadline satu eksekusi, positif |
| INITIAL_PASSWORD | 12345* lokal | Hanya dipakai seed/bootstrap, bukan up/status |
| SEED_DEMO_ITEMS | false | Hanya mengontrol butir demo pada startup server |

Contoh target QA eksplisit, memakai DB scratch yang dipilih operator:

```bash
go run ./cmd/migrate -command status -uri 'mongodb://127.0.0.1:27018/?replicaSet=ssqa&directConnection=true' -database standard-setting-migration-qa
go run ./cmd/migrate -command up -uri 'mongodb://127.0.0.1:27018/?replicaSet=ssqa&directConnection=true' -database standard-setting-migration-qa
```

Respons JSON memuat `database` dan array `migrations` berisi version/name/applied/appliedAt. Status applied menyatakan ledger tercatat, bukan audit kesehatan setiap indeks. Jangan mengganti nama atau waktu versi yang sudah diterapkan.

## Versi 2 — katalog dan riwayat administrasi

Versi 2 membuat katalog `subjects`, unique normalizedName dan username case-insensitive. Katalog dibackfill dari mapel butir/proyek existing, dengan MAT/BIN/IPA awal. Riwayat pemilik butir yang diketahui dimasukkan; sejarah yang tidak disimpan tidak diciptakan. Slot personel legacy dihidrasi dari penugasan dan dipersist saat konfigurasi/akun disimpan. `version` proyek dan `authVersion` legacy yang tidak ada dibaca sebagai0. Format refresh lama perlu login ulang setelah upgrade.

## Versi 3 — revisi, kontak dan audit

Backfill hanya mengisi field yang belum ada; nilai existing tidak ditimpa:

| Koleksi | Default legacy |
| --- | --- |
| projects | featureVersion0, collectContactPanelis false, roundingPolicy none, evaluationClosed false, sourceProjectId kosong |
| projects.subjects | candidateDefinition kosong, candidateVersion0; seluruh field subject existing tetap |
| rounds | revision1, excludedPanelists kosong, snapshots kosong |
| submissions | version1, roundRevision1, reviewRevision1, candidateVersion0, note/idempotencyKey kosong |

Timestamps, tanggal periode, jawaban dan agregat final existing dipertahankan. Tidak ada snapshot/roster historis yang direkonstruksi. Snapshot legacy dengan aturan pembulatan kosong dibaca sebagai `none` pada laporan; bukan dihitung ulang dengan aturan proyek baru. Proyek baru memakai featureVersion1 dan default `ceil_1pp`.

Indeks mendukung audit actor/target/project/createdAt, kontak project/subject, evaluasi unik project/user/instrument, dan batch dampak. `PanelistContact.userId` disimpan sebagai Mongo `_id`, sehingga indeks `_id` bawaan menjamin satu kontak per user; jangan membuat unique index pada field `userId` yang tidak ada. Audit tidak memakai TTL penghapusan otomatis. Definisi/kandidat dan roster snapshot baru dijaga aplikasi; migrasi tidak membuat sejarah yang tidak diketahui.

## Existing data, kegagalan dan rollback

Sebelum `up` pada target nyata, simpan backup terenkripsi dan verifikasi URI/database. [Runbook](../../deployment/OPERATIONS.md) menjelaskan konsistensi oplog, restore drill terisolasi, dan pemisahan identitas operator/runtime.

Indeks unique berhenti jika terdapat duplikasi username berbeda kapital, normalizedName, kode/scope bisnis atau evaluasi. Runner tidak menghapus/menggabungkan duplikat. Catat temuan, rekonsiliasi eksplisit, lalu rerun. Jika sebagian operasi berhasil dan operasi berikutnya gagal, hasil sebelumnya tetap ada; versi gagal belum dicatat applied. Backfill/indeks dapat dilanjutkan dengan rerun. Jalankan satu proses migration per DB; runner tidak menyediakan lock deployment lintas instance.

Tidak ada `down`, drop koleksi, reset password atau rollback data otomatis. Gunakan migrasi kompensasi yang ditinjau atau prosedur restore yang disetujui. Jangan menghapus ledger untuk menyembunyikan kegagalan. Kode lama hanya boleh dipakai setelah memeriksa kompatibilitas data baru.

## Pemeriksaan di lingkungan QA

```bash
MIGRATION_TEST_MONGO_URI='mongodb://127.0.0.1:27018/?replicaSet=ssqa&directConnection=true' go test ./internal/migration ./internal/store -count=1 -v
go test ./...
go vet ./...
```

Tes membuat database scratch unik dan membersihkannya; tanpa env Mongo, integration test ditandai skipped. Skenario mencakup status read-only, rerun/ledger, unique username, bootstrap/demo opt-in dan default legacy tanpa merusak hasil final. Perintah ini menjelaskan cara menjalankan pemeriksaan, bukan mengklaim bahwa target operasional telah dimigrasikan atau dipulihkan.

Jika server selection timeout, periksa replica set/port/URI. Untuk `IndexOptionsConflict`/duplicate key, inspect indeks/duplikasi sebelum mengubah data. Untuk ledger tidak sesuai, inspect nama/appliedAt; jangan mengeditnya tanpa prosedur rekonsiliasi. Redis tidak memerlukan migrasi schema; TTL/counter/session dan AOF diatur operasi.
