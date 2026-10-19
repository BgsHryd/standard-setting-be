# Migration MongoDB

## Replica set untuk transaksi P1

Mulai perbaikan 13 Oktober 2026, operasi bisnis multi-document memakai transaksi. MongoDB standalone tidak mendukungnya; migration baseline masih dapat dijalankan tetapi **tidak** mengubah mongod menjadi replica set. Port tetap `localhost:27017`.

Untuk instalasi Compose baru, `docker compose up -d --wait mongodb` menjalankan single-node `rs0` dan healthcheck menginisialisasi serta menunggu primary. Bila Redis lokal sudah tersedia pada 6379, tidak perlu membuat container Redis kedua.

Untuk container Compose lama, lakukan pada waktu layanan boleh berhenti sementara dan sesudah backup. Pastikan `docker inspect standard-setting-mongodb` menunjukkan volume data aplikasi yang benar, lalu:

```bash
docker compose config
docker compose up -d --force-recreate --wait mongodb
docker exec standard-setting-mongodb mongosh --quiet --eval 'db.hello().isWritablePrimary'
```

Recreate memakai named volume yang sama; **jangan** jalankan `down -v` atau hapus volume. Pastikan output terakhir `true`. Backend berjalan dari host memakai URI `mongodb://localhost:27017/?replicaSet=rs0&directConnection=true`. Bila MongoDB Anda bukan container Compose ini, gunakan prosedur replica set yang sesuai instalasinya; jangan menjalankan recreate pada database milik aplikasi lain.

Retest memakai container `standard-setting-qa-rs` terpisah pada 27018/database `standard-setting-retest-20261013`; container utama 27017 tidak direstart. Data lama tested tanpa `finalResult` tidak diberi snapshot otomatis karena putaran/validitas historis harus direkonsiliasi oleh pemilik data. API memberi warning dan statistik null.

Migration yang tersedia adalah **baseline versi 1 `initial_schema`** dan **versi 2 `admin_catalog_and_history`**, sesuai model dan index repository saat ini. Executable berada di `cmd/migrate/main.go`; implementasi di `internal/migration/migration.go`; index bersama startup di `internal/store/indexes.go`.

## Apa yang dilakukan

- `status`: membaca ledger dan menampilkan baseline pending/applied. Tidak membuat collection atau mengubah data.
- `up`: membuat collection users, projects, items, rounds, submissions jika belum ada; memastikan delapan index bisnis; mencatat sukses di schema_migrations. Aman diulang untuk baseline yang sudah diterapkan.
- `seed`: bootstrap `superadmin` dan 36 butir contoh MAT/BIN/IPA, sama dengan Seed saat startup server. Menggunakan `$setOnInsert` sehingga password, flag first-login, item tested/alokasi dan data existing tidak ditimpa. Seed **tidak** mencatat versi migration.

Migration `up` tidak menambah akun/data contoh, tidak melakukan import dari sistem lain, tidak mengubah field existing, dan tidak memperbaiki known issues aplikasi. Database utama tidak otomatis dimigrasikan oleh pekerjaan pembuatan dokumen ini.

## Menjalankan dari backend-ss

```bash
# Pilih environment proses; file .env tidak dimuat otomatis.
set -a
source .env
set +a

# Read-only; ini juga default bila -command tidak ditulis.
go run ./cmd/migrate -command status

# Buat baseline dan catat di ledger.
go run ./cmd/migrate -command up

# Opsional untuk instalasi/demo lokal.
go run ./cmd/migrate -command seed
```

Username bootstrap: `superadmin`; password berasal dari environment INITIAL_PASSWORD (fallback lokal `12345*`). Seed tidak menampilkan password/hash/token di output. Startup `cmd/server` saat ini masih selalu melakukan seed, meskipun operator hanya menjalankan migration up. Perubahan agar seed demo dapat dinonaktifkan merupakan pekerjaan aplikasi terpisah.

Contoh target database QA yang eksplisit:

```bash
go run ./cmd/migrate -command status -uri mongodb://localhost:27017 -database standard-setting-qa
go run ./cmd/migrate -command up -uri mongodb://localhost:27017 -database standard-setting-qa
```

| Flag/env | Default | Keterangan |
| --- | --- | --- |
| `-command` | status | status, up, seed. Nilai lain ditolak sebelum koneksi. |
| `-uri` / MONGO_URI | mongodb://localhost:27017 | Alamat MongoDB. Untuk credential gunakan environment proses. |
| `-database` / MONGO_DATABASE | standard-setting | Database target. CLI selalu mengembalikan nama target di output. |
| `-timeout` | 30s | Deadline satu eksekusi; harus positif. |
| INITIAL_PASSWORD | 12345* | Hanya dipakai seed; tidak digunakan up/status. |

Contoh output setelah up:

```json
{
  "database":"standard-setting",
  "migrations":[{
    "version":1,"name":"initial_schema","applied":true,
    "appliedAt":"2026-10-12T00:00:00Z"
  }]
}
```

Timestamp contoh, bukan waktu migration nyata. Waktu asli dicatat UTC dengan BSON Date.

## Database yang sudah berisi data

Server lama telah membuat collection/index tanpa ledger. Menjalankan up pada database itu menerima collection yang sudah ada dan memastikan index, kemudian mencatat baseline. Dokumen bisnis tidak diubah. Status applied berarti versi tercatat, bukan pemeriksaan ulang kesehatan setiap index; jika index dihapus manual, startup EnsureIndexes membangunnya kembali.

Index unique akan gagal bila database sudah mengandung username/kode proyek/scope round/submission ganda. Migration berhenti dengan exit code 1, dan belum mencatat sukses. Periksa duplikasi, tentukan rekonsiliasi data secara terpisah, lalu jalankan up lagi. Migration tidak menghapus duplikat otomatis.

Setiap operasi baseline idempotent, tetapi tidak berjalan dalam transaksi. Bila sebagian collection/index berhasil lalu operasi berikutnya gagal, bagian yang sudah dibuat tetap ada dan rerun melanjutkan secara aman. Jalankan satu proses migration per database sebagai prosedur operasional; runner ini bukan sistem orchestration/locking untuk deployment multi-instance.

## Menambah versi berikutnya

Tambahkan entry migration dan fungsi up berurutan di package migration, perbarui Status agar membaca semua versi, lalu test fresh dan existing database. Jangan mengganti name/appliedAt versi yang sudah tercatat. Baseline saat ini memanfaatkan index bersama startup; perubahan index substantif harus dibuat sebagai versi berikutnya, bukan diasumsikan akan dijalankan ulang setelah baseline applied.

Tidak ada `down`, drop collection, reset password, atau rollback data otomatis. Untuk rollback perubahan data mendatang gunakan migration kompensasi yang ditinjau atau restore backup. Sebelum up pada data nyata, simpan backup memakai mongodump ke direktori backup yang ditentukan operator dan verifikasi target URI/database.

## Integration test dan troubleshooting

```bash
MIGRATION_TEST_MONGO_URI=mongodb://localhost:27017 go test ./internal/migration -count=1 -v
go test ./...
go vet ./...
```

Test menghasilkan database scratch `standard-setting-migration-test-<timestamp>` dan membersihkannya sesudah test. Test membuktikan status read-only, pembuatan collection/index, unique username, serta rerun yang mempertahankan passwordHash, flag user, kepemilikan/status item dan timestamp ledger. Tanpa env test MongoDB, integration test ditandai skipped, bukan dianggap sudah menjalankan database.

`server selection timeout`: periksa MongoDB/port/URI. `IndexOptionsConflict` atau duplicate-key: periksa index existing/duplikasi data; jangan drop index/data secara buta. `ledger migration ... tidak sesuai`: inspect name/appliedAt pada schema_migrations; jangan mengubah ledger untuk menyembunyikan failure.

Compose sekarang menyediakan healthcheck MongoDB dan Redis, persistent volumes serta port localhost. Bila port 6379 sudah dipakai Redis lain, jalankan hanya service MongoDB dan gunakan REDIS_ADDRESS existing. Override MONGO_PORT/REDIS_PORT compose hanya mengubah port host; sesuaikan MONGO_URI/REDIS_ADDRESS ketika override.


## Versi 2 — katalog dan riwayat administrasi

`up` kini menerapkan versi 1 dan 2 yang masih pending. Versi 2 menambah koleksi `subjects`, index unique nama ternormalisasi, serta index username case-insensitive. Katalog dibackfill dari mapel butir/proyek existing dengan MAT/BIN/IPA awal. Riwayat pemilik butir yang diketahui dimasukkan; data penilaian/password/status existing tidak direset.

Startup juga memastikan katalog/index untuk salinan lokal legacy. Slot personel lama dihidrasi dari penugasan existing, lalu dipersist saat konfigurasi/akun disimpan. `version` konfigurasi legacy dimulai dari 0; `authVersion` legacy dari 0. Refresh token format lama tidak diterima lagi: login ulang setelah upgrade.

Index unique akan menolak data legacy dengan username berbeda kapital atau nama katalog duplikat. Rekonsiliasi data secara eksplisit diperlukan; migration tidak menghapus/menggabungkan data otomatis. Jangan mengubah replica set atau menghapus volume untuk menjalankan migration.

Untuk environment lokal pengguna saat ini, source `.env`, lalu status/up memakai replica set `ssqa` di 27018 dan database `standard-setting-retest-20261013`. QA memakai database scratch terpisah. `status` tetap read-only.
