# Kondisi Implementasi dan Batasan

## Status perbaikan — 13 Oktober 2026

Seluruh P1 pada daftar ini sudah diperbaiki dan mendapat regresi backend/FE serta retest API pada MongoDB QA replica set. [Laporan retest](QA-2026-10-13-RETEST.md) memisahkan bukti baru dari QA historis; [log 84 request](retest-api-2026-10-13.json) tidak menyimpan password/token.

| Temuan | Status sekarang | Verifikasi |
| --- | --- | --- |
| KI-01 | Selesai: middleware admin per-route; API PIC/Panelis dapat diakses sesuai role. | Test role HTTP, retest API, browser PIC/Panelis. |
| KI-02 | Selesai: jawaban diinisialisasi sebelum render; copy eksplisit tanpa structuredClone proxy. | Test hidrasi/copy; browser borang tiga jenis soal dan reload. |
| KI-03 | Selesai: statistik server dari respons lengkap, preview nyata, donut mengikuti status. | Dataset manual 71,25/SD 1,25; browser Admin/PIC. ICC konstanta dihapus, bukan diganti estimasi tak terverifikasi. |
| KI-04 | Selesai: error tidak menghasilkan saved, ID akun semu atau aktivasi sukses lokal. | Test autosave/error/API client; retest browser jaringan dan wizard. |
| KI-07 | Selesai: change-password mengembalikan session baru dan FE mengganti token. | Test HTTP dan API QA-09; request protected sukses tanpa login ulang. |
| KI-08 | Selesai: guard FE per-role; scope PIC/preview diverifikasi terhadap akun dan penugasan. | Test redirect, HTTP scope dan API 403 negatif. |
| KI-10 | Selesai: field server terlindungi, hanya Draft dapat diedit, perubahan multi-document atomik. | Test payload palsu dan gagal alokasi; MongoDB transaksi. |
| KI-11 | Selesai: assignment, jenis/length jawaban, proyek aktif dan respons lengkap diwajibkan. | Test service/API; finalize kosong ditolak, stale round ditolak. |
| KI-12 | Selesai: completion hanya hasil lengkap finalized; snapshot final per item dan panelis unik. | Mean PG final 80, SD 10, n=2, hanya round 2. Legacy tanpa snapshot diberi warning. |
| KI-16 | Selesai: upsert mempertahankan ID existing dan satu respons per scope. | Test stable ID, API resave, browser autosave ulang/reload. |

P2 terkait yang diperbaiki: KI-06 label/count/lastLogin; KI-09 tanggal/timeline/donut; KI-13 akun Draft dinonaktifkan, generate/reopen idempotent; KI-14 array kosong; KI-05 tambah/edit/lihat butir, CSV/laporan/ekspor dan Simpan Draft Panelis terhubung. Role/mapel akun merupakan label penugasan, bukan switch akses; notifikasi contoh dihapus.

KI-15 belum ditutup sebagai audit aksesibilitas menyeluruh: modal/drawer yang disentuh mendapat dialog/focus trap/Escape/restore focus, tetapi tabel div dan verifikasi screen reader/WCAG tetap pekerjaan P2 terpisah. Logout server/revokasi refresh dan cache monitoring juga belum ditambahkan.

### Prasyarat operasional transaksi

MongoDB harus berupa replica set, termasuk lokal single-node. [Compose](../../docker-compose.yml) sudah memuat `rs0` dan healthcheck inisialisasi. Instance utama port 27017 **tidak dibuat ulang** dalam retest ini; QA memakai replica set terisolasi port 27018. MongoDB standalone menolak perubahan atomik dengan pesan konfigurasi yang jelas, tanpa fallback non-atomik. Ikuti [migration/deployment](../MIGRATION.md#replica-set-untuk-transaksi-p1).

## Riwayat temuan sebelum perbaikan

Tabel berikut adalah arsip pembacaan source 12 Oktober 2026 dan observasi QA 16 September 2026, bukan kondisi source setelah perbaikan di atas.

| ID | Prioritas | Kondisi dan dampak | Sumber/bukti |
| --- | --- | --- | --- |
| KI-01 | P1 | Grup admin memakai prefix kosong dengan middleware `requireRoles(admin_super)`. Middleware itu juga berjalan sebelum route `/pic/*` dan `/panelist/*`, sehingga token PIC/Panelis yang valid menerima 403. | Observasi API QA; `internal/httpapi/router.go`, pendaftaran `admin := protected.Group("", ...)`. |
| KI-02 | P1 | Halaman Panelis kosong. Setelah form API gagal, fallback diisi ke `$state`; `structuredClone(existing.answer)` menerima proxy Svelte, memicu `DataCloneError`. Rendering juga pernah membaca `answers[item.id]` sebelum initialized. | Observasi console/browser QA; `frontend-ss/src/routes/panelis/+page.svelte`. |
| KI-03 | P1 | Ringkasan monitoring menampilkan angka tetap 68,42; 67,18; 3,84; 61,20-74,80; ICC 0,87. API Monitoring tidak mengembalikan statistik ini. Preview gagal menggantinya dengan respons demo; kategori PG/PGK/Essay juga angka tetap. | Observasi QA dan source `MonitoringDashboard.svelte`, `PanelistPreviewDrawer.svelte`. |
| KI-04 | P1 | Beberapa kegagalan request dianggap sukses lokal. `saveItem()` menandai saved walau PUT gagal; wizard generate dapat membuat ID semu; aktivasi mengabaikan error. Ini berpotensi membuat pengguna yakin data sudah tersimpan ke server. | Pembacaan source FE; belum diuji autosave karena KI-01/KI-02. |
| KI-05 | P2 | Banyak tombol belum memiliki handler: tambah/edit/lihat butir belum diuji, CSV, laporan, ekspor hasil, buka seluruh detail, Simpan Draft panelis, dropdown role/mapel, notifikasi. | Source dan observasi tombol Lihat ID butir QA. Tidak ada endpoint ekspor. |
| KI-06 | P2 | Akun Admin Super pada daftar akun dilabeli Panelis; jumlah Proyek Asal juga menghitung `undefined`. LastLoginAt belum diperbarui ketika login. | Observasi QA dan source halaman akun/auth service. |
| KI-07 | P1 | Ganti password hanya mengubah `session.user.mustChangePassword` di FE. Access JWT lama masih memuat true hingga refresh/login ulang; request protected dapat menerima 428. | Pembacaan source auth/FE; submit password valid belum dijalankan pada QA historis. |
| KI-08 | P1 | Guard FE memeriksa keberadaan session/first-login, belum membatasi halaman berdasarkan role. `/pic/monitoring` mendukung override projectId/subjectCode dari query tanpa validasi scope PIC. | Pembacaan `domain/auth.js` dan `picMonitoring`. Risiko scope akan relevan setelah KI-01 diperbaiki. |
| KI-09 | P2 | Monitoring putaran 1 tetap menggambar putaran sebelumnya sebagai Putaran 1 Selesai. Tanggal zero Go menjadi 1/1/1; beberapa tanggal demo menghasilkan Invalid Date. Warna segmen donut tetap CSS, tidak dihitung dari status aktual. | Observasi QA; source komponen/CSS. |
| KI-10 | P1 | `SaveProject` menerima hampir seluruh model client dan ReplaceOne; status/createdBy/field kepemilikan belum dilindungi. Save items/alokasi/project bukan transaksi, sehingga kegagalan sebagian dapat meninggalkan alokasi. | Pembacaan service/store; perlu penguatan sebelum penggunaan nyata. |
| KI-11 | P1 | Finalisasi tidak memvalidasi seluruh panelis selesai. SaveSubmission tidak mencocokkan `answer.questionType` dan panjang distribution dengan metadata butir; `ensureRound` bisa membuat putaran tanpa memeriksa status proyek. | Pembacaan service/domain; konfirmasi UI diuji, eksekusi belum diuji. |
| KI-12 | P1 | Hasil butir menghitung seluruh respons dari semua putaran, bukan hanya putaran final. `panelistCount` adalah jumlah respons. Menyelesaikan proyek belum mewajibkan respons lengkap dan belum menyimpan snapshot statistik final. | Source `ItemResult`, `calculateStatistics`, `CompleteProject`; QA proyek kosong selesai dengan statistik nol. |
| KI-13 | P2 | Menghapus Draft tidak menghapus/menonaktifkan akun yang sudah dibuat. Generate akun ulang dapat berbenturan dengan index unique username; rollback kemudian finalize lagi dapat berbenturan index nomor putaran. | Pembacaan store/service; belum diuji eksekusi. |
| KI-14 | P2 | Daftar kosong MongoDB dapat diserialisasi `null`, sementara beberapa komponen FE memakai `.filter`/`.find` tanpa normalisasi. | QA console pernah menampilkan `.filter is not a function`; source store memakai nil slice. |
| KI-15 | P2 | Modal/drawer menggunakan div/aside tanpa dialog role, focus trap dan label beberapa tombol icon/input. Tabel memakai div sehingga semantik tabular terbatas. | Pembacaan DOM/source dan QA visual; belum audit screen reader penuh. |
| KI-16 | P1 | Service membuat ID Submission baru setiap save. ReplaceOne berdasarkan scope akan mencoba mengganti `_id` immutable ketika respons existing diisi ulang; resave berisiko gagal. FE kemudian dapat tetap menandai saved karena KI-04. | Pembacaan service/store; belum dieksekusi pada QA historis karena form terblokir. |

## Arti prioritas

P1: menghambat alur utama, integritas data/keputusan, atau pembatasan akses. P2: menghambat sebagian fungsi, ketepatan informasi, atau aksesibilitas. Label ini merupakan triage engineering, bukan sertifikasi keamanan.

## Perilaku cache dan seed

Redis refresh session benar-benar digunakan. Interface cache monitoring tersedia, tetapi `Service.Monitoring` belum memanggil GetCache/SetCache; polling 10 detik saat ini membaca MongoDB. Startup server selalu memanggil `Seed`, termasuk bank 36 butir contoh. Migration `up` tidak mengubah perilaku startup tersebut.

## Rekomendasi retest historis

Prioritaskan middleware dan render Panelis, lalu autosave dengan verifikasi MongoDB. Sesudah itu uji scope role/mapel/proyek, refresh setelah ganti password, alur finalisasi/rollback beserta history, serta statistik putaran final dengan dataset yang hasilnya diketahui. Temuan ini dicantumkan agar dokumen API tidak membuat janji yang belum dipenuhi source.
