# Dokumentasi Backend Standard Setting

Dokumen awal disusun pada **12 Oktober 2026**; kontrak endpoint, fitur baru dan migrasi diperbarui **20 Oktober 2026** berdasarkan source `backend-ss` dan `frontend-ss` di workspace. Format kontrak API mengikuti referensi `real-backend-lms/docs/openapi.yaml`, tetapi endpoint dan payload seluruhnya berasal dari kode Standard Setting.

## Cara membaca

1. [Fitur dan alur API](FITUR-DAN-ALUR-API.md): mulai dari kebutuhan pengguna, halaman yang dipakai, lalu panggilan API dan asal data request.
2. [OpenAPI YAML](openapi.yaml): gunakan untuk melihat request/response setiap endpoint atau mengimpor kontrak ke Swagger Editor/Postman. File ini dokumen statis; server belum menyajikan route Swagger.
3. [Struktur aplikasi](STRUKTUR-APLIKASI.md): temukan file yang harus dibaca atau diubah.
4. [Model data](MODEL-DATA.md): pahami relasi collection, index, session dan statistik.
5. [Migration](MIGRATION.md): siapkan schema/index dan bootstrap secara terkontrol.
6. [Known issues](KNOWN-ISSUES.md): bedakan kebutuhan produk, kondisi source saat ini, dan hal yang belum selesai.
7. [Laporan QA historis](qa/QA-2026-09-16.md) dan [PDF](qa/QA-roles-2026-09-16.pdf): hasil pengujian 16 September 2026, bukan retest per 12 Oktober.
8. [Retest P1 dan QA](qa/QA-2026-10-13-RETEST.md): hasil baru dan bukti browser/API, terpisah dari sejarah.
9. [Verifikasi dokumen/migration](VERIFIKASI-DOKUMEN.md): hasil pemeriksaan kode, YAML, link, PDF dan test migration pada pekerjaan ini.

Panduan pengguna tersedia di [docs FE](../../frontend-ss/docs/PANDUAN-FE.md), dengan dokumen terpisah untuk [Admin Super](../../frontend-ss/docs/roles/ADMIN-SUPER.md), [PIC Mapel](../../frontend-ss/docs/roles/PIC-MAPEL.md), dan [Panelis](../../frontend-ss/docs/roles/PANELIS.md).

## Fitur dan operasi terbaru

- [Kontak, kandidat, revisi, snapshot dan audit](FEATURES-2026-10-19.md)
- [Laporan, dampak dan evaluasi](../../docs/features/reporting-impact-evaluation.md)
- [Instalasi, HTTPS, backup dan restore drill](../../deployment/OPERATIONS.md)

OpenAPI mencakup route helper core/reporting/security serta schema bersama. `GET /me` memakai UserView dan `contactRequired` terkini. Migration versi 3 aditif; startup demo default false.

## Konvensi

- Endpoint pada prose dan tabel relatif terhadap `http://localhost:8080/api/v1`.
- Halaman FE relatif terhadap `http://localhost:5176`.
- `id` proyek/item/panelis di path adalah ID database, bukan nama tampilan.
- Payload JSON memakai camelCase. Dokumen MongoDB memakai `_id` sementara respons API memakai `id`.
- Tanggal request proyek berbentuk RFC3339/ISO8601, bukan string `YYYY-MM-DD` langsung.
- Semua data bisnis berada di database `standard-setting`. Nama collection akun aktual adalah **`users`** (plural).
- FE menampilkan kegagalan nyata; tidak memakai fallback demo atau sukses semu.
- MongoDB replica set wajib untuk transaksi; port lokal tetap 27017.
- Mermaid di Markdown dapat dirender GitHub/GitLab/editor yang mendukung Mermaid.
