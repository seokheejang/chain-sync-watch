# ArgoCD GitOps for chain-sync-watch

Two manifests for two patterns:

| File | Pattern | Use when |
|---|---|---|
| [application.yaml](application.yaml) | Single `Application` | One cluster, one environment, or you manage envs in separate ArgoCD instances. |
| [applicationset.yaml](applicationset.yaml) | `ApplicationSet` (list generator) | One ArgoCD instance manages dev / staging / prod side-by-side. |

## Prerequisites

- ArgoCD ≥ 2.7 installed in the cluster (the `argocd` namespace by
  convention). Older versions don't support OCI Helm chart sources.
- Namespace `argocd` exists; the manifests write Applications into it.
- The destination namespace (default: `chain-sync-watch`) doesn't need
  to pre-exist — `CreateNamespace=true` in `syncOptions` lets ArgoCD
  create it.

## Quick start (single Application)

```bash
kubectl apply -n argocd -f deploy/argocd/application.yaml
argocd app sync chain-sync-watch
```

The default config points at `main` branch and the `staging` overlay.
Edit `spec.source.targetRevision` (branch / tag / commit SHA) and
`spec.source.helm.valueFiles` to switch environments.

## Switching to OCI source

The release pipeline publishes the chart to `ghcr.io/<owner>/charts`
on every tag push. To pin a released version instead of tracking
`main`:

```yaml
spec:
  source:
    repoURL: ghcr.io/seokheejang/charts
    chart: chain-sync-watch
    targetRevision: 0.1.0   # the chart .tgz tag
    helm:
      valueFiles:
        - environments/values.prod.yaml
```

OCI sources guarantee the same `.tgz` ArgoCD installs is what `helm
package` produced — no surprise re-renders from a moving git ref.

## Multi-environment fan-out

```bash
kubectl apply -n argocd -f deploy/argocd/applicationset.yaml
```

The default list generator emits three Applications:

- `csw-dev`     → namespace `chain-sync-watch-dev`     ← `values.dev.yaml`
- `csw-staging` → namespace `chain-sync-watch-staging` ← `values.staging.yaml`
- `csw-prod`    → namespace `chain-sync-watch-prod`    ← `values.prod.yaml`

Each tracks `main`. Replace the list generator with a `git` /
`pullRequest` / `cluster` generator if your environment topology
isn't enumerable upfront.

## Sync waves

The chart's `migrate` Job uses Helm hooks
(`helm.sh/hook: pre-install,pre-upgrade`). ArgoCD honours these as
sync waves automatically — the Job runs before any Deployment is
applied. No extra configuration needed.

## Secrets

The committed manifests don't carry any credentials. Production
deployments should set `externalSecrets.enabled=true` in the values
overlay and pre-provision the matching `ClusterSecretStore` in the
target cluster (see [../helm/chain-sync-watch/environments/values.prod.yaml](../helm/chain-sync-watch/environments/values.prod.yaml)).

For dev / smoke testing where ExternalSecrets isn't available, set
`secrets.CSW_SECRET_KEY` via:

- ArgoCD CLI: `argocd app set chain-sync-watch -p secrets.CSW_SECRET_KEY=...`
- An additional values file added via `valueFiles` (gitignored)
- `parameters` block in the Application spec (visible to anyone with
  read access on Applications — avoid for real secrets)
