# provider-openwebui

A native Crossplane v2 provider for Open WebUI. Work in progress; see `docs/progress.md`.

## Provider configuration

A `ProviderConfig` connects the managed resources of its namespace to an Open WebUI instance; a `ClusterProviderConfig` serves every namespace, and managed resources use the one named `default` unless they reference another. Both authenticate with the API key of an Open WebUI admin account, read from a Secret:

```yaml
apiVersion: openwebui.m.crossplane.io/v1alpha1
kind: ProviderConfig
metadata:
  name: example
  namespace: default
spec:
  url: http://open-webui.open-webui.svc:8080
  credentials:
    source: Secret            # the only source
    secretRef:                # a Secret of the ProviderConfig's namespace
      name: openwebui-api-key
      key: token
```

- `credentials.secretRef` of a `ProviderConfig` has no namespace: its Secrets are always read in its own namespace ([ADR-0006](docs/adr/0006-rbac-scope.md)). A `ClusterProviderConfig` names the namespace with `secretRef.namespace`, and likewise `caCertSecretRef.namespace`.
- Optional settings: `authHeader` (default `Authorization`, which sends `Bearer <key>`; any other header sends the bare key), `caCertSecretRef` (PEM CA certificates trusted besides the system's), `insecureSkipVerify` and `allowUnsupportedVersions`.

`examples/provider/config.yaml` holds both kinds. Once connected, a provider config reports `status.version`, `status.profile` and its `Ready` condition.

## Development environment

The devcontainer is the only execution environment. The host needs a container runtime (Docker or Podman) and the [devcontainer CLI](https://github.com/devcontainers/cli); everything else is installed inside the container. Tool versions are pinned in `mise.toml`, mise itself in `.devcontainer/Dockerfile` and `.github/workflows/ci.yml`, which Renovate updates together. The tools live in the `provider-openwebui-mise` volume, so rebuilding the container does not download them again. Containers started from inside (Open WebUI instances, kind clusters) run in the devcontainer's own Docker daemon (docker-in-docker).

```bash
devcontainer up --workspace-folder .                    # add --docker-path podman with Podman
devcontainer exec --workspace-folder . <command>        # e.g. mise run owui:up 0.12.4
```

Every command in this repository is meant to run through `devcontainer exec` (or a shell opened with `devcontainer exec --workspace-folder . bash`).

### Building the provider

The provider is scaffolded from [`crossplane/provider-template`](https://github.com/crossplane/provider-template) and builds with its `make` targets, using the `build` submodule (`git submodule update --init` after cloning). `make` takes every tool from `mise.toml` and never downloads its own; outside the devcontainer it stops with a message.

```bash
make reviewable     # generate code and CRDs, go mod tidy, lint, unit tests
make build          # binary, image and package (xpkg) for the host platform
make provider.addtype provider=OpenWebUI group=<group> kind=<Kind>   # scaffold a new kind, then make generate
```

### Throwaway Open WebUI instances

```bash
mise run owui:up 0.12.4        # slim image, SQLite, ENV=dev; prints OWUI_URL and OWUI_API_KEY
mise run owui:openapi 0.12.4   # stores docs/api-notes/openapi/v0.12.4.json
mise run owui:down 0.12.4      # removes the container and its data
```

Each version gets a fixed port (`0.12.4` → `31204`) so several versions can run side by side. The admin credentials are disposable defaults from `hack/openwebui/lib.sh`, overridable through `OWUI_ADMIN_EMAIL`, `OWUI_ADMIN_PASSWORD` and `OWUI_ADMIN_NAME`.

### Supported Open WebUI matrix

The versions the contract and e2e tests run against, one patch per supported minor, are defined once, as the `OWUI_MATRIX_<major>_<minor>` variables of `hack/openwebui/lib.sh`. `mise run contract`, `mise run e2e` and the CI jobs `contract` and `e2e` read them through `owui_matrix`, and Renovate bumps their patches. To add a version, add one variable with its Renovate annotation:

```bash
# renovate: datasource=docker depName=ghcr.io/open-webui/open-webui
OWUI_MATRIX_0_13="0.13.0"
```

Renovate holds a new minor for approval on the dependency dashboard; it would bump an existing variable, so add the new one by hand instead of approving it. A new minor also needs its API profile in `internal/client`, and the fixtures of every resource, recorded with `mise run contract:record 0.13.0` and `mise run owui:capture <resource> 0.13.0`.

### Contract tests

The contract tests, in `test/contract`, are Go tests of the client layer (`internal/client`) against a real Open WebUI instance. They build only with the `contract` tag. For each version, `hack/contract/run.sh` starts a fresh instance with `hack/openwebui/up.sh`, runs them, removes it, then does the same with an instance whose API keys are restricted to [the allow-list](docs/api-notes/api-key-allowed-endpoints.txt). CI runs them as a matrix job, one version per job.

```bash
mise run contract                   # every version of the matrix
mise run contract 0.12.4            # one version
mise run contract:record 0.12.4     # the same, then regenerate the fixtures the tests record
```

The recording mode replaces, under `test/fixtures/openwebui/v<version>/`, the fixtures of each resource whose contract records, once every test of that version passed. A recorder (`record` in `test/contract/harness_test.go`) sits between the client and the instance, and writes each exchange under the scenario and step the test names, in the format of the capture scenarios. `hack/capture/write.sh`, shared with `mise run owui:capture`, normalizes and redacts them, and refuses to write a fixture holding a secret. Recording again regenerates byte-identical fixtures, so `git diff` after a recording shows only what changed on the server.

Writing a contract test:

- Read the instance with `instanceFromEnv(t)`, passing `instanceDefault` or `instanceRestricted` to skip on the other kind. A resource contract runs on the restricted instance too, so that every route it needs is shown to pass the allow-list.
- To record the fixtures of a resource, create its recorder with `record(t, inst, "<resource>")`, send through `rec.transport(key)`, and name each exchange with `rec.begin("<scenario>")` and `rec.next("<step>")`. The resource's fixtures then belong to the contract: its capture scenario, if any, is removed. `credentials` is recorded this way; the other resources still come from `hack/capture/scenarios` until their contract lands. `endpoint-restrictions` and `endpoint-restrictions-control` stay with their capture scenarios until every resource has a contract: they record the allowed write of every resource, and destructive routes (`models/sync`, `models/delete/all`) a contract test does not send. `TestEndpointRestrictions` checks the cross-cutting part on every run.

### End-to-end tests

The e2e suite runs [chainsaw](https://kyverno.github.io/chainsaw/) tests from `test/e2e` against the provider built from the working tree, installed by Crossplane in a kind cluster, for each Open WebUI version of the [supported matrix](#supported-open-webui-matrix). CI runs it as a matrix job, one version per job.

```bash
mise run e2e                  # make build, cluster, provider, every version, then delete the cluster
mise run e2e 0.12.4           # one version
```

To iterate, keep the cluster and run the steps separately:

```bash
mise run e2e:up               # kind cluster, local registry, Crossplane (helm); safe to run again
make build && mise run e2e:deploy               # install or upgrade the provider from the last build
mise run e2e:test 0.12.4 --include-test-regex providerconfig   # extra flags go to chainsaw test
mise run e2e:down             # delete the cluster and the registry
```

A full run deletes the cluster at the end, even one created earlier by `e2e:up`; `E2E_KEEP_CLUSTER=true mise run e2e` keeps it.

How the pieces fit, in `hack/e2e`:

- The provider package is pushed to a registry container on the kind network. Crossplane pulls it from the registry's IP over plain HTTP, and containerd pulls the runtime image embedded in it through a `hosts.toml` written on each node. The tag is derived from the package content, so a rebuild rolls out a new revision.
- `e2e:test` starts a fresh Open WebUI instance with `hack/openwebui/up.sh` on the kind network, exposes it in the cluster as `http://open-webui.open-webui.svc:8080` (a Service with a manual EndpointSlice), runs chainsaw, and removes the instance on exit.

Writing a test for a new kind:

- Add a directory under `test/e2e` with a `chainsaw-test.yaml`. Chainsaw runs each test in its own namespace, so a test first creates the API key Secret and a `ProviderConfig` there, as `test/e2e/providerconfig` does, then its managed resources.
- The tests read the instance from `$values`: `url` (in-cluster, for provider configs), `apiUrl` (from the devcontainer, for direct API calls in `script` steps), `apiKey`, `version` and `profile` (e.g. `0.12`).
- The tests run one at a time, because they share the instance. On failure, `test/e2e/.chainsaw.yaml` prints the events of the test namespace and the provider's logs.

### Podman on macOS

- The bind-mounted workspace keeps the host UID (e.g. 501). `.devcontainer/on-create.sh` gives the `vscode` user that UID so it can write to the workspace; it does nothing where the UIDs already match.
- `devcontainer up` sometimes stops progressing right after `Not setting dockerd DNS manually.` (devcontainer CLI 0.87.0, Podman 5.8.2), with no child process running. Interrupt it and run the same command again: the second run reuses the started container and completes.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the contribution process and the coding and testing conventions, and [`SECURITY.md`](SECURITY.md) to report a vulnerability. Every commit is signed off under the [DCO](DCO).

## License

provider-openwebui is licensed under the [Apache License 2.0](LICENSE).
