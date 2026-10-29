-- Applied on the DB host only by deploy.sh section 0 (sudo mysql < this file). Idempotent.
-- NOT loaded by init.sh (init.sh names its files explicitly; no glob).
CREATE USER IF NOT EXISTS 'isucon'@'%' IDENTIFIED BY 'isucon';
GRANT ALL ON isuride.* TO 'isucon'@'%';
FLUSH PRIVILEGES;
