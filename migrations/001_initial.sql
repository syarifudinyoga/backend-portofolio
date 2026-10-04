CREATE TABLE IF NOT EXISTS profile (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    headline TEXT NOT NULL,
    about TEXT NOT NULL,
    location TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    website TEXT NOT NULL DEFAULT '',
    avatar_url TEXT NOT NULL DEFAULT '',
    resume_url TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS experiences (
    id BIGSERIAL PRIMARY KEY,
    company TEXT NOT NULL,
    role TEXT NOT NULL,
    location TEXT NOT NULL DEFAULT '',
    start_date DATE NOT NULL,
    end_date DATE,
    description TEXT NOT NULL DEFAULT '',
    highlights TEXT[] NOT NULL DEFAULT '{}',
    CHECK (end_date IS NULL OR end_date >= start_date)
);

CREATE TABLE IF NOT EXISTS education (
    id BIGSERIAL PRIMARY KEY,
    institution TEXT NOT NULL,
    degree TEXT NOT NULL,
    field TEXT NOT NULL DEFAULT '',
    start_date DATE NOT NULL,
    end_date DATE,
    description TEXT NOT NULL DEFAULT '',
    CHECK (end_date IS NULL OR end_date >= start_date)
);

CREATE TABLE IF NOT EXISTS skills (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    category TEXT NOT NULL,
    level SMALLINT NOT NULL CHECK (level BETWEEN 1 AND 5),
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tech_stack (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    category TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS projects (
    id BIGSERIAL PRIMARY KEY,
    title TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    year SMALLINT NOT NULL CHECK (year BETWEEN 1900 AND 2200),
    image_url TEXT NOT NULL DEFAULT '',
    live_url TEXT NOT NULL DEFAULT '',
    repo_url TEXT NOT NULL DEFAULT '',
    tech TEXT[] NOT NULL DEFAULT '{}'
);

INSERT INTO profile (id, name, role, headline, about, location, email, website, avatar_url, resume_url)
VALUES (
    1,
    'Nama Anda',
    'Full-stack Developer',
    'Merangkai ide menjadi pengalaman digital yang bermakna.',
    'Saya adalah developer yang senang membangun produk digital dengan perhatian pada detail, performa, dan pengalaman pengguna. Ganti cerita ini dengan perkenalan dan fokus profesional Anda.',
    'Kota Anda, Indonesia',
    'hello@example.com',
    '',
    '',
    ''
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO experiences (company, role, location, start_date, end_date, description, highlights)
SELECT 'Studio Kreatif', 'Full-stack Developer', 'Remote', '2023-01-01', NULL,
       'Membangun produk web end-to-end bersama tim desain dan produk.',
       ARRAY['Mengembangkan layanan API yang mudah dirawat', 'Meningkatkan pengalaman pengguna melalui iterasi berbasis feedback']
WHERE NOT EXISTS (SELECT 1 FROM experiences);

INSERT INTO education (institution, degree, field, start_date, end_date, description)
SELECT 'Universitas Anda', 'Sarjana', 'Ilmu Komputer', '2018-08-01', '2022-06-01',
       'Ganti dengan pendidikan, pencapaian, atau fokus studi Anda.'
WHERE NOT EXISTS (SELECT 1 FROM education);

INSERT INTO skills (name, category, level, sort_order)
SELECT sample.name, sample.category, sample.level, sample.sort_order
FROM (VALUES
    ('Product thinking', 'Cara kerja', 4, 1),
    ('Kolaborasi tim', 'Cara kerja', 5, 2),
    ('Problem solving', 'Cara kerja', 4, 3),
    ('Web development', 'Keahlian', 5, 1),
    ('API design', 'Keahlian', 4, 2),
    ('UI implementation', 'Keahlian', 4, 3)
) AS sample(name, category, level, sort_order)
WHERE NOT EXISTS (SELECT 1 FROM skills);

INSERT INTO tech_stack (name, category, sort_order)
SELECT sample.name, sample.category, sample.sort_order
FROM (VALUES
    ('Go', 'Backend', 1),
    ('PostgreSQL', 'Backend', 2),
    ('React', 'Frontend', 1),
    ('TypeScript', 'Frontend', 2),
    ('Docker / Podman', 'DevOps', 1),
    ('GitHub Actions', 'DevOps', 2)
) AS sample(name, category, sort_order)
WHERE NOT EXISTS (SELECT 1 FROM tech_stack);

INSERT INTO projects (title, category, description, year, image_url, live_url, repo_url, tech)
SELECT sample.title, sample.category, sample.description, sample.year, sample.image_url, sample.live_url, sample.repo_url, sample.tech
FROM (VALUES
    ('Ruang Cerita', 'Web application', 'Platform sederhana untuk mengumpulkan cerita dan ide dalam satu ruang yang nyaman.', 2025, '', '', '', ARRAY['React', 'Go', 'PostgreSQL']::TEXT[]),
    ('Kolektif Studio', 'Brand & website', 'Eksplorasi identitas digital yang menghubungkan narasi, visual, dan interaksi.', 2024, '', '', '', ARRAY['React', 'CSS', 'Figma']::TEXT[])
) AS sample(title, category, description, year, image_url, live_url, repo_url, tech)
WHERE NOT EXISTS (SELECT 1 FROM projects);
