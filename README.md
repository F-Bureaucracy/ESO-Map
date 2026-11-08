# eso-map

`eso-map` scans a directory of Kubernetes manifests and maps how secrets flow
through the [External Secrets Operator](https://external-secrets.io), all the
way from the provider (OpenBao, Vault, AWS, GCP, Azure, ...) to the
environment variable or file inside your app:

```
OpenBao secret/apps/api/database#password
  → ExternalSecret api/api-db [password]
  → Secret api/api-db-credentials [DATABASE_URL]
  → Deployment api/api [$DATABASE_URL]
```

## Install

```sh
go install github.com/vincent/eso-map@latest
# or
make build   # → bin/eso-map
```

## Usage

```sh
eso-map scan [directory...] [flags]

  -o, --output string     json (default), yaml or html
      --out string        write to a file instead of stdout
      --include strings   file globs to scan (default *.yaml,*.yml)
      --exclude strings   file/dir globs to skip (default .git,node_modules,vendor,.terraform)
      --strict            exit 1 when warnings are found (useful in CI)
  -q, --quiet             no summary / parse errors on stderr
```

```sh
eso-map scan ./deploy -o html --out eso-map.html   # interactive diagram
eso-map scan ./deploy | jq -r '.flows[].summary'    # every end-to-end flow
eso-map scan ./deploy -o yaml | yq '.diagnostics'   # likely misconfigurations
```

## What it understands

| Layer              | Resources                                                                                                                                                                                                                             |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Providers          | `SecretStore`, `ClusterSecretStore` (any provider; OpenBao is detected on the `vault` provider by server URL)                                                                                                                         |
| Generators         | anything in `generators.external-secrets.io` (`Password`, `ECRAuthorizationToken`, `VaultDynamicSecret`, `ClusterGenerator`, ...)                                                                                                     |
| Remote paths       | `data[].remoteRef` (key + property), `dataFrom[].extract`, `dataFrom[].find`, PushSecret `remoteKey`                                                                                                                                  |
| ESO                | `ExternalSecret`, `ClusterExternalSecret`, `PushSecret`, `ClusterPushSecret`, incl. per-entry `sourceRef`, `target.template` (key-level mapping via template references) and store label selectors                                    |
| Kubernetes Secrets | targets of ExternalSecrets, plain `Secret` manifests (key names only — **values are never read into the output**)                                                                                                                     |
| Consumers          | Pod, Deployment, StatefulSet, DaemonSet, ReplicaSet, Job, CronJob, Argo Rollout, ...: `env[].valueFrom.secretKeyRef`, `envFrom[].secretRef`, `secret` and `projected` volumes with their mount paths, `imagePullSecrets`; Ingress TLS |

Objects nested inside other objects (resource templates of composition CRDs
and operators) are found too. When such templates sit in a list next to an
`id` and refer to each other as `${<id>.<field>}`, those references are
resolved so the objects link up. Other template expressions are kept verbatim
in names and paths.

Pass several directories to combine repositories, e.g. the apps and the repo
that defines the stores: `eso-map scan ../deployment ../flux`.

Namespaces that are left out of manifests (because kustomize or Helm sets them)
are treated as wildcards when resolving references.

Files that are not valid YAML (e.g. un-rendered Helm templates) are skipped and
reported on stderr. Render charts first (`helm template ... > rendered.yaml`) to
include them.

## Output

All formats contain the same model:

- `nodes` — stores, generators, remote paths, ESO resources, Secrets and
  consumers, each with `items` (keys, env vars, mount paths), attributes and
  the defining `source` file and line. Nodes that are referenced but not
  defined in the scanned files have `missing: true`.
- `edges` — `contains`, `fetch`, `generate`, `sync`, `select`, `push`,
  `consume`, at item granularity (`fromItem` / `toItem`).
- `flows` — every end-to-end path with a readable `summary`.
- `diagnostics` — e.g. references to undefined stores, env vars reading keys
  no ExternalSecret provides, Secrets nothing manages, synced Secrets nobody uses.

The HTML output is a single self-contained file (no CDN, works offline) that
draws the topology as five columns — providers → remote paths → ESO resources
→ Secrets → consumers — with arrows between individual keys. Click any card or
row to highlight its full upstream and downstream lineage, search for a path or
env var (`/`), filter by namespace, and open the diagnostics panel.

## Development

```sh
make test    # go test -race ./...
make lint    # go vet + gofmt
make example # render examples/ to bin/example.html
```

Layout:

```
main.go                 entrypoint
internal/cli            Cobra commands
internal/manifest       file discovery (globbing) and multi-document YAML decoding
internal/eso            minimal decode-only ESO / workload types
internal/graph          reference resolution, flows and diagnostics
internal/render         JSON, YAML and HTML renderers (HTML assets are embedded)
examples/               sample repository used by the tests
```
