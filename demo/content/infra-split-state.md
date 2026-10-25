# Split Terraform state per environment

## Context

All 412 resources live in one state at `s3://acme-tfstate/main.tfstate`. Every `terraform plan` refreshes staging and prod together and takes about 6 minutes, and a lock held for one environment blocks the other.

## Approach

Three root modules sharing the existing child modules:

```
envs/
  shared/    VPC, DNS, IAM
  staging/
  prod/
modules/     unchanged
```

Each root gets its own backend key. Cross-root values such as VPC and zone ids are read from `shared` through `terraform_remote_state`.

## Steps

1. Create the three roots and their backends
2. Generate a `terraform state mv` per resource address and review the list
3. Migrate `shared`, then `staging`, then `prod`, checking that `plan` shows no changes after each
4. Make CI plan and apply each root separately

## Verification

- `terraform plan` reports no changes in every root after the migration
- Plan time per environment under 2 minutes
