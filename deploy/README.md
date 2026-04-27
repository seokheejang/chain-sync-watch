# Deployment

Two deployment paths live here:

- **Helm chart** — `helm/chain-sync-watch/` — the primary Kubernetes
  package. Single `helm install` brings up server + worker + web +
  bundled Postgres + Redis + migration Job. See
  [helm/chain-sync-watch/README.md](helm/chain-sync-watch/README.md).
- **Compose auth proxy** — `Caddyfile` — basic-auth reverse proxy used
  by the Docker Compose `auth` profile for team-shared dev deployments.
  See [docs/plans/phase-10-integration-observability.md](../docs/plans/phase-10-integration-observability.md).

## Kubernetes quick start

```bash
# From the repo root:
helm dependency update ./deploy/helm/chain-sync-watch

helm install csw ./deploy/helm/chain-sync-watch \
  --namespace chain-sync-watch --create-namespace \
  --set secrets.CSW_SECRET_KEY="$(openssl rand -base64 32)"

kubectl -n chain-sync-watch port-forward svc/csw-chain-sync-watch-web 3000:3000
open http://localhost:3000
```

Full reference: [helm/chain-sync-watch/README.md](helm/chain-sync-watch/README.md).

## What ships / what's next

| Slice | Status | Scope |
|---|---|---|
| 1 (this one) | ✅ | Chart skeleton, server/worker/web Deployments, migrate Job (helm hook), bundled Bitnami postgres+redis, NOTES + README |
| 2 | ⏳ | Ingress, HPA, PDB, ServiceMonitor, `values.schema.json`, env overlays (dev/staging/prod), ExternalSecrets support |
| 3 | ⏳ | `.github/workflows/release.yml` — GHCR image push + `helm package` + `kubeconform` CI |
| 4 (optional) | ⏳ | ArgoCD `Application` template, kind smoke test |

Design doc: [docs/plans/phase-11-kubernetes-deploy.md](../docs/plans/phase-11-kubernetes-deploy.md).
