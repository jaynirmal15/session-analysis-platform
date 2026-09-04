-- Unschedule, but leave the extension: another database in this cluster may be
-- using it, and dropping an extension out from under one is not this
-- migration's business.
SELECT cron.unschedule('maintain-event-raw-partitions')
 WHERE EXISTS (SELECT 1 FROM cron.job WHERE jobname = 'maintain-event-raw-partitions');
