# Portfolio API — Go + PostgreSQL

Backend independen untuk menyajikan data portfolio dari PostgreSQL melalui API read-only.

## Jalankan lokal

1. Salin `.env.example` menjadi `.env`; isi `GHCR_OWNER` dan ganti `POSTGRES_PASSWORD`.
2. Sesuaikan seed awal di `migrations/001_initial.sql` sebelum database pertama kali dijalankan.
3. Jalankan `podman compose up --build` (atau gunakan `docker compose`).
4. API tersedia di `http://localhost:8080`.

Migration SQL otomatis dijalankan hanya saat volume database masih kosong. Untuk perubahan setelah inisialisasi, terapkan migration secara eksplisit. Volume bernama `postgres_data` menyimpan data di luar lifecycle container.

## Menjalankan tanpa container

API memerlukan PostgreSQL 16+ dan membaca `DATABASE_URL`, atau variabel `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, dan `POSTGRES_SSLMODE`. Terapkan `migrations/001_initial.sql`, lalu jalankan `go run ./cmd/api`.

## Endpoint

- `GET /api/health` — status API dan koneksi database (`204` jika sehat).
- `GET /api/portfolio` — profil, pengalaman, pendidikan, skills, tech stack, dan karya dalam satu JSON.

API sengaja read-only; endpoint admin tidak diekspos. Perbarui konten melalui SQL/migration. Aplikasi web di repository terpisah mengakses API lewat reverse proxy Nginx-nya: atur `API_UPSTREAM` di repository frontend ke alamat API yang dapat dijangkau dari container frontend.

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

API diterbitkan pada `API_PORT` (default `8080`) agar Nginx pada deployment frontend terpisah dapat mengaksesnya. Batasi akses port ini pada firewall ke host frontend atau jaringan tepercaya; gunakan reverse proxy/TLS jika API perlu diakses lintas jaringan. Set `IMAGE_TAG` ke nomor build Jenkins untuk deploy versi tertentu. PostgreSQL tidak diterbitkan ke jaringan host.

## Catatan konten

Seed adalah contoh generik berbahasa Indonesia. Ganti nama, cerita, tanggal, lokasi, email, pengalaman, pendidikan, skill, dan project dengan data asli sebelum dipublikasikan.
