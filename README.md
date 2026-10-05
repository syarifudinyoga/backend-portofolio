# Portfolio API — Go + PostgreSQL

Backend independen untuk menyajikan data portfolio dari PostgreSQL melalui API read-only.

## Jalankan lokal tanpa container

Semua komponen development berjalan di komputer lokal: PostgreSQL di `localhost:5432`, Go API di `127.0.0.1:3004`, dan Vite di `127.0.0.1:3005`. Tidak perlu container atau homeserver. Pastikan PostgreSQL lokal sudah berjalan dan buat `.env`:

```sh
cp .env.example .env
```

Sesuaikan `POSTGRES_USER` dan `POSTGRES_PASSWORD` di `.env` dengan user PostgreSQL lokal Anda. Buat key admin acak sekali saja, lalu simpan di `.env`:

```sh
printf 'ADMIN_KEY=%s\n' "$(openssl rand -hex 32)" >> .env
```

Jangan bagikan atau commit `.env`. File `.env` dibaca dengan `source` (Go tidak memuatnya sendiri). Dari direktori `backend`, jalankan:

```sh
set -a
. ./.env
set +a
createdb -h "$POSTGRES_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" "$POSTGRES_DB"
go run ./cmd/api
```

Jika database sudah ada, `createdb` mungkin melaporkan bahwa database sudah dibuat; lanjutkan dengan `go run ./cmd/api`. Migration diterapkan otomatis saat API start. Buka `http://127.0.0.1:3004/api/health` untuk cek API.

Di terminal kedua, jalankan frontend:

```sh
cd ../frontend
cp .env.example .env
npm ci
npm run dev
```

Buka `http://127.0.0.1:3005`. Vite meneruskan request `/api` ke Go API lokal, sedangkan Go API yang mengakses PostgreSQL lokal. Halaman edit manual ada di `http://127.0.0.1:3005/myconfig`; masukkan nilai `ADMIN_KEY` dari `.env`.

Key diperiksa backend dan wajib sedikitnya 32 karakter. API publik tetap read-only; browser hanya menyimpan key di `sessionStorage` selama sesi tab. Untuk pemakaian di luar komputer lokal, lindungi koneksi dengan HTTPS dan batasi akses jaringan.

## Migration database

Migration dijalankan otomatis oleh API setiap kali startup. File SQL di folder `migrations/` di-embed ke binary, lalu diterapkan berurutan di dalam transaksi sebelum HTTP server mulai. Tabel `schema_migrations` mencatat nomor versi, nama file, checksum, dan waktu penerapan; advisory lock PostgreSQL mencegah dua instance menerapkan versi bersamaan. Jika migration gagal, API berhenti dan perubahan migration itu di-rollback.

Migration pertama adalah `migrations/001_initial.sql`. Saat menambah atau mengubah schema, **jangan edit migration yang sudah pernah diterapkan**. Tambahkan file bernomor berikutnya, misalnya:

```text
migrations/
├── 001_initial.sql
└── 002_add_profile_bio.sql
```

Contoh isi `002_add_profile_bio.sql`:

```sql
ALTER TABLE profile
    ADD COLUMN bio_short TEXT NOT NULL DEFAULT '';
```

Commit file migration bersama perubahan kode yang membutuhkannya, lalu jalankan ulang API lokal dengan `go run ./cmd/api` (atau build/deploy backend baru bila nanti diperlukan). Pada startup, API membaca versi yang tercatat, memverifikasi checksum migration lama, lalu hanya menjalankan versi baru. Tidak perlu menjalankan `psql` manual atau menghapus database. Nama file harus memakai nomor minimal tiga digit, underscore, nama deskriptif, dan ekstensi `.sql` (`003_add_project_links.sql`).

Periksa riwayat migration di database lokal dengan:

```sh
psql -h localhost -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "SELECT version, name, applied_at FROM schema_migrations ORDER BY version"
```
Database lama yang sudah punya schema dari `001_initial.sql` juga dapat diadopsi: migration awal menggunakan operasi idempotent, kemudian API mencatat versi 001 sebelum melanjutkan.

## Endpoint

- `GET /api/health` — status API dan koneksi database (`204` jika sehat).
- `GET /api/portfolio` — profil, pengalaman, pendidikan, skills, tech stack, dan karya dalam satu JSON.

API sengaja read-only; endpoint admin tidak diekspos. Perbarui konten melalui SQL/migration. Untuk development lokal, frontend mengakses API Go melalui proxy Vite yang ditentukan di `frontend/.env`.

Saat start, terminal Go mencetak status koneksi database (`connecting`, `connected successfully`, atau `connection failed` beserta error PostgreSQL). Bila koneksi gagal, API langsung berhenti dengan exit code non-zero; periksa host, port, database, user, password, dan status layanan PostgreSQL. Saat berhasil, log juga menyebut HTTP address yang sedang dipakai.

## Upload gambar ke MinIO

Foto profil dan gambar preview project bisa diunggah dari `/myconfig` (tombol **UPLOAD**) dan disimpan di MinIO. Isi di `.env`:

```text
MINIO_ENDPOINT=localhost:9000
MINIO_ACCESS_KEY=...
MINIO_SECRET_KEY=...
MINIO_BUCKET=portfolio
MINIO_USE_SSL=false
```

- Bucket dibuat otomatis jika belum ada dan tetap private; gambar disajikan lewat `GET /api/media/images/<nama>` sehingga tidak perlu kebijakan public.
- `POST /api/admin/upload` (Bearer `ADMIN_KEY`, multipart field `file`) menerima JPG, PNG, WebP, atau GIF maksimal 8 MB dan mengembalikan `{"url": "/api/media/images/..."}`.
- Log menampilkan `minio: connecting`, `connected successfully`, atau `connection failed: <error>`. Bila MinIO belum diatur, API tetap jalan dan upload mengembalikan 503; kolom URL manual tetap bisa dipakai.

## Build dan publikasi

```sh
podman build -f Containerfile -t portfolio-api:local .
```

Jenkinsfile backend hanya menjalankan test Go, membangun image API, dan push ke `ghcr.io/<GHCR_OWNER>/portfolio-api` pada branch `main`. Job ini tidak membangun frontend.

Tambahkan credential Jenkins jenis username/password dengan ID `ghcr-creds` (username GitHub; password PAT dengan izin `write:packages`) dan environment variable `GHCR_OWNER` berisi username/organisasi pemilik paket. Pada homeserver, gunakan `.env` dengan nilai GHCR owner sama dan password database yang kuat.

Pull dan jalankan stack backend di homeserver:

```sh
podman compose pull
podman compose up -d
```

API diterbitkan pada `API_PORT` (default `3004`) agar Nginx pada deployment frontend terpisah dapat mengaksesnya. Batasi akses port ini pada firewall ke host frontend atau jaringan tepercaya; gunakan reverse proxy/TLS jika API perlu diakses lintas jaringan. Set `IMAGE_TAG` ke nomor build Jenkins untuk deploy versi tertentu. PostgreSQL tidak diterbitkan ke jaringan host.

## Catatan konten

Seed adalah contoh generik berbahasa Indonesia. Ganti nama, cerita, tanggal, lokasi, email, pengalaman, pendidikan, skill, dan project dengan data asli sebelum dipublikasikan.
