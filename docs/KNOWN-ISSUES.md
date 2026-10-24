# Batas implementasi — 20 Oktober 2026

Panduan administrasi awal ada pada [perubahan 14 Oktober](../../frontend-ss/docs/ADMIN-2026-10-14.md). Fitur berikutnya dijelaskan pada [kontak/revisi/snapshot/audit](FEATURES-2026-10-19.md), [laporan/dampak/evaluasi](../../docs/features/reporting-impact-evaluation.md), dan [runbook operasi](../../deployment/OPERATIONS.md). Laporan QA historis tetap berlabel tanggal; dokumen ini bukan klaim seluruh pengujian/deployment terbaru selesai.

Batas yang tetap berlaku:

- MongoDB harus replica set untuk transaksi. Tidak ada fallback non-atomik.
- Startup memisahkan bootstrap admin dari demo. `SEED_DEMO_ITEMS=false` secara default; true/CLI seed hanya opt-in. Flag false tidak menghapus 36 contoh existing. Inventaris/pembersihan memerlukan tindakan terpisah.
- Riwayat butir lama yang tidak tersimpan tidak dapat direkonstruksi; backfill hanya pemilik/relasi yang diketahui.
- Snapshot final baru menyimpan revisi, roster/status saat finalisasi, konteks kandidat dan aturan. Snapshot legacy tanpa roster lengkap tetap diberi warning/rosterComplete=false; status historis tidak diambil dari akun hari ini.
- Belum ada audit WCAG/screen reader menyeluruh; layout, fokus/dialog, font minimum dan alur keyboard yang disentuh sudah diperiksa.
- Monitoring membaca MongoDB setiap polling, belum memakai cache Redis.
- Logout browser masih menghapus sesi lokal. Reset, nonaktif, dan perubahan password menginvalidasi versi sesi; endpoint logout global terpisah belum ditambahkan.
- Tidak ada estimasi ICC atau confidence interval; grafik hanya statistik deskriptif yang didefinisikan pada panduan.
- Username existing terkunci. Mengubah username akun lama atau memindahkan jawaban ke orang baru tidak tersedia.
- Proyek selesai tidak mendukung pembukaan penilaian/edit struktural.

[Arsip temuan historis](qa/KNOWN-ISSUES.md) menjelaskan kondisi sebelum perubahan; aturan draft-only di arsip sudah digantikan aturan edit terbaru.

Migration 3 tidak memperbaiki provenance/tanggal legacy yang tidak diketahui. Retensi audit dan backup ditetapkan operasi; aplikasi tidak menyediakan delete audit atau melihat password pribadi. Target RPO 24 jam/RTO 4 jam dan restore produksi belum menjadi SLA tanpa bukti drill operator. Docker/TLS/cloud dan pengiriman pengingat WhatsApp tidak dijalankan otomatis.


Audit dependency 20 Oktober2026: setelah patch devalue/source-map-js dan pembaruan Vitest4.1.12, tersisa3low pada cookie dan rantai SvelteKit2/adapter5. Tidak ada high/critical/moderate pada audit akhir. Penyelesaian rantai low memerlukan migrasi framework mayor terpisah. Lihat [QA implementasi](qa/QA-2026-10-20-IMPLEMENTASI.md).
