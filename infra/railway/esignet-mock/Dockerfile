FROM mosipid/mock-identity-system:0.14.0@sha256:4c3bbddfd280b5a6411c89db5f3b6ab231fc3a49fc25794e2e210db36f8cbb96
USER root
COPY --chmod=0755 infra/railway/esignet/keystore-start.sh /usr/local/bin/gramstack-keystore
ENTRYPOINT ["/usr/local/bin/gramstack-keystore", "/keys", "/home/mosip/configure_start.sh"]
CMD ["/bin/sh","-c","java -jar -Dloader.path=\"${loader_path_env}\" -Dspring.cloud.config.label=\"${spring_config_label_env}\" -Dspring.profiles.active=\"${active_profile_env}\" -Dspring.cloud.config.uri=\"${spring_config_url_env}\" mock-identity-system.jar"]
