# chain-sync-watch Helm chart

Packages the full stack — `csw-server`, `csw-worker`, the Next.js web
UI, and a one-shot `csw migrate up` Job — into a single release. Bundles
Bitnami `postgresql` and `redis` subcharts so `helm install` works out
of the box; disable them to plug in managed services for production.

## Requirements

- Kubernetes ≥ 1.28
- Helm ≥ 3.14
- Network access to `charts.bitnami.com` (for `helm dependency update`)
  and `ghcr.io` (for the application images)

## Install

The chart is published as an OCI artifact on GHCR with every tagged
release — `helm install` can pull it directly:

```bash
helm install csw oci://ghcr.io/seokheejang/charts/chain-sync-watch \
  --version 0.1.0 \
  --namespace chain-sync-watch --create-namespace \
  --set secrets.CSW_SECRET_KEY="$(openssl rand -base64 32)"
```

Or install from a checked-out source tree:

```bash
# one-time: fetch the postgres + redis subcharts into ./charts/
helm dependency update ./deploy/helm/chain-sync-watch

# minimal install — only requires CSW_SECRET_KEY
helm install csw ./deploy/helm/chain-sync-watch \
  --namespace chain-sync-watch --create-namespace \
  --set secrets.CSW_SECRET_KEY="$(openssl rand -base64 32)"

# install with a local secrets file (recommended for real credentials)
cp ./deploy/helm/chain-sync-watch/values.example.yaml \
   ./deploy/helm/chain-sync-watch/values.secret.yaml
# ...edit values.secret.yaml...
helm install csw ./deploy/helm/chain-sync-watch \
  --namespace chain-sync-watch --create-namespace \
  -f ./deploy/helm/chain-sync-watch/values.secret.yaml
```

`values.secret*.yaml` and `values.local*.yaml` are gitignored — use
those filenames to keep real credentials out of source control.

## Upgrade / rollback

```bash
helm upgrade csw ./deploy/helm/chain-sync-watch \
  -n chain-sync-watch \
  -f ./deploy/helm/chain-sync-watch/values.secret.yaml

helm rollback csw 1 -n chain-sync-watch   # back to the previous revision
```

Every install/upgrade runs the migrate Job as a Helm pre-hook, so the
app pods only boot against a migrated schema.

## Uninstall

```bash
helm uninstall csw -n chain-sync-watch
# Postgres + Redis PVCs survive uninstall on purpose. Delete manually:
kubectl -n chain-sync-watch delete pvc -l app.kubernetes.io/instance=csw
```

## Secrets: how values flow into the pods

The chart takes the following pattern (see
[docs/plans/phase-11-kubernetes-deploy.md](../../../docs/plans/phase-11-kubernetes-deploy.md)
§"Secret processing"):

1. You supply values only — via a gitignored `values.secret.yaml`
   or `--set secrets.*=...` from CI.
2. The chart renders them into a `Secret` resource named
   `<release>-chain-sync-watch-secrets`.
3. Server / worker / migrate pods pick them up via
   `envFrom: secretRef`. Pods never see the raw values file and you
   never have to create a `Secret` object manually.

Required keys:

| Key                | Source                                   |
|--------------------|------------------------------------------|
| `CSW_SECRET_KEY`   | mandatory — `openssl rand -base64 32`    |
| `DATABASE_URL`     | auto (subchart) OR explicit for managed DB |
| `REDIS_URL`        | auto (subchart) OR explicit for managed Redis |

Optional: `CSW_ADAPTERS__ETHERSCAN__API_KEY`,
`CSW_ADAPTERS__RPC__ENDPOINTS__10`, etc.

## Production checklist

- [ ] `postgresql.enabled: false` — point `.secrets.DATABASE_URL` at a
      managed Postgres (RDS / Cloud SQL / Crunchy). The bundled subchart
      is for demos and CI.
- [ ] `redis.enabled: false` — use a managed Redis with persistence on.
- [ ] `<component>.autoscaling.enabled: true` for server / web; the
      Deployment then omits `spec.replicas` so HPA owns it.
- [ ] `<component>.pdb.enabled: true` so drains and node upgrades
      don't take the release down.
- [ ] `ingress.enabled: true` with a `className`, `host`, and `tls`.
      The default rule routes `/api` → server and `/` → web; pair with
      a controller that supports rewrite (see `environments/values.prod.yaml`).
- [ ] `externalSecrets.enabled: true` plus a populated `data:` map —
      the inline `.secrets` block is ignored in this mode.
- [ ] Set resource requests/limits per cluster capacity.
- [ ] `imagePullSecrets:` if ghcr.io images are private.

## Environment overlays

`environments/values.{dev,staging,prod}.yaml` ship as starting points:

```bash
# dev — single-replica, bundled subcharts, no Ingress
helm install csw deploy/helm/chain-sync-watch \
  -f deploy/helm/chain-sync-watch/environments/values.dev.yaml \
  --set secrets.CSW_SECRET_KEY="$(openssl rand -base64 32)" \
  -n csw-dev --create-namespace

# staging — HPA + PDB on, Ingress on, ServiceMonitor on
helm install csw deploy/helm/chain-sync-watch \
  -f deploy/helm/chain-sync-watch/environments/values.staging.yaml \
  --set secrets.CSW_SECRET_KEY="$(openssl rand -base64 32)" \
  -n csw-staging --create-namespace

# prod — managed Postgres/Redis, ExternalSecrets-only
helm install csw deploy/helm/chain-sync-watch \
  -f deploy/helm/chain-sync-watch/environments/values.prod.yaml \
  --set externalSecrets.secretStoreRef.name=<your-secret-store> \
  -n csw --create-namespace
```

Edit the prod overlay's `externalSecrets.data` map to match your
remote secret keys before installing.

## Optional add-ons

| Toggle                                       | What it ships                              |
|----------------------------------------------|--------------------------------------------|
| `ingress.enabled=true`                       | One `Ingress` for web + API (`/api` route) |
| `<server\|worker\|web>.autoscaling.enabled=true` | `HorizontalPodAutoscaler` (CPU + optional memory) |
| `<server\|worker\|web>.pdb.enabled=true`        | `PodDisruptionBudget` (default `minAvailable: 1`) |
| `metrics.serviceMonitor.enabled=true`        | Prometheus Operator `ServiceMonitor` for csw-server (waits on the `/metrics` endpoint, not yet implemented) |
| `externalSecrets.enabled=true`               | `ExternalSecret` instead of inline Secret  |

Schema validation runs automatically — invalid keys, types, or enum
values fail `helm install` / `helm template` early.

## Development

Render locally without installing:

```bash
helm template csw ./deploy/helm/chain-sync-watch \
  --set secrets.CSW_SECRET_KEY=dummykey | less

helm lint ./deploy/helm/chain-sync-watch
```

## Values reference

See [values.yaml](values.yaml) — every field has an inline comment.
