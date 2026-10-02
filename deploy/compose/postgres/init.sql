-- Shared setup, run once by init.sh as the bootstrap superuser.
-- Extensions live in their own schema so every service role can use them
-- without any role owning them.
CREATE SCHEMA IF NOT EXISTS extensions;
CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA extensions;

-- Nobody creates objects in public.
REVOKE ALL ON SCHEMA public FROM PUBLIC;
