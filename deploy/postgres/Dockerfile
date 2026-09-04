# PostgreSQL with pg_cron.
#
# pg_cron must be in shared_preload_libraries, so it cannot be added to a
# running stock image — the scheduler is part of the server, not an extension
# that can be dropped in later. That is also the argument for using it: the
# schedule lives inside the database it maintains (ADR-0029).
FROM postgres:16

RUN apt-get update \
 && apt-get install -y --no-install-recommends postgresql-16-cron \
 && rm -rf /var/lib/apt/lists/*
