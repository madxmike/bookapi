CREATE TABLE IF NOT EXISTS books (
  isbn text PRIMARY KEY,
  title text NOT NULL,
  author text NOT NULL,
  published_year integer NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
