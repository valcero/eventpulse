DROP FUNCTION IF EXISTS ensure_deliveries_partition(DATE);
DROP TABLE IF EXISTS deliveries CASCADE;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS endpoints;
DROP TABLE IF EXISTS applications;
DROP TYPE IF EXISTS delivery_status;
