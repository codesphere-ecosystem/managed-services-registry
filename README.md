# Harbor Managed Service Provider

This repository provides a Codesphere managed service for a registry as a service. It uses Harbor as the backing registry and exposes a managed service provider at `/api/v1/harbor` via `github.com/codesphere-cloud/managed-services-lib`.

The repository consists of two parts:

- the Harbor deployment in Codesphere, defined in [`ci.harbor.yml`](./ci.harbor.yml)
- the managed service provider implementation and provider definition, which are described below

Each managed service instance creates:

- one Harbor project
- one Harbor project-scoped robot account with full project privileges

The provider derives Harbor resource names from the managed-service `id` so it can support list, status, update, and delete operations without an external state store.
Project and robot names are prefixed with `teamId` so they stay unique in a shared Harbor instance.
The generic plan's `parameters.storage` value is used as the Harbor project storage quota in MiB.

## Configuration

Core server settings come from the upstream library:

- `PORT` defaults to `8080`
- `API_KEY` enables request authentication when set
- `ENVIRONMENT` accepts `development` or `production`

Harbor settings:

- `HARBOR_URL` required, for example `https://harbor.example.com`
- `HARBOR_AUTH_MODE` optional, `basic` or `bearer`, default `basic`
- `HARBOR_USERNAME` required for `basic`
- `HARBOR_PASSWORD` required for `basic`
- `HARBOR_TOKEN` required for `bearer`
- `HARBOR_INSECURE_SKIP_VERIFY` optional, default `false`
- `HARBOR_TIMEOUT_SECONDS` optional, default `30`
- `HARBOR_PROJECT_PREFIX` optional, default `ms-`
- `HARBOR_DEFAULT_ROLE_ID` optional, default `2`

## Repository Structure

### 1. Harbor deployment in Codesphere

[`ci.harbor.yml`](./ci.harbor.yml) deploys the Harbor instance itself inside Codesphere. It provisions and wires together:

- a Harbor Helm deployment in a vCluster
- a PostgreSQL managed service instance for Harbor metadata
- an S3 managed service instance for Harbor object storage
- a headless service route for internal access to Harbor

The Harbor Helm values are defined in [`values.prod.yaml`](./values.prod.yaml).

### 2. Managed service provider

The Go service in this repository exposes the Harbor-backed managed service API. It is deployed with [`ci.provider.yml`](./ci.provider.yml) and registered in Codesphere through [`provider.yml`](./provider.yml).

This part is responsible for:

- creating Harbor projects
- creating and rotating Harbor robot credentials
- updating project visibility and storage quota
- reporting managed service status and details back to Codesphere

## API

Create:

```json
{
  "id": "team-a",
  "teamId": 42,
  "config": {
    "public": false
  },
  "plan": {
    "parameters": {
      "cpu": 0,
      "memory": 0,
      "storage": 10240
    }
  },
  "secrets": {
    "superuserPassword": "change-me"
  }
}
```

`superuserPassword` is used as the initial Harbor robot secret.

Update:

```json
{
  "config": {
    "public": true
  },
  "plan": {
    "parameters": {
      "storage": 20480
    }
  },
  "secrets": {
    "superuserPassword": "new-password"
  }
}
```

`secrets.superuserPassword` rotates the managed Harbor robot secret.

## Deployment

The full system deployment has three parts and should be done in this order.

### 1. Deploy the Harbor backing services

Run the Codesphere deployment defined in [`ci.harbor.yml`](./ci.harbor.yml).

This deploys:

- Harbor via Helm
- a PostgreSQL managed service instance
- an S3 managed service instance

Required vault secrets for this deployment:

- `HARBOR_ADMIN_PASSWORD`
- `HARBOR_DATABASE_PASSWORD`
- `HARBOR_JOBSERVICE_SECRET`
- `HARBOR_REGISTRY_PASSWORD`
- `HARBOR_REGISTRY_SECRET`
- `HARBOR_SECRET_KEY`
- `HARBOR_S3_ACCESS_KEY`
- `HARBOR_S3_SECRET_KEY`

After deployment, Harbor is reachable through the workspace domain configured by:

- `externalURL=https://$WORKSPACE_DEV_DOMAIN`

### 2. Build and publish the provider image

Build the provider container image from [`Dockerfile`](./Dockerfile) and publish it to GitHub Container Registry with the workflow in [`.github/workflows/check-and-build.yml`](./.github/workflows/check-and-build.yml).

The workflow publishes to:

- `ghcr.io/<owner>/<repo>`
- `:latest` from the default branch
- branch, tag, and commit-SHA tags from GitHub Actions metadata

### 3. Deploy the provider service

Deploy the Codesphere workload defined in [`ci.provider.yml`](./ci.provider.yml).

The provider expects:

- `API_KEY` from vault key `HARBOR_PROVIDER_API_KEY`
- `HARBOR_URL` from workspace env `HARBOR_URL`
- `HARBOR_USERNAME` from vault key `HARBOR_USERNAME`
- `HARBOR_PASSWORD` from vault key `HARBOR_PASSWORD`

The provider workload uses the published OCI image:

<!-- x-release-please-start-version -->
```text
ghcr.io/codesphere-ecosystem/managed-services-registry:v1.0.1
```
<!-- x-release-please-end -->

The service exposes the managed service API on:

- `/api/v1/harbor`

### 4. Register the provider in Codesphere

Use [`provider.yml`](./provider.yml) to register the Harbor managed service in Codesphere.

Important values to verify before rollout:

- `backend.api.endpoint` points to the deployed provider service URL
- `backend.api.accessKey` matches `HARBOR_PROVIDER_API_KEY`
- the plan definitions match the intended service offering

Once registered, Codesphere can provision Harbor-backed registry instances through the provider API.

## Naming

For a service ID of `team-a` and `teamId` `42` the provider creates:

- project: `ms-team-42-team-a`
- robot: `ms-team-42-team-a`

If the service ID contains unsupported characters, it is normalized to lowercase and non-alphanumeric runs are converted to `-`.
