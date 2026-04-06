-- Aether PostgreSQL initialization script
-- Runs once when the database container is first created

-- Enable UUID generation support
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Create the application role
-- NOTE: Change this password in production via POSTGRES_PASSWORD env or secrets manager
CREATE ROLE aether WITH
  LOGIN
  PASSWORD 'aether'
  CONNECTION LIMIT 100
  NOSUPERUSER
  NOCREATEDB
  NOCREATEROLE;

-- Grant full privileges on the aether database to the aether role
GRANT ALL PRIVILEGES ON DATABASE aether TO aether;

-- Ensure the aether role owns the public schema so it can create tables
ALTER SCHEMA public OWNER TO aether;

-- Grant usage and create on public schema (needed for PostgreSQL 15+)
GRANT USAGE, CREATE ON SCHEMA public TO aether;
