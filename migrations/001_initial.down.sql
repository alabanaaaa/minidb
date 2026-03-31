-- 001_initial.down.sql
DROP TABLE IF EXISTS system_metrics;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS mpesa_transactions;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS workers;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS shop_users;
DROP TABLE IF EXISTS shops;
DROP EXTENSION IF EXISTS "uuid-ossp";
