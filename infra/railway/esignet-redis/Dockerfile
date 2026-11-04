FROM redis:8.2-bookworm@sha256:99ffb973380cc2f47bca06bd094f7f8c7138a0879a63cb8939896a22b7a65bba
COPY infra/railway/esignet-redis/start.sh /usr/local/bin/gramstack-redis
ENTRYPOINT ["/usr/local/bin/gramstack-redis"]
CMD []
