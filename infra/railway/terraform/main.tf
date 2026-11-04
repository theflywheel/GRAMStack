terraform {
  required_version = ">= 1.11, < 2.0"

  required_providers {
    railway = {
      source  = "terraform-community-providers/railway"
      version = "0.6.2"
    }
  }
}

provider "railway" {}

locals {
  project_id     = "b9b69819-44c0-4681-8dc1-c1f646863aa2"
  environment_id = "02c9cbe2-2170-4f83-a9a9-bcf2401831cb"
  region         = "asia-southeast1-eqsg3a"
  docs_id        = "90b4a55c-dd88-4746-a832-d4746b676a88"
  database_id    = "24edbc4c-a655-419f-8f7f-9220b77cce8b"
}

resource "railway_service" "docs" {
  name       = "docs"
  project_id = local.project_id
  regions    = [{ region = local.region, num_replicas = 1 }]

  lifecycle {
    prevent_destroy = true
  }
}

resource "railway_service" "platform_db" {
  name       = "platform-db"
  project_id = local.project_id
  regions    = [{ region = local.region, num_replicas = 1 }]
  volume = {
    name       = "platform-data"
    mount_path = "/var/lib/postgresql/data"
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "railway_custom_domain" "docs" {
  domain         = "docs.gramstack.try-dough.com"
  service_id     = railway_service.docs.id
  environment_id = local.environment_id
  target_port    = 8080

  lifecycle {
    prevent_destroy = true
  }
}

resource "railway_service_domain" "docs" {
  subdomain      = "design-site-production-2e14"
  service_id     = railway_service.docs.id
  environment_id = local.environment_id

  lifecycle {
    prevent_destroy = true
  }
}

import {
  to = railway_service.docs
  id = local.docs_id
}

import {
  to = railway_service.platform_db
  id = local.database_id
}

import {
  to = railway_custom_domain.docs
  id = "${local.docs_id}:production:docs.gramstack.try-dough.com"
}

import {
  to = railway_service_domain.docs
  id = "${local.docs_id}:production:design-site-production-2e14.up.railway.app"
}
