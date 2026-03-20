CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

SELECT 'CREATE DATABASE oro_db_test' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'oro_db_test')\gexec
\c oro_db_test
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
