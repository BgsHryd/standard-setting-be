# Verifikasi Paket Dokumentasi dan Migration

Dokumen ini adalah catatan verifikasi **12 Oktober 2026** sebelum perbaikan aplikasi. Untuk kondisi sesudah perbaikan P1, lihat [retest 13 Oktober](qa/QA-2026-10-13-RETEST.md); hasil/count di bawah tetap historis.

Tanggal: **12 Oktober 2026**, Asia/Jakarta. Verifikasi ini terpisah dari QA manual historis 16 September 2026. Tidak dilakukan ulang seluruh browser workflow aplikasi pada pekerjaan dokumentasi ini.

## Hasil yang dijalankan

| Pemeriksaan | Hasil dan batas cakupan |
| --- | --- |
| `MIGRATION_TEST_MONGO_URI=mongodb://127.0.0.1:27017 go test ./... -count=1 -v` | Lulus 25 test Go, termasuk 4 integration test migration/CLI dengan MongoDB scratch. Package cmd/service/session belum memiliki test tersendiri. |
| `go vet ./...` | Exit 0, tidak ada temuan vet. |
| `npm test` (FE) | 4 file, 20 test lulus: auth, password, project, scoring. |
| `npm run check` (FE) | 0 error, 0 warning dari svelte-check. |
| `npm run build` (FE) | Build client/server dan adapter-node berhasil. Bukan bukti bug runtime browser sudah diperbaiki. |
| `docker compose config --quiet` | Exit 0; compose valid. Tidak menjalankan ulang service dengan konfigurasi baru/port alternatif pada pekerjaan ini. |
| OpenAPI schema validation | Swagger Parser memvalidasi OpenAPI 3.0.3 dan seluruh reference; 23 operasi cocok dengan method/path pada router. Tidak berarti seluruh operasi bisa dijalankan tanpa bug middleware. |
| Link dokumen lokal | Diperiksa oleh scripts/verify_docs.mjs; semua target file ada. |
| PDF QA | 9 halaman A4; text extraction memuat QA-01 sampai QA-28; halaman dirender PNG dan diperiksa visual, termasuk lebar kolom temuan dan nomor halaman. |

## Bukti migration

Integration test hanya membuat/membersihkan database `standard-setting-migration-test-<timestamp>`. Database utama `standard-setting` dan data QA historis tidak dijadikan target migration/seed baru.

- Status tidak membuat collection.
- Up membuat lima collection bisnis + ledger schema_migrations; index unique username menolak duplikasi.
- Rerun up mempertahankan appliedAt, passwordHash, mustChangePassword, dan status/kepemilikan butir existing.
- CLI status/up/seed berjalan dengan target eksplisit; seed dua kali tetap menghasilkan 36 butir tanpa mengganti hash admin existing atau item tested/alokasi existing.
- Sebelum implementasi runner, tiga integration test pertama gagal dengan pesan migration belum tersedia; setelah implementasi lulus.

## Mengulangi validasi dokumentasi

`@apidevtools/swagger-parser` adalah dependency pemeriksaan dokumen, bukan dependency runtime aplikasi. Instal pada direktori sementara, kemudian jalankan dari backend-ss:

```bash
npm install --prefix tmp/openapi-validator --no-save --package-lock=false @apidevtools/swagger-parser
OPENAPI_VALIDATOR_ROOT=./tmp/openapi-validator/node_modules node scripts/verify_docs.mjs
docker compose config --quiet
```

Script mencocokkan method/path pada grup route yang ada sekarang. Bila menambah grup/prefix baru, sesuaikan pemetaan grup script dan kontrak OpenAPI. Script juga mengecek tautan file Markdown, bukan validitas anchor heading atau kelengkapan logika bisnis payload.

PDF dibangun dari source Markdown QA menggunakan Python dengan reportlab:

```bash
python3 scripts/build_qa_pdf.py
pdftoppm -png docs/qa/QA-roles-2026-09-16.pdf tmp/pdfs/page
```

Buat folder tmp/pdfs terlebih dahulu bila belum ada. Builder memakai Arial terpasang pada macOS bila tersedia agar font tertanam; fallback Helvetica digunakan di environment lain. Periksa kembali hasil render setelah mengubah laporan.

## Status fitur aplikasi pada 12 Oktober 2026

Temuan KI-01 sampai KI-16 tetap terbuka pada [known issues](KNOWN-ISSUES.md). Lulus build/helper test bukan pengganti retest role, scope, persistensi autosave, statistik final, atau aksesibilitas. Pekerjaan ini menghasilkan dokumen dan migration baseline, bukan perbaikan seluruh temuan aplikasi.
