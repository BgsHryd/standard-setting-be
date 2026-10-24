# QA implementasi rencana Standard Setting — 20 Oktober 2026

Acuan: rencana implementasi dan mockup 26 halaman versi 17 Oktober 2026. Nomor 1 dikecualikan. Nomor 7 berupa panduan database/pemulihan dengan host kosong; tidak membuat akun admin cadangan. Satu PIC per mapel. Pengujian ini QA internal, bukan UAT client atau deployment produksi.

## Cakupan yang dikerjakan

| Nomor | Hasil implementasi | Bukti utama |
|---|---|---|
| 2 | Definisi kandidat per mapel, versi dan riwayat server; tampil di atas borang | Tes histori/konteks; browser nyata |
| 3 | Feedback snapshot putaran sebelumnya: median, rentang, sebaran anonim, posisi sendiri; nilai individu disembunyikan jika n < 3 | Tes privasi payload; browser nyata dan mock |
| 4, 19 | Ekspor raw dan ringkasan, putaran/revisi/manifest; CSV Indonesia dan standar; teks numerik/formula dilindungi | Tes CSV, arsip dan matriks roster |
| 5 | SD populasi, SD sampel, SE dalam pp, ambang mentah/operasional dan kebijakan pembulatan | Tes rumus dan batas numerik |
| 6 | Audit append-only, pencarian nama/username/ID, reset/ganti password mencabut sesi lama | Tes auth/HTTP/Mongo/Redis; browser audit |
| 7 | Panduan koneksi/edit DB/pemulihan admin yang telah disiapkan | Dokumen pendamping; tidak ada reset pada DB pengguna |
| 8 | Pengecualian panelis per putaran oleh PIC, alasan/aktor/waktu, undo, respons tetap | Tes transaksi dan QA API nyata |
| 9 | Proyeksi diskusi satu butir dari snapshot final, anonim dan tanpa kontak/catatan | Tes scope/privasi; mock browser |
| 10 | Rollback menaikkan revisi; butir perlu ditinjau ulang; antrean versi lama ditolak | Tes regresi rollback dan pembukaan ulang putaran berikutnya |
| 11 | Catatan per butir maksimal 1.000 karakter, hanya dalam tampilan privat PIC/admin | API dan browser nyata: simpan/reload |
| 12 | Toggle Collect Contact Panelis; setelah ganti password wajib minimal satu WA/email valid; pengingat manual PIC | Tes gate428/versi/kontak ganda; mock browser |
| 13 | Template skor Excel strict, preview semua baris, apply transaksional, normalisasi, batch/snapshot dan label kedaluwarsa | Tes keamanan workbook, invalid-row, binding, rollback transaksi dan ambang inklusif |
| 14 | Evaluasi tiga konstruk terpisah 1–5, respons berversi, agregat anonim, buka/tutup | Tes CAS, privasi n < 3 dan role; browser nyata |
| 15 | Salin ke draft baru: struktur/mapel/nama/pengaturan; tanpa butir/akun/periode/kontak/hasil | Tes service dan QA API nyata |
| 16 | Periode WIB, awal inklusif/akhir eksklusif; server menolak simpan/review di luar periode | Tes batas waktu dan payload frontend |
| 17 | IndexedDB sebelum kirim, retry/backoff, ACK-only, isolasi akun/authVersion/putaran/revisi/kandidat | Tes race antrean, mock offline/reload, browser nyata simpan/reload |
| 18 | Bootstrap admin terpisah; seed contoh default false; existing tidak ditimpa | Tes startup, seed idempotent dan migrasi v3 |
| 20 | Docker/Nginx/HTTPS, backup terenkripsi, restore drill terisolasi, limiter Redis dan runbook | Compose config, tes keamanan script, Redis nyata; batas operasional di bawah |

Tabel butir langkah 2, segmented control dua pilihan, font minimum dan gaya aplikasi existing dipertahankan. Tanggal kosong salinan proyek ditampilkan sebagai “Belum diatur”.

## Verifikasi otomatis

Perintah backend dijalankan dari `backend-ss` dengan `QA_TEST_MONGO_URI` menunjuk replica set QA `127.0.0.1:27018/ssqa`. Fixture membuat database unik dan membersihkannya, bukan memakai database aplikasi pengguna.

- `go test -race ./... -count=1`: seluruh package lulus, termasuk transaksi Mongo, migrasi, sesi dan counter Redis nyata.
- `go vet ./...`: lulus tanpa keluaran diagnostik.
- Frontend setelah pembaruan dependency: `npm test` 70/70 lulus, `npm run check` 0 error/0 warning, `npm run build` lulus; hasil pada log `final-frontend-*`.
- OpenAPI: 52 operasi cocok dengan seluruh file route produksi, schema dan tautan lokal divalidasi; checker discovery memiliki dua tes regresi.
- Script backup/restore: dua tes perilaku dan pemeriksaan sintaks shell lulus. Compose produksi lolos `docker compose ... config --quiet`.

Log mentah berada di `tmp/implementation-20261019/`: `final-go-race.log`, `final-go-vet.log`, `final-frontend-test.log`, `final-frontend-build.log`, `docs-validation-20261020.log`, dan log keamanan/fitur. Folder ini artefak lokal yang diabaikan Git; salin bukti yang diperlukan ke paket serah terima.

## API dan browser

QA API nyata memakai backend port18083, Mongo database khusus `standard-setting-feature-qa-20261020`, dan Redis QA terpisah pada port16389/database1. Sebelas kelompok skenario lulus: login/ganti password akun uji, konfigurasi/aktivasi, gate kontak, simpan/CAS, pengecualian/undo, snapshot/feedback/diskusi, laporan, rollback/review/arsip, evaluasi, salin draft, dan audit/role. Bukti: `qa-api-final.log`, `qa-api-results.json`.

Mock browser mencakup lima kelompok alur: panelis/ACK/offline/reload/revisi/mobile; gate kontak; privasi proyeksi PIC; wizard/tabel/payload WIB; filter audit. Tidak ada page error. Bukti: `frontend-browser-qa.json` dan screenshot. Mock memeriksa interaksi/payload; tidak menggantikan pengujian transaksi backend.

Browser yang terhubung ke backend nyata juga memverifikasi:

- Laporan statistik, arsip revisi 1/2, halaman dampak dan agregat evaluasi tiga panelis.
- Audit menampilkan nama pelaku/target tanpa rahasia.
- Borang menampilkan definisi kandidat dan feedback anonim.
- Input `065` menjadi `65`, catatan tersimpan bersama jawaban, progres1/3 hanya setelah ACK, nilai/catatan tetap ada setelah reload.

Screenshot nyata: `real-admin-audit.png`, `real-panelist-saved.png` di folder bukti lokal. Seluruh nama/kontak/kredensial dalam fixture adalah data QA; tidak mengubah akun atau respons pengguna operasional.

## Review independen dan perbaikan

Review awal menemukan tujuh masalah; seluruhnya diperbaiki dan diverifikasi ulang:

1. Retry request akun A tidak boleh memakai sesi akun B atau generasi password baru.
2. Putaran berikutnya yang dibuka kembali sesudah rollback mendapat revisi baru.
3. Noise floating point tidak boleh menaikkan ambang integral59 menjadi60.
4. Skor58% atau29/50 harus lulus pada ambang58; skor yang benar-benar di bawah tetap gagal.
5. ACK jawaban lama memperbarui versi CAS tanpa menandai draft baru sebagai sudah tersimpan.
6. Ekspor mencakup matriks roster historis × butir, termasuk baris kosong panelis nonaktif/dikecualikan.
7. Edit definisi kandidat sebelum finalisasi tetap menyimpan konteks lama; client tidak dapat menghapus histori.

Review lanjutan read-only menyatakan tujuh temuan terselesaikan, tanpa temuan blocking tersisa pada perbaikan tersebut. Acknowledgement kandidat yang sudah berubah juga ditolak server. Snapshot lama tanpa roster lengkap diberi label dan tidak diisi dari status peserta hari ini.

## Batas verifikasi dan operasi

- Docker image produksi, TLS pada domain client, jadwal backup nyata, restore backup produksi, dan SLA RPO/RTO belum dieksekusi. Base image/tool backup tidak tersedia lokal. [Runbook operasi](../../../deployment/OPERATIONS.md) menjelaskan langkah yang harus dibuktikan operator.
- Kebijakan `ceil_1pp` adalah default usulan rencana, dapat dipilih `none` atau `nearest_1pp`; pemilik metodologi perlu memeriksa pilihan sebelum finalisasi. Snapshot final menyimpan kebijakannya.
- Rentang statistik deskriptif/SE tidak membuktikan validitas batas lulus atau independensi panel; tidak ada ICC/CI otomatis.
- Audit dependency dan pembaruan keamanan dicatat pada `dependency-*.log`/`final-npm-audit.json`. Dua dependency transitif diperbarui dan Vitest menjadi4.1.12. Audit akhir:0critical,0high,0moderate,3low pada rantai cookie/SvelteKit/adapter. Tiga temuan low memerlukan migrasi framework mayor terpisah; tidak memakai force override.
- Nomor host produksi tetap kosong pada panduan DB. Kode tidak dipublikasikan ke GitHub atau dideploy ke GCP dalam pekerjaan ini.

## Mengaktifkan versi baru pada instance lokal utama

Backup sesuai kebijakan terlebih dahulu. Jalankan migrasi aditif v3 dari environment yang benar, lalu restart backend dan frontend seperti [README](../../../README.md). QA menggunakan database terpisah; migrasi pada database operasional tidak dijalankan otomatis dalam pengujian ini. Histori yang memang tidak pernah tersimpan pada versi lama tidak dapat direkonstruksi.

## Koreksi UI lanjutan — 20 Oktober 2026

Permintaan koreksi tampilan telah diterapkan: field pencarian audit dan pilihan jenis aktivitas memakai styling form yang sama; target memakai nama proyek/mapel dan nomor putaran; jenis aktivitas/hasil diterjemahkan; ringkasan akun dan bank butir menjadi satu strip empat informasi; fokus login memakai border netral tanpa highlight hijau ganda; panduan ON dan subtitle panduan umum di administrasi/wizard dihapus. Status/error dan alasan penguncian tetap ditampilkan.

Dua regresi backend memeriksa label nama/nomor putaran, cache lookup, identitas akun, dan fallback target yang telah dihapus. Tahap RED tercatat sebelum implementasi. Semua package backend lulus tes regresi penuh; frontend70/70 lulus, check0/0, build berhasil; OpenAPI52 operasi dan150 tautan lokal valid. Bukti: log `ui-refinement-*` pada folder artefak lokal.

Browser nyata mengonfirmasi filter nama+jenis aktivitas menghasilkan baris yang sesuai, target tanpa ID internal, empat sel ringkasan sejajar dalam satu baris setinggi62px, toggle ON tanpa paragraf panduan, serta outline input login `none` dengan border abu-abu dan tanpa box shadow. Login admin QA dengan password yang diinformasikan berhasil, tanpa reset password. Tidak ada konfigurasi proyek uji yang disimpan selama pemeriksaan tampilan.

Screenshot bukti lokal: `ui-audit-refined.png`, `ui-accounts-strip.png`, `ui-items-strip.png`, `ui-project-clean.png`, `ui-login-focus-neutral.png` dalam `tmp/implementation-20261019/`.

## Kode mapel dan review wizard — 20 Oktober 2026

Kode mapel manual diuji melalui handler HTTP dan service dengan Mongo replica set. Kasus mencakup normalisasi huruf besar, kode wajib/sintaks, kode duplikat tanpa membedakan kapital, nama duplikat, alias payload yang bertentangan, impor butir memakai kode/nama, dan penolakan nama/kode ambigu. Tahap RED tersimpan pada `mapel-code-red.log`, tahap GREEN pada `mapel-code-green.log`. Tes seluruh package backend lulus (`mapel-wizard-backend-all.log`). Frontend70/70 lulus (`mapel-wizard-frontend-tests.log`).

Browser nyata memverifikasi field kode/nama, validasi native untuk kode `12 !`, serta penambahan kode lowercase `qasej20` yang tersimpan dan tampil sebagai `QASEJ20` di katalog. Data tersebut hanya dibuat pada database QA bernama di atas; kode lama tidak diubah. Langkah 5 menampilkan ringkasan empat kolom dan dua panel akun/kesiapan. Keempat connector terukur setinggi2px dengan titik tengah sama dengan pusat lingkaran. Tidak ada konfigurasi proyek yang disimpan saat pemeriksaan tampilan.

Screenshot: `mapel-manual-code-form.png` dan `wizard-review-and-lines.png`. Pemeriksaan Svelte terakhir menghasilkan0 error/0 warning; build produksi frontend berhasil (`mapel-wizard-final-build.log`). OpenAPI52 operasi dan150 tautan lokal valid setelah penambahan kontrak kode mapel.

Koreksi jarak connector berikutnya diverifikasi pada browser desktop dan viewport390px: ujung garis berjarak12px dari lingkaran berikutnya; pada mobile kedua ujung berjarak12px dari lingkaran. Ketebalan tetap2px. `npm run check` kembali lulus0 error/0 warning. Bukti: `wizard-connector-gap.png` dan `wizard-connector-gap-mobile.png`. Perubahan hanya CSS, tanpa perubahan data proyek.

## Penyesuaian ukuran teks — 20 Oktober 2026

Kontrol header A−/A+ diuji pada batas12 dan20px, persistensi ukuran, nilai tersimpan tidak valid, serta storage browser yang diblokir. Empat tes baru gagal sebelum implementasi (`font-size-red.log`), lalu seluruh74 tes frontend lulus (`font-size-tests.log`). Svelte check0 error/0 warning dan build produksi berhasil (`font-size-check.log`, `font-size-build.log`).

Browser nyata memverifikasi default12px dengan A− nonaktif; delapan klik A+ mencapai20px dengan A+ nonaktif; sidebar, tabel, label, dan input terukur20px sementara judul24px tetap. Reload memulihkan20px dan perpindahan ke wizard mempertahankan pilihan. Pada desktop1280px dan viewport mobile390px, lebar dokumen tidak melebihi viewport dan kontrol header tetap berada di dalam layar. Preview dikembalikan ke12px setelah QA. Data proyek tidak disimpan atau diubah.

Bukti lokal: `font-size-20-desktop.png`, `font-size-20-mobile.png`, `font-size-20-wizard.png`, serta `font-size-default.png` dalam folder artefak QA yang sama.

## Posisi floating daftar butir

Penyebab overlap: kandidat memakai sticky top78px sedangkan daftar butir memakai top92px tetap, tanpa memperhitungkan tinggi kandidat. Borang kini mengukur tinggi header dan kandidat melalui binding dimensi Svelte. Posisi daftar butir mengikuti keduanya dengan jarak16px dan batas tinggi berdasarkan ruang viewport di atas footer.

QA browser menggunakan frontend15181/backend18084 sementara dan akun Panelis B fixture pada database QA yang sama; sesi browser utama15179 tidak diganti. Setelah halaman digulir, jarak kandidat–daftar terukur16,02px pada font12px dan16,11px pada font20px (selisih subpixel/pembulatan tinggi). Pencarian serta judul daftar tidak tertutup. Pada viewport390px, kedua panel terukur `position:static`, daftar tanpa batas tinggi, dan token tinggi header mengikuti tinggi aktual109px. Tidak ada jawaban atau konfigurasi proyek yang disimpan selama pemeriksaan.

Bukti screenshot: `sticky-nav-12.png` dan `sticky-nav-20.png`. Seluruh74 tes frontend lulus, Svelte check0 error/0 warning, dan build produksi berhasil; log `sticky-nav-tests.log`, `sticky-nav-check.log`, `sticky-nav-build.log`.

## Styling Profil Kontak

Profil tidak lagi memakai kartu login yang diperbesar; judul bagian, jarak form, pesan bantuan, input, dan area tombol mengikuti komponen aplikasi. Browser dengan akun fixture Panelis B memverifikasi input setinggi44px, radius8px, padding10px/12px dan teks14px pada ukuran default. Pada viewport390px dengan pengaturan20px, input terukur20px dan tetap berada di dalam kartu; lebar dokumen sama dengan viewport390px. Tombol menjadi selebar form pada mobile. Tidak ada kontak yang diedit atau disimpan selama pemeriksaan.

Seluruh74 tes frontend lulus, Svelte check0 error/0 warning, build berhasil. Bukti: `contact-style-tests.log`, `contact-style-check.log`, `contact-style-build.log`, `contact-profile-desktop.png`, `contact-profile-mobile-20.png` pada folder artefak lokal.
