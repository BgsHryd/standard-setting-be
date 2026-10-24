# Panduan CI/CD GitHub Actions ke GCP

Disiapkan 24 Oktober 2026. Target: push ke **`main`** pada `BgsHryd/standard-setting-FE` atau `BgsHryd/standard-setting-be` menjalankan tes, build di GitHub, push image ke Artifact Registry, lalu deploy service terkait ke **`ss-demo`** melalui IAP. Setiap deployment berhasil setelah container dan endpoint lokal sehat.

FE/BE tetap pada VM aplikasi. MongoDB/Redis memakai VM terpisah dengan endpoint dari `.env.remote`. Pergantian container pada satu VM dapat menimbulkan jeda singkat. Kedua repo bisa berjalan bersamaan di GitHub; satu lock pada VM mengurutkan perubahan aplikasi. Nomor workflow mencegah build lama yang selesai belakangan menggantikan rilis lebih baru.

Dokumen ini mendampingi file workflow yang telah disiapkan lokal. **Otomasi belum aktif sebelum bootstrap GCP, instalasi tool VM, konfigurasi repository, dan push workflow selesai.**

## 1. Pastikan migrasi sebelumnya sudah selesai

Di **VM aplikasi `ss-demo`**, pastikan FE/BE/Caddy sehat dan aplikasi berfungsi dengan database remote. Selesaikan restore, pencocokan manifest MongoDB, dan verifikasi Redis dari panduan migrasi sebelumnya terlebih dahulu.

```bash
(
set -euo pipefail
test "$(hostname -s)" = ss-demo
curl --fail --silent --show-error http://127.0.0.1:8080/api/v1/health
curl --fail --silent --show-error --output /dev/null http://127.0.0.1:3000/login
sudo docker compose \
  --env-file /home/bagushariyadi081/standard-setting/deploy/.env.remote \
  -f /home/bagushariyadi081/standard-setting/deploy/compose.yaml \
  -f /home/bagushariyadi081/standard-setting/deploy/compose.remote-data.yaml \
  ps backend frontend caddy
)
```

Konfigurasi existing harus tetap tersedia di `/home/bagushariyadi081/standard-setting/deploy`: `compose.yaml`, `compose.remote-data.yaml`, `.env.remote`. Tool tidak membaca variabel sesi seperti `SS_OLD_COMPOSE`. Compose harus mendukung overlay `!reset` yang sudah dipakai migrasi sebelumnya (>= 2.24.4). Target image CI adalah `linux/amd64`.

`migrate up` dijalankan sebelum restart BE sementara BE lama masih hidup. Migrasi berikutnya harus kompatibel dengan versi BE sebelumnya. Perubahan schema/API yang tidak kompatibel memerlukan maintenance khusus. Healthcheck bukan pengganti pengujian login, transaksi, dan alur bisnis setelah perubahan besar.

## 2. Kirim paket CI/CD dari laptop ke Cloud Shell

Paket lokal yang disiapkan Codex:

```text
/Users/bagushariyadi/Repository/Standard Setting/output/deployment/ss-github-cicd-20261024.tar.gz
/Users/bagushariyadi/Repository/Standard Setting/output/deployment/ss-github-cicd-20261024.tar.gz.sha256
```

Upload **kedua file** melalui menu Upload Cloud Shell ke direktori home. Paket berisi tool `cicd/` dan kedua workflow di `workflows/`; tidak membawa `.env`, database, private key, atau token. Workflow hanya dipasang ke GitHub setelah setup pada bagian 3–6 selesai.

Di **Cloud Shell**:

```bash
(
set -euo pipefail
umask 077
cd "$HOME"
sha256sum -c ss-github-cicd-20261024.tar.gz.sha256
mkdir -p -m 700 "$HOME/ss-cicd"
tar -xzf ss-github-cicd-20261024.tar.gz -C "$HOME/ss-cicd"
)
```

## 3. Login GitHub dan siapkan GCP satu kali

Setup memakai akun operator Cloud Shell yang bisa membuat Artifact Registry, service account, WIF, mengubah IAM project, firewall, tag dan metadata VM. Hak tersebut hanya dibutuhkan saat setup. Biaya penyimpanan Artifact Registry dan pemakaian runner GitHub mengikuti akun masing-masing.

**Tetap buka sesi SSH operator ke `ss-demo` pada tab lain.** Setup memberi akun operator hak OS Admin Login sebelum mengaktifkan OS Login hanya pada `ss-demo`. OS Login mengganti mekanisme SSH berbasis metadata; pengguna operator lain juga perlu hak OS Login yang sesuai. Setelah setup, uji login kedua sebelum menutup sesi yang masih terbuka. Bila akun berada di luar organisasi GCP, admin organisasi mungkin perlu memberi `roles/compute.osLoginExternalUser` terlebih dahulu. [Dokumentasi OS Login](https://docs.cloud.google.com/compute/docs/oslogin/set-up-oslogin)

Di **Cloud Shell**, pasang GitHub CLI bila belum ada, lalu login dengan akun yang dapat mengatur kedua repository:

```bash
(
set -euo pipefail
if ! command -v gh >/dev/null; then
  sudo apt-get update
  sudo apt-get install -y gh
fi
gh auth login --web --git-protocol https --scopes repo,workflow
)
```

Ambil ID numerik GitHub secara otomatis, kemudian jalankan bootstrap. Jangan isi ID dengan nama repo atau username:

```bash
(
set -euo pipefail
cd "$HOME/ss-cicd"
SS_OWNER_ID="$(gh api users/BgsHryd --jq '.id')"
SS_FRONTEND_ID="$(gh api repos/BgsHryd/standard-setting-FE --jq '.id')"
SS_BACKEND_ID="$(gh api repos/BgsHryd/standard-setting-be --jq '.id')"
test "$(gh api repos/BgsHryd/standard-setting-FE --jq '.owner.id')" = "$SS_OWNER_ID"
test "$(gh api repos/BgsHryd/standard-setting-be --jq '.owner.id')" = "$SS_OWNER_ID"
bash cicd/setup-gcp.sh "$SS_OWNER_ID" "$SS_FRONTEND_ID" "$SS_BACKEND_ID"
)
```

Script menetapkan project `project-795e1b7d-3b06-4c7b-bbe`, mencari zona aktual `ss-demo`, dan memakai region zona itu untuk registry `ss-apps`. Hasil nonrahasia ditulis ke **`$HOME/ss-cicd/gcp-config.json`**. Tidak perlu mempertahankan variabel `export` antara sesi.

Resource/izin yang disiapkan:

- Artifact Registry Docker `ss-apps`; service account `ss-cicd-deployer` mendapat Artifact Registry Writer pada repository tersebut.
- WIF pool `ss-github`, provider `github-main`, dibatasi owner/repository **ID numerik**, branch `main`, dan event `push`/`workflow_dispatch`.
- Workload Identity User pada service account untuk dua repository yang dipilih. Tidak membuat kunci JSON service account.
- Compute Viewer pada project untuk lookup VM, OS Admin Login pada **instance `ss-demo`**, serta IAP Tunnel Resource Accessor dengan kondisi IP privat `ss-demo` dan port 22.
- Bila VM memiliki service account, Service Account User hanya pada account yang terpasang tersebut.
- Firewall `ss-cicd-iap-ssh`: sumber `35.235.240.0/20`, port TCP 22, tag `ss-cicd-ssh` hanya ditambahkan ke VM aplikasi. Rule/kebijakan deny berprioritas lebih tinggi tetap perlu ditangani admin jaringan bila menghalangi IAP.
- Metadata `enable-oslogin=TRUE` hanya pada VM aplikasi. VM database tidak dikelola oleh setup ini.

Setup memeriksa resource yang sudah ada dan dapat dijalankan ulang dengan ID yang sama. Jika perubahan IAM ditolak, script berhenti; jangan menganggap error izin sebagai resource yang belum ada. Propagasi IAM/WIF bisa memerlukan beberapa menit. [WIF untuk pipeline deployment](https://docs.cloud.google.com/iam/docs/workload-identity-federation-with-deployment-pipelines)

## 4. Uji login operator dan kirim tool ke ss-demo

Di **Cloud Shell**, uji koneksi SSH dan `sudo` melalui sesi baru:

```bash
(
set -euo pipefail
cd "$HOME/ss-cicd"
SS_ZONE="$(python3 -c 'import json; print(json.load(open("gcp-config.json"))["variables"]["GCP_VM_ZONE"])')"
gcloud compute ssh ss-demo \
  --project=project-795e1b7d-3b06-4c7b-bbe --zone="$SS_ZONE" \
  --tunnel-through-iap --command='sudo -n true'
)
```

Jika gagal, pertahankan sesi SSH lama dan periksa IAM/OS Login/IAP sebelum lanjut. Jika berhasil, kirim paket dan konfigurasi:

```bash
(
set -euo pipefail
cd "$HOME/ss-cicd"
SS_ZONE="$(python3 -c 'import json; print(json.load(open("gcp-config.json"))["variables"]["GCP_VM_ZONE"])')"
gcloud compute scp \
  "$HOME/ss-github-cicd-20261024.tar.gz" \
  "$HOME/ss-github-cicd-20261024.tar.gz.sha256" \
  "$HOME/ss-cicd/gcp-config.json" ss-demo:~/ \
  --project=project-795e1b7d-3b06-4c7b-bbe --zone="$SS_ZONE" --tunnel-through-iap
)
```

Buka SSH baru ke `ss-demo` melalui Cloud Shell/Console. Nama home OS Login dapat berbeda dari home deployment lama; konfigurasi aplikasi tetap memakai path absolut existing.

## 5. Instal dan adopsi image yang sekarang berjalan

Di **VM aplikasi `ss-demo`**:

```bash
(
set -euo pipefail
test "$(hostname -s)" = ss-demo
umask 077
cd "$HOME"
sha256sum -c ss-github-cicd-20261024.tar.gz.sha256
mkdir -p -m 700 "$HOME/ss-cicd-install"
tar -xzf ss-github-cicd-20261024.tar.gz -C "$HOME/ss-cicd-install"
sudo bash "$HOME/ss-cicd-install/cicd/install-vm.sh" "$HOME/gcp-config.json"
)
```

Installer membuat:

```text
/usr/local/sbin/ss-deploy
/etc/standard-setting-cicd/config.json
/var/lib/standard-setting-cicd/state.json
/var/lib/standard-setting-cicd/images.json
/var/lib/standard-setting-cicd/deploy.lock
```

`init` mensyaratkan FE/BE sudah berjalan dan sehat, menyimpan referensi image baseline, dan memberi tag lokal pada image tersebut. Tahap ini **tidak restart container**. Pengulangan instalasi dengan konfigurasi yang sama mempertahankan state rilis; jangan menghapus state untuk mengatasi deployment gagal.

Password MongoDB, Redis, JWT, dan `INITIAL_PASSWORD` tetap dibaca dari `.env.remote`. CI memakai token registry sementara melalui stdin SSH dan Docker config sementara yang dihapus setelah pull/deploy. [Autentikasi Artifact Registry](https://docs.cloud.google.com/artifact-registry/docs/docker/authentication)

## 6. Isi repository variables GitHub

Kembali ke **Cloud Shell**:

```bash
(
set -euo pipefail
cd "$HOME/ss-cicd"
python3 cicd/configure-github.py gcp-config.json
)
```

Script mengisi **Settings → Secrets and variables → Actions → Variables** pada kedua repo:

| Variable | Isi |
| --- | --- |
| GCP_PROJECT_ID | project-795e1b7d-3b06-4c7b-bbe |
| GCP_VM_NAME | ss-demo |
| GCP_VM_ZONE | Zona aktual hasil lookup |
| GCP_REGISTRY_LOCATION | Region hasil lookup |
| GCP_AR_REPOSITORY | ss-apps |
| GCP_WIF_PROVIDER | Nama lengkap provider hasil bootstrap |
| GCP_SERVICE_ACCOUNT | ss-cicd-deployer pada project ini |

Tidak ada password database atau kunci JSON GCP yang perlu dimasukkan ke GitHub Secrets. GitHub CLI yang dipakai operator tetap memakai autentikasi GitHub normal; jangan menyalin tokennya ke repository.

## 7. Commit dan push workflow dari laptop

Workflow sudah disiapkan pada masing-masing repo. `.gitignore` dan `.dockerignore` mengecualikan `gha-creds-*.json` agar credential WIF sementara tidak ikut Git atau Docker build context. **Masukkan perubahan ignore bersamaan dengan workflow.**

Ada perubahan lokal lain di workspace saat panduan dibuat. Periksa `git status` dan isi staging sebelum commit; stage hanya file CI/CD berikut, tanpa `git add .` atau `git commit -a`. Jika ada staging dari pekerjaan lain, pisahkan terlebih dahulu.

Di **laptop**, repository frontend:

```bash
(
set -euo pipefail
cd '/Users/bagushariyadi/Repository/Standard Setting/frontend-ss'
test "$(git branch --show-current)" = main
git add .github/workflows/deploy-gcp.yml .dockerignore .gitignore README.md
git diff --cached --stat
git commit -m 'ci: build and deploy frontend to GCP on main'
git push origin main
)
```

Repository backend:

```bash
(
set -euo pipefail
cd '/Users/bagushariyadi/Repository/Standard Setting/backend-ss'
test "$(git branch --show-current)" = main
git add .github/workflows/deploy-gcp.yml .dockerignore .gitignore deploy/cicd README.md
git diff --cached --stat
git commit -m 'ci: build and deploy backend to GCP on main'
git push origin main
)
```

Jika branch dilindungi, buat branch/PR dari perubahan CI/CD ini lalu merge ke `main`; merge itulah yang memicu deployment. Jika push ditolak karena remote berubah, selesaikan sinkronisasi Git tanpa membuang perubahan lokal. Jangan mengganti branch ke `master` untuk mengikuti panduan lama.

Setelah bootstrap lengkap, setiap push ke `main` otomatis memicu job **test → deploy**. Job deploy membangun dan mem-push image, lalu memasang digest hasil build. Kedua workflow juga mendukung **Actions → Run workflow**, dengan branch `main`. Tidak memakai approval Environment per rilis.

Tes BE membuat MongoDB replica set sementara pada runner dan mengisi `MIGRATION_TEST_MONGO_URI` serta `QA_TEST_MONGO_URI`. Database produksi tidak menjadi target tes. BE menjalankan migrasi `status` dan `up` saat deployment; tidak menjalankan `seed`.

## 8. Verifikasi rilis pertama dan operasi selanjutnya

Di GitHub, pastikan job test dan deploy hijau. Di **VM `ss-demo`**:

```bash
sudo /usr/local/sbin/ss-deploy status
```

`services.frontend.image` atau `services.backend.image` akan menunjuk Artifact Registry dengan `@sha256:...`. `commit` cocok dengan commit GitHub, `run_number` tercatat, dan `pending` harus `null`.

Setelah adopsi CI/CD, **selalu sertakan `images.json` sebagai overlay terakhir** untuk operasi Compose aplikasi. File remote lama masih berisi tag backend awal; menjalankan `up` tanpa overlay CI/CD dapat mengembalikan image lama. Contoh inspeksi:

```bash
sudo docker compose \
  --env-file /home/bagushariyadi081/standard-setting/deploy/.env.remote \
  -f /home/bagushariyadi081/standard-setting/deploy/compose.yaml \
  -f /home/bagushariyadi081/standard-setting/deploy/compose.remote-data.yaml \
  -f /var/lib/standard-setting-cicd/images.json \
  ps backend frontend caddy
```

Untuk log, gunakan susunan flags yang sama lalu ganti `ps backend frontend caddy` dengan `logs --tail=80 backend` atau `logs --tail=80 frontend`. Hindari mencetak `docker compose config` penuh karena environment mengandung rahasia.

Uji login admin lama/admin_bp3, akses proyek existing, dan transaksi aplikasi melalui domain. Perubahan kontrak API harus kompatibel selama FE dan BE belum selesai dideploy pada versi baru.

## 9. Menangani job gagal dan rollback image

- Job test/build gagal: versi aplikasi yang berjalan tidak diubah.
- Pull image gagal: state rilis sehat tetap dipertahankan; periksa izin Artifact Registry/token/koneksi keluar VM.
- Migrasi BE gagal: BE belum direstart, tetapi schema mungkin sudah berubah sebagian. Periksa penyebabnya sebelum retry atau rollback.
- Recreate/healthcheck gagal: aplikasi mungkin sudah memakai candidate image. `pending` mencatat service dan fase yang belum selesai. Deploy berikutnya dihentikan sampai operator menyelesaikannya.
- Periksa status dan log service pada VM. Detail command terakhir yang gagal disimpan pada `/var/lib/standard-setting-cicd/last-error.json` dengan izin root saja. Baca lokal memakai `sudo less`; jangan unggah file mentah ke chat/repo karena output aplikasi dapat mengandung informasi sensitif.

Rollback FE ke rilis sehat terakhir (atau sebelum rilis terakhir yang berhasil):

```bash
sudo /usr/local/sbin/ss-deploy rollback frontend
```

Untuk BE, terlebih dahulu pastikan schema sekarang masih kompatibel dengan image tujuan. Setelah dipastikan:

```bash
sudo /usr/local/sbin/ss-deploy rollback backend --confirm-schema-compatible
```

Rollback **hanya image service**, tidak membalik schema/data MongoDB. Tool menggunakan image lokal yang disimpan; jangan menjalankan prune yang menghapus baseline/rilis rollback. Bila image tidak tersedia, pulihkan image yang tepat dari registry sebelum rollback. Setelah rollback sehat, `pending` kembali `null`; jalankan ulang workflow yang gagal atau push perbaikan ke `main`.

Nomor run yang sudah berhasil menjadi batas bawah rilis. Memindahkan atau mengganti nama workflow sehingga penghitung run berubah memerlukan peninjauan state; jangan mengedit angka state secara sembarang. Untuk merilis ulang source lama dengan cara biasa, revert melalui commit baru pada `main`.

## 10. Validasi paket dan batasnya

Pemeriksaan lokal mencakup tes perilaku deploy/rollback/locking/stale run, tes validasi setup, pemeriksaan kebijakan workflow, dan actionlint. Tes aplikasi juga dijalankan sesuai catatan validasi. Hasilnya tidak membuktikan IAM, SSH, registry, dan deployment GCP sudah berhasil; keberhasilan live ditentukan dari job pertama dan verifikasi VM.

Kode bersama berada di repo backend pada `deploy/cicd/`. Setelah mengubah `deploy.py`, pasang ulang tool pada VM; workflow tidak otomatis mengganti skrip root tersebut. Perubahan workflow berada di `.github/workflows/deploy-gcp.yml` pada tiap repo. Action dipin ke commit SHA; referensi upstream yang diperiksa tersimpan di `action-refs.json`.

Referensi: [trigger workflow GitHub](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow), [autentikasi GCP action](https://github.com/google-github-actions/auth), [IAP TCP forwarding](https://docs.cloud.google.com/iap/docs/using-tcp-forwarding).
