# Terraform adoption for Railway

This configuration drafts the import of the existing production services,
the PostgreSQL volume attached to its service, and both docs domains.
It has no resources for Railway secrets and declares no public database route.

Railway IaC currently owns build settings, migrations and health checks.
Before a Terraform cutover, decide how those settings will be managed.
The committed source archives from `../stage.sh` are uploaded separately.

## Cutover

1. Select a private, locked Terraform state backend. The repository does not
   include a backend choice or credentials.
2. Set a Railway workspace or account API token in the operator environment
   as `RAILWAY_TOKEN`. Keep database passwords and service key bundles in Railway.
3. Run `terraform init`, `terraform validate` and `terraform plan` here.
   Review the complete plan for an import-only change with zero resource
   additions, changes or deletions. Compare the live volume ID, private DNS
   names and docs routes with the current Railway inventory. Do not apply a
   plan that changes those settings or any live service.
4. After the import, require a clean plan. Retire the TypeScript apply path
   before Terraform makes further infrastructure changes. Use one writer
   per managed resource.

Until this cutover is verified, the existing Railway configuration remains
authoritative and the Terraform directory is a migration candidate.

[Provider documentation](https://registry.terraform.io/providers/terraform-community-providers/railway/0.6.2/docs)
