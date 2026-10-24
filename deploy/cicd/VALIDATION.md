# Validasi CI/CD — 24 Oktober 2026

## Hasil lokal

- 21 tes Python tooling lulus: isolasi FE/BE, urutan migrasi, kegagalan pull/migrasi/healthcheck, state pending, rollback eksplisit, stale run, lock nyata antara dua instance deployer, validasi input/host, dan bootstrap gagal sebelum mutation ketika lookup ditolak.
- 3 pemeriksaan kebijakan workflow lintas repo lulus. Backend CI menjalankan tes tooling yang tersedia di repo backend.
- `actionlint` 1.7.12: kedua workflow valid. Referensi action diperiksa melalui API resmi GitHub, dipin ke commit SHA, dicatat di `action-refs.json`.
- Python AST valid; dua script Bash dan seluruh 14 blok Bash runbook lolos `bash -n`.
- Frontend: Svelte check 0 error/0 warning; 13 file /75 tes lulus; production build adapter-node berhasil.
- Backend: `go test ./... -count=1 -timeout=10m` lulus. Tes integrasi MongoDB dilewati secara lokal karena variabel URI scratch tidak diisi dan Docker daemon sedang berhenti. Workflow menyiapkan replica set MongoDB sendiri dan mengisi kedua URI integrasi.
- Tracked diff di kedua repo lolos `git diff --check`. Perubahan existing yang tidak terkait dipertahankan.

## Bukti RED → GREEN

Tes deployer pertama gagal karena deploy.py belum tersedia, lalu lulus sesudah implementasi. Pemeriksaan workflow/ignore gagal sebelum workflow dan pengecualian credential dibuat, lalu lulus. Tes validasi bootstrap gagal sebelum script dibuat, lalu lulus. Tes diagnostics command gagal karena CommandFailure belum diimplementasikan, lalu lulus dengan file log berizin 0600 tanpa input token.

## Batas bukti

Gcloud di laptop tidak memiliki akun aktif. Tidak ada resource/IAM GCP dibuat atau diubah, tidak ada SSH ke VM produksi, tidak ada image dipush ke Artifact Registry, dan workflow belum dipush/diaktifkan pada GitHub. Container build dan deployment Docker belum diuji terhadap daemon lokal pada sesi ini. Uji pertama operator setelah bootstrap tetap diperlukan untuk membuktikan IAM, OS Login, IAP, registry, healthcheck, dan kompatibilitas aplikasi produksi.

Review akhir baca saja selesai. Satu temuan Important tentang retry rollback yang berpindah target direproduksi lewat tes gagal, lalu diperbaiki dengan menyimpan target/history pada state pending. Suite akhir 21 tes tooling +3 pemeriksaan workflow lulus; actionlint tetap lulus.

Temuan Minor yang ditunda: fixture bootstrap belum menjalankan skenario sukses penuh/rerun resource existing. Tes sekarang memeriksa input invalid serta missing/denied lookup; keberhasilan bootstrap aktual tetap harus dibuktikan operator. Reviewer tidak menilai IAM propagation, organization policy, SSH operator, build/pull container, kebenaran migrasi database, atau deployment end-to-end karena memerlukan lingkungan cloud terautentikasi.
