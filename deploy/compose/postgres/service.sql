-- Per-service setup, run by init.sh with psql -v svc=<name> -v pw=<password>.
-- The role owns its schema and has no access to any other service's schema.
CREATE ROLE :"svc" LOGIN PASSWORD :'pw';
CREATE SCHEMA :"svc" AUTHORIZATION :"svc";
ALTER ROLE :"svc" SET search_path = :"svc";
GRANT USAGE ON SCHEMA extensions TO :"svc";
