FROM mosipid/esignet:2.0.0@sha256:e85ff415527b355765b8603c14c738512c185df537e3ad69feffbbbdb45358ee
COPY infra/esignet/flow-local-demo.yaml /home/mosip/data/flows/flow-local-demo-v2.yaml
USER root
COPY --chmod=0755 infra/railway/esignet/keystore-start.sh /usr/local/bin/gramstack-keystore
ENTRYPOINT ["/usr/local/bin/gramstack-keystore", "/opt/mosip/keys", "/home/mosip/entrypoint.sh"]
CMD ["/home/mosip/esignet.exe"]
