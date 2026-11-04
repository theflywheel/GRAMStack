FROM postgres:17-bookworm@sha256:839fbb5a4942a7758dd93e18b42ea5649807491a0545b5eeeb8f0b8bb11857a5
ENV PGDATA=/var/lib/postgresql/data/pgdata
COPY infra/railway/database/init.sh /docker-entrypoint-initdb.d/10-platform.sh
COPY infra/esignet/init.sql /usr/local/share/esignet-init.sql
COPY infra/railway/database/start.sh /usr/local/bin/start-platform-db
ENTRYPOINT ["/usr/local/bin/start-platform-db"]
CMD ["postgres"]
