# Rotate CI deploy credentials to GitHub OIDC

## Summary

Replace the long-lived `ci-deploy` IAM user with per-environment roles assumed through GitHub's OIDC provider.

## Changes

- Terraform: `aws_iam_openid_connect_provider` for `token.actions.githubusercontent.com`
- Roles `gha-deploy-staging` and `gha-deploy-prod`, each trusting only `repo:acme/infra:environment:<env>`
- Workflows: `permissions: id-token: write` and `aws-actions/configure-aws-credentials` with `role-to-assume`
- Delete the `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` secrets, then the IAM user's keys

## Rollout

1. Apply the Terraform
2. Switch `nightly-snapshot.yml` first and watch one run
3. Switch staging deploys, then prod
4. Remove the IAM user after a week with no use in CloudTrail
