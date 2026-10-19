# Panduan demo Standard Setting

Kondisi akun, proyek, putaran dan container diverifikasi pada 13 Oktober 2026. Panduan ini memakai **database QA yang sudah tersedia**, bukan database utama `standard-setting` di port 27017. Tidak ada password yang direset atau data utama yang diubah saat menyiapkan panduan ini.

## Akun yang dapat digunakan

Semua akun berikut aktif dan passwordnya telah dicocokkan dengan hash di MongoDB. Akun QA ini sudah melewati ganti password pertama; passwordnya **bukan** `12345*`.

| Peran | Username | Password | Proyek / penggunaan |
| --- | --- | --- | --- |
| Admin Super | `superadmin` | `QA-Retest-20261013` | Semua proyek pada database QA |
| PIC Mapel | `ss-5d53df.mat.pic01` | `QA-Retest-20261013` | SS-5D53DF, Matematika, proyek aktif |
| Panelis | `ss-5d53df.mat.panelis01` | `QA-Retest-20261013` | SS-5D53DF, borang Putaran 2 aktif |
| PIC Mapel | `ss-c5728d.mat.pic01` | `QA-Retest-20261013` | SS-C5728D, proyek selesai |
| Panelis | `ss-c5728d.mat.panelis01` | `QA-Retest-20261013` | SS-C5728D, hasil dan riwayat read-only |
| Panelis | `ss-c5728d.mat.panelis02` | `QA-Retest-20261013` | SS-C5728D, hasil dan riwayat read-only |

Gunakan akun SS-5D53DF untuk demo pengisian, pemantauan dan pergantian putaran. Gunakan akun SS-C5728D untuk demo proyek selesai, statistik hasil butir, riwayat dan akses read-only. Akun Admin Super dapat melihat keduanya.

Pada database **baru** yang belum memiliki superadmin, bootstrap mengikuti `INITIAL_PASSWORD` (fallback `12345*`) dan mewajibkan ganti password. Bootstrap tidak mengganti password akun existing. Akun proyek baru yang dibuat melalui wizard juga mengikuti `INITIAL_PASSWORD`, bukan password QA pada tabel ini. Jangan memakai daftar akun QA di atas jika backend diarahkan ke database utama.

## Service dan alamat demo

| Komponen | Alamat / pengaturan |
| --- | --- |
| Frontend | `http://127.0.0.1:15177` |
| Login | `http://127.0.0.1:15177/login` |
| Backend API | `http://127.0.0.1:18081/api/v1` |
| Health check | `http://127.0.0.1:18081/api/v1/health` |
| MongoDB QA | `127.0.0.1:27018`, replica set `ssqa` |
| Database MongoDB | `standard-setting-retest-20261013` |
| Redis | `127.0.0.1:6379`, logical database `15` |

Backend dan frontend dijalankan di dua terminal terpisah. Biarkan terminal tersebut terbuka. Dependency frontend sudah tersedia pada workspace ini; pada checkout baru jalankan `npm ci` di `frontend-ss` terlebih dahulu.

### 1. MongoDB dan Redis

Pastikan Docker Desktop aktif. Container berikut sudah ada pada mesin ini dan sedang berjalan saat verifikasi. Command ini dapat digunakan kembali jika container berhenti:

```sh
docker start standard-setting-qa-rs redis-local
```

Periksa status tanpa mengubah data:

```sh
docker ps --format '{{.Names}}\t{{.Status}}\t{{.Ports}}'
docker exec standard-setting-qa-rs mongosh --quiet --eval 'db.hello().setName'
docker exec redis-local redis-cli ping
```

Replica set MongoDB QA harus menghasilkan `ssqa`; Redis harus menghasilkan `PONG`. Database utama tetap berada di `localhost:27017`. Tidak perlu menghapus container atau volume untuk menjalankan demo ini.

### 2. Backend — terminal pertama

```sh
cd "/Users/bagushariyadi/Library/Mobile Documents/com~apple~CloudDocs/Fira BP3/backend-ss"
export HTTP_ADDRESS='127.0.0.1:18081'
export MONGO_URI='mongodb://127.0.0.1:27018/?replicaSet=ssqa&directConnection=true'
export MONGO_DATABASE='standard-setting-retest-20261013'
export REDIS_ADDRESS='127.0.0.1:6379'
export REDIS_DATABASE='15'
export REDIS_PASSWORD=''
export JWT_SECRET='standard-setting-local-demo-secret-20261013'
export INITIAL_PASSWORD='12345*'
export CORS_ORIGINS='http://127.0.0.1:15177,http://localhost:15177'
go run ./cmd/server
```

Backend membaca environment proses; file `.env` tidak dimuat otomatis. Export di atas sengaja menunjuk database QA dan Redis DB 15. `INITIAL_PASSWORD` tidak menimpa password existing.

Migration QA sudah diterapkan pada database demo ini. Untuk memeriksa status di terminal dengan environment yang sama:

```sh
go run ./cmd/migrate -command status
```

Jika diperlukan pada database yang belum dimigrasikan, gunakan `go run ./cmd/migrate -command up` setelah memastikan environment menunjuk database yang benar. Jangan menjalankan ulang script retest untuk sekadar menyalakan aplikasi; retest membuat fixture tambahan.

### 3. Frontend — terminal kedua

```sh
cd "/Users/bagushariyadi/Library/Mobile Documents/com~apple~CloudDocs/Fira BP3/frontend-ss"
PUBLIC_API_URL='http://127.0.0.1:18081/api/v1' npm run dev -- --host 127.0.0.1 --port 15177 --strictPort
```

Buka `http://127.0.0.1:15177/login`. Pengunjung tanpa session yang membuka alamat dasar akan diarahkan ke login. `--strictPort` memastikan Vite tidak pindah port tanpa sengaja ketika port yang diminta telah dipakai.

Health check dapat dijalankan dari terminal lain setelah backend aktif:

```sh
curl -sS http://127.0.0.1:18081/api/v1/health
```

Respons yang diharapkan: `{"service":"standard-setting","status":"ok"}` (urutan key boleh berbeda). Health endpoint ini tidak menggantikan pengujian seluruh fitur.

Untuk berhenti, tekan `Ctrl+C` pada masing-masing terminal backend dan frontend. Tidak perlu menghentikan container MongoDB/Redis yang mungkin dipakai aplikasi lain.

## Contoh respons Panelis

Workbook pendamping berada di `outputs/01a04c6f-5d2b-7170-8559-015bc3b1a0fb/contoh-respons-panelis-demo.xlsx` dari root workspace. Isinya tiga respons sintetis sesuai ID butir proyek **SS-5D53DF**, akun `ss-5d53df.mat.panelis01`, **Putaran 2**:

| ID butir | Jenis | Contoh isian |
| --- | --- | --- |
| QA-1791862878943-PG-UI | Pilihan ganda | Nilai kandidat `75` |
| QA-1791862878943-PGK-UI | Pilihan ganda kompleks, max skor 3 | Skor 0–3: `8, 17, 34, 41` |
| QA-1791862878943-ESSAY-UI | Essay, max skor 4 | Skor 0–4: `5, 12, 26, 38, 19` |

Pilihan ganda menggunakan satu angka bulat 0–100. PG kompleks dan essay menggunakan distribusi **100 kandidat**; semua isian berupa angka bulat 0–100 dengan jumlah tepat 100. Nilai nol tetap diisi `0`, jangan dikosongkan. Masukkan `8`, bukan `8%` atau `0,08`. Kolom skor yang tidak tersedia pada butir tetap kosong; misalnya `score4` untuk PG kompleks dengan max skor 3.

Kolom `total` dan `status` di workbook berisi formula pemeriksaan, bukan payload backend. Metadata serta tiga ID butir sesuai database QA saat verifikasi; bila PIC memindahkan putaran, sesuaikan `roundNumber` dengan putaran borang terbaru.

### Batasan upload Excel saat ini

**Upload/import Excel respons Panelis belum diimplementasikan:** halaman Panelis hanya menyediakan isian borang dan penyimpanan per butir. Tidak ada endpoint multipart/batch untuk mengunggah workbook. Karena itu workbook adalah **contoh data**, bukan jaminan format impor yang sudah didukung aplikasi.

Untuk demo sekarang, salin nilai dari Excel ke borang Panelis. Jawaban valid disimpan otomatis setelah jeda input sekitar 700 ms. PIC dan borang Panelis menyinkronkan data tiap 10 detik. Alternatif pengisian teknis adalah endpoint per butir berikut, memakai Bearer token akun **Panelis**:

```http
PUT /api/v1/panelist/submissions/QA-1791862878943-PG-UI
Authorization: Bearer <accessToken-panelis>
Content-Type: application/json

{"roundNumber":2,"answer":{"questionType":"pilihan_ganda","value":75}}
```

```http
PUT /api/v1/panelist/submissions/QA-1791862878943-PGK-UI
Authorization: Bearer <accessToken-panelis>
Content-Type: application/json

{"roundNumber":2,"answer":{"questionType":"pilihan_ganda_kompleks","distribution":[8,17,34,41]}}
```

```http
PUT /api/v1/panelist/submissions/QA-1791862878943-ESSAY-UI
Authorization: Bearer <accessToken-panelis>
Content-Type: application/json

{"roundNumber":2,"answer":{"questionType":"essay","distribution":[5,12,26,38,19]}}
```

Token didapat dari `POST /api/v1/auth/login` dengan payload `{"username":"ss-5d53df.mat.panelis01","password":"QA-Retest-20261013"}` pada `data.accessToken`. `itemId` berasal dari borang/metadata butir, `roundNumber` dari putaran aktif; identitas Panelis, proyek dan mapel ditentukan server dari akun, bukan diambil dari kolom spreadsheet.

## Alur presentasi singkat

1. **Admin Super** — login, buka daftar proyek dan monitoring SS-5D53DF. Tunjukkan konfigurasi mapel, ID butir, PIC, Panelis dan asal proyek pada daftar akun.
2. **Panelis aktif** — login sebagai `ss-5d53df.mat.panelis01`; tunjukkan Putaran 2 serta riwayat Putaran 1. Isi tiga respons contoh, tunggu konfirmasi tersimpan dan progres `3/3`.
3. **PIC aktif** — login sebagai `ss-5d53df.mat.pic01`; lihat progres berubah setelah sinkronisasi, klik ikon mata untuk detail respons. Jika ingin mendemokan finalisasi, pahami bahwa tindakan ini mengubah putaran proyek demo dan wajib melalui konfirmasi dua tahap.
4. **Proyek selesai** — kembali ke Admin Super, buka proyek SS-C5728D dan bank butir yang sudah diuji. Tunjukkan statistik hasil final dan respons setiap Panelis. Akun PIC/Panelis SS-C5728D memperlihatkan akses read-only.

Gunakan profil browser terpisah untuk masing-masing role agar session tidak saling tertimpa. Jika hanya satu profil, logout sebelum berganti akun. Beberapa tab incognito dalam profil incognito yang sama tetap berbagi session.

## Jika tidak bisa login atau service gagal

- Username tidak ditemukan: periksa `MONGO_DATABASE` dan `MONGO_URI`; tabel akun ini hanya berlaku untuk database QA tersebut.
- Password ditolak: password tabel diverifikasi pada 13 Oktober 2026; jika kemudian diganti, gunakan password terbaru. Jangan mengandalkan `INITIAL_PASSWORD` untuk mereset akun existing.
- CORS error: frontend harus memakai port 15177 yang sesuai `CORS_ORIGINS`; setelah mengubah environment, restart backend.
- Port terpakai: periksa proses pemilik port terlebih dahulu; jangan mematikan proses lain tanpa mengetahui pemiliknya. Jika mengubah port frontend, sesuaikan CORS; jika mengubah port backend, sesuaikan `PUBLIC_API_URL`.
- Transaksi MongoDB gagal: pastikan memakai URI replica set QA `ssqa` di port 27018, bukan instance standalone.
- Session lama tidak cocok setelah mengganti backend/database/secret: logout, lalu login kembali dengan akun dari database target.

Kredensial ini hanya untuk demo lokal. Jangan menggunakan secret dan password contoh sebagai konfigurasi produksi.
