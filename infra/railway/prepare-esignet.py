"""Prepare private operator settings for the existing Railway eSignet topology."""
import argparse
import json
import os
from pathlib import Path
import secrets
from urllib.parse import quote

ISSUER_HOST = "gramstack-esignet-b9b69819.up.railway.app"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path, help="new private output directory")
    directory = parser.parse_args().directory
    directory.mkdir(mode=0o700)
    os.umask(0o077)
    database_password = secrets.token_hex(32)
    redis_password = secrets.token_hex(32)
    settings = {
        "platform-db": {
            "GRAMSTACK_DB_ESIGNET_PASSWORD": database_password,
            "GRAMSTACK_DB_ESIGNET_MOCK_PASSWORD": secrets.token_hex(32),
        },
        "esignet-redis": {"REDIS_PASSWORD": redis_password},
        "esignet-mock": {
            "container_user": "mosip",
            "active_profile_env": "default,local",
            "MOSIP_KERNEL_KEYMANAGER_HSM_CONFIG_PATH": "/keys/mock_local.p12",
            "MOSIP_KERNEL_KEYMANAGER_HSM_KEYSTORE_TYPE": "PKCS12",
            "MOSIP_KERNEL_KEYMANAGER_HSM_KEYSTORE_PASS": secrets.token_hex(32),
            "SPRING_DATASOURCE_URL": (
                "jdbc:postgresql://platform-db.railway.internal:5432/"
                "mosip_mockidentitysystem?currentSchema=mockidentitysystem"
            ),
            "SPRING_DATASOURCE_USERNAME": "esignet_mock",
            "SPRING_DATASOURCE_PASSWORD": "${{platform-db.GRAMSTACK_DB_ESIGNET_MOCK_PASSWORD}}",
            "MOSIP_ESIGNET_HOST": "esignet.railway.internal:8080",
        },
        "esignet": {
            "container_user": "mosip",
            "PORT": "8080",
            "DATABASE_HOST": "platform-db.railway.internal",
            "DATABASE_PORT": "5432",
            "DATABASE_USERNAME": "esignet",
            "DATABASE_PASSWORD": "${{platform-db.GRAMSTACK_DB_ESIGNET_PASSWORD}}",
            "KEYMANAGER_KEYSTORE_TYPE": "PKCS12",
            "KEYMANAGER_DB_SCHEMA": "esignet",
            "KEYMANAGER_PKCS12_FILE_PATH": "/opt/mosip/keys/demo.pfx",
            "KEYMANAGER_PKCS12_PASSWORD": secrets.token_hex(32),
            "KEYMANAGER_PKCS12_ALLOW_INSECURE_SOFTWARE_KEYSTORE": "true",
            "MOSIP_ESIGNET_CACHE_TYPE": "redis",
            "MOSIP_ESIGNET_HOST": "https://" + ISSUER_HOST,
            "MOSIP_ESIGNET_OIDC_UI_HOSTNAME": ISSUER_HOST,
            "MOSIP_ESIGNET_OIDC_UI_PORT": "443",
            "MOSIP_ESIGNET_OIDC_UI_SCHEME": "https",
            "MOSIP_ESIGNET_AUTH_FLOW_ID": "flow-local-demo-v2",
            "MOSIP_ESIGNET_CAPTCHA_SITE_PROVIDER": "",
            "MOSIP_ESIGNET_CAPTCHA_SITE_KEY": "",
            "MOSIP_ESIGNET_AUTHN_PROVIDER": "mock",
            "MOSIP_ESIGNET_MOCK_DOMAIN_URL": "http://esignet-mock.railway.internal:8082",
            "REDIS_URL": f"redis://:{redis_password}@esignet-redis.railway.internal:6379/0",
        },
        "esignet-ui": {
            "container_user": "mosip",
            "PORT": "3000",
            "DEFAULT_LANG": "en",
            "DEFAULT_WELLKNOWN": quote(json.dumps([
                {"name": "OpenID Configuration", "value": "/.well-known/openid-configuration"},
                {"name": "Jwks Json", "value": "/.well-known/jwks.json"},
                {"name": "OAuth Authorization Server", "value": "/.well-known/oauth-authorization-server"},
            ]), safe=""),
        },
    }
    for service, values in settings.items():
        with (directory / f".env.{service}.json").open("x", encoding="utf-8") as output:
            json.dump(values, output, indent=2)
            output.write("\n")
    print("Prepared private settings for five eSignet services.")


if __name__ == "__main__":
    main()
