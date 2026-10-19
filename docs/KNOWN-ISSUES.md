# Kondisi implementasi — 14 Oktober 2026

Perubahan administrasi, katalog mapel, template Excel, akun dan monitoring telah ditambahkan. Lihat [panduan fitur](../../frontend-ss/docs/ADMIN-2026-10-14.md) dan [QA terbaru](qa/QA-2026-10-14-ADMIN.md).

Batas yang tetap berlaku:

- MongoDB harus replica set untuk transaksi. Tidak ada fallback non-atomik.
- Startup masih melakukan seed akun bootstrap dan 36 butir contoh; existing dipertahankan.
- Riwayat butir lama yang tidak tersimpan tidak dapat direkonstruksi; backfill hanya pemilik/relasi yang diketahui.
- Snapshot hasil final tetap historis dan snapshot baru menyimpan identitas pembentuknya. Daftar peserta operasional mengikuti akses/penugasan saat ini; snapshot lama tanpa identitas pembentuk diberi warning.
- Belum ada audit WCAG/screen reader menyeluruh; layout, fokus/dialog, font minimum dan alur keyboard yang disentuh sudah diperiksa.
- Monitoring membaca MongoDB setiap polling, belum memakai cache Redis.
- Logout browser masih menghapus sesi lokal. Reset, nonaktif, dan perubahan password menginvalidasi versi sesi; endpoint logout global terpisah belum ditambahkan.
- Tidak ada estimasi ICC atau confidence interval; grafik hanya statistik deskriptif yang didefinisikan pada panduan.
- Username existing terkunci. Mengubah username akun lama atau memindahkan jawaban ke orang baru tidak tersedia.
- Proyek selesai tidak mendukung pembukaan penilaian/edit struktural.

[Arsip temuan historis](qa/KNOWN-ISSUES.md) menjelaskan kondisi sebelum perubahan; aturan draft-only di arsip sudah digantikan aturan edit terbaru.
