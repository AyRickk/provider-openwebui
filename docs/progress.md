# Progress

This document is **rewritten, never appended to**. It describes the current state only; history lives in git and in the issues.

## Current phase

**Phase 1: foundation.** Phase 0 is closed: [ADR-0001](adr/0001-phase-0-api-consolidation.md) is accepted, with the maintainer's [decisions](adr/0001-phase-0-api-consolidation.md#decisions). Phase 1 builds what every resource needs, per spec #3 and ADR-0001's [phase 1 consequences](adr/0001-phase-0-api-consolidation.md#phase-1-foundation). The phase 1 foundation is done, with one follow-up left from the survey of the major providers (below); no resource kind exists yet, and `Group` opens phase 2. The three test harnesses of spec #3 are in place: the tracer bullet #28 (e2e, S3) runs the provider in kind under Crossplane, and the contract harness #29 (S1) runs the client layer against real instances, on both supported versions.

Done so far:

- The scaffold from `crossplane/provider-template` (#31).
- The HTTP transport of the client layer, `internal/client/transport` (#25): timeouts, retries, TLS, auth header, request counter, sentinel errors, redacted error details. Per-request `StatusErrors` override the default sentinels by status and, optionally, by detail.
- Version detection and API profiles, `internal/client` (#26):
  - `VersionCache` reads `GET /api/version`, cached per provider-config key for 5 minutes.
  - `SelectProfile` picks the 0.11 or 0.12 profile and applies the unknown-minor policy. An unknown minor reads `Open WebUI vX.Y.Z is not in the supported range [0.11, 0.12]`. With `allowUnsupported`, it returns the newest profile and reports the version unsupported.
  - `Profile.CheckCredentials` calls `GET /api/v1/users/default/permissions`. A bad key gives `ErrUnauthorized`. A non-admin key gives `ErrForbidden`: it also answers 401, recognized by its detail.
  - ADR-0001's not-found and conflict map is profile data: `Profile.StatusErrors(op)` and `Profile.MissingSucceeds(op)`. Resource tickets pass them into their requests.
- `ProviderConfig` and `ClusterProviderConfig` (#27):
  - Spec: `url`, `credentials`, `authHeader`, `caCertSecretRef`, `insecureSkipVerify`, `allowUnsupportedVersions`. A `ProviderConfig` reads its Secrets in its own namespace; a `ClusterProviderConfig` names theirs.
  - The credentials take the layout of the major providers (#43): `credentials.source`, an enum restricted to `Secret`, and `credentials.secretRef`, a local selector on `ProviderConfig` (ADR-0006) and a namespaced one on `ClusterProviderConfig`. `caCertSecretRef` stays beside `url` and `insecureSkipVerify`, the other TLS setting, rather than under a `tls` block. The README's "Provider configuration" section describes the layout.
  - `config.Connector`, in `internal/controller/config`, is shared by every controller. It reads the Secrets, keeps one `Doer` per provider config, detects the version, selects the profile and checks the credentials. `Connect(ctx, mg)` tracks the usage first, then resolves the managed resource's provider config. Resource controllers receive the connector in `Setup`, and the `provider.addtype` scaffold already uses it.
  - The provider config controllers report `status.version`, `status.profile` and the `Ready` condition, checked every poll interval. A `Warning` event is emitted when `Ready` turns to an error or to an allowed unsupported version.
- The baseline CI (#24):
  - `.github/workflows/ci.yml`, with every tool coming from `mise.toml` through `jdx/mise-action`. Its jobs:
    - lint: golangci-lint, `go vet`, and `make generate` leaving the tree clean;
    - Renovate configuration validation;
    - unit tests, through `hack/ci/test-coverage.sh`;
    - govulncheck;
    - a multi-arch build of the image and the xpkg, scanned by trivy (HIGH and CRITICAL with a fix), without publishing.
  - The coverage table per package goes to the job summary. The gate is 80% on `internal/client/...` and on every `internal/controller/<pkg>`, but not on the controller root, which only wires the controllers together.
  - `.github/workflows/scorecard.yml` runs OpenSSF Scorecard.
  - `.github/renovate.json5` watches Go modules, `mise.toml`, actions, Dockerfiles, and versions annotated with `# renovate: datasource=... depName=...`. The supported matrix in `hack/openwebui/lib.sh` annotates each Open WebUI version this way with `depName=ghcr.io/open-webui/open-webui`. Patch bumps are grouped, and a new minor waits for approval on the dependency dashboard.

- The e2e harness (#28), seam S3, in `hack/e2e` and `test/e2e` (see the README, "End-to-end tests"):
  - kind with Crossplane 2.4.3 (Helm), and a registry container on the kind network that serves the provider package to Crossplane and its runtime image to containerd;
  - a fresh Open WebUI per version from `hack/openwebui/up.sh`, reached in the cluster at `http://open-webui.open-webui.svc:8080`;
  - chainsaw tests reading the instance from `$values` (`url`, `apiUrl`, `apiKey`, `version`, `profile`); the first one, `test/e2e/providerconfig`, asserts a `ProviderConfig` is `Ready` with the version and profile;
  - `mise run e2e [version...]` for the whole run, `e2e:up`, `e2e:deploy`, `e2e:test` and `e2e:down` to iterate, and the CI job `e2e`, one matrix entry per version.
  - The Open WebUI scripts now use `127.0.0.1`: on kind's IPv6-enabled network, the port published on `::1` resets connections, and `localhost` resolves to `::1` first.
- The contract harness (#29), seam S1, in `test/contract` and `hack/contract` (see the README, "Contract tests"):
  - Go tests of `internal/client` behind the `contract` build tag, run by `hack/contract/run.sh` against a fresh default instance, then a fresh instance whose API keys are restricted to the allow-list, per version;
  - first contracts: version detection and the credential check (`TestCredentials`), and the allow-list of ADR-0001 (`TestEndpointRestrictions`);
  - a recording mode, `mise run contract:record [version...]`: a recorder in front of the instance writes the exchanges in the capture format, and `hack/capture/write.sh`, now shared with `owui:capture`, turns them into fixtures. `credentials` is recorded this way, byte-identical to the former capture; its bash scenario is gone;
  - `mise run contract [version...]`, and the CI job `contract`, one matrix entry per version.
- The contrib-ready repository files (#23): `LICENSE` (Apache-2.0), `DCO`, `CODE_OF_CONDUCT.md` (CNCF pointer), `OWNERS.md`, `SECURITY.md` (private vulnerability reporting, cosign verification of releases), the issue and PR templates with the DCO checkbox, and `CONTRIBUTING.md`, which holds the coding, testing, commit and PR conventions every ticket follows. `GOVERNANCE`, `MAINTAINERS` and `ADOPTERS` wait for a contrib proposal.
- The ADRs for the deviations from the original brief (#30), indexed in [`docs/adr/README.md`](adr/README.md): GitHub Actions and ghcr (0002), required immutable identifiers (0003), no `Group.members` (0004), adoption that fails without the annotation (0005), RBAC scope (0006), chainsaw instead of uptest (0007), the CRD reference with `crd-ref-docs` (0008). All are accepted. ADRs 0005 and 0006 were settled against a survey of the major Crossplane providers (2026-10-29), recorded in their Precedents sections: adoption refused without the annotation as a justified exception, a hand-set external-name accepted as an import on UUID kinds, no automatic adoption after a crash, Crossplane's default RBAC, the Secret cache on, no URL restriction. ADR-0007 corrects spec #3: uptest can call external APIs through its hooks; chainsaw is kept for the structure of the tests.
- The supported matrix is defined once, as `OWUI_MATRIX_<major>_<minor>` in `hack/openwebui/lib.sh`, read through `owui_matrix` by the contract and e2e runs and by a `matrix` CI job that feeds both matrix jobs. Adding a version is adding one annotated variable (README, "Supported Open WebUI matrix").
- The README documents the per-resource `crossplane.io/poll-interval` annotation and its relation to `--poll`, and the provider config `Ready` condition, specific to this provider (#45).

## Open phase 1 tickets

GitHub's issue dependencies are the source of truth for blockers; this table is a snapshot.

| Ticket | Title | Blocked by |
| --- | --- | --- |
| #44 | docs(security): ImageConfig example to verify releases | none, but needs the phase 4 release workflow |

Signed publishing to ghcr (cosign, SBOM, maintainer approval) is phase 4, with the release.

## Frontier

No ticket is ready. #44 waits for the release workflow of phase 4. Phase 2 (`Group`, then `Model`) starts once its tickets are opened under #3; the `Group` ticket builds on ADRs 0003, 0005 and 0006 (UUID identity, adoption and ownership, local references).

## Open questions

- GitHub private vulnerability reporting, which `SECURITY.md` names as the only reporting channel, is off on the repository. The maintainer turns it on under Settings, Code security.
- The spec names the file `OWNERS`; the repository uses `OWNERS.md`, the template's name.
- The coverage gate covers `internal/client/...` and `internal/controller/<pkg>`. Spec #3 also gates the normalization package: when it lands outside `internal/client`, add it to `hack/ci/test-coverage.sh` and to `CONTRIBUTING.md`.
- Spec #3 sets the default poll interval to 5 minutes; `cmd/provider-openwebui/main.go` keeps the template's `1m`, and the README documents `1m`. A resource ticket should align them, README included, or record the difference.
- The README's "Poll interval" section says the provider has no managed resource kind yet, and its example uses `Group`: the `Group` ticket removes that note and checks the example against the real kind. The annotation itself is read from crossplane-runtime's source, and no test covers it yet.
- [ADR-0004](adr/0004-no-group-members.md) and the limitations doc: the "Group membership" limitation section and its issue are still to be written, with the other limitation issues.
- Provider configs have no `Synced` condition, so an unknown minor shows there as `Ready=False`. Managed resources get `Synced=False` from the connector's error, with the same message behind the runtime's `connect failed:` prefix. `Connect` reports an allowed unsupported version through `Connection.Unsupported`; only the provider config emits the `Warning` event, so resource controllers need not emit one.
- 0.11.4 answers a function update with the same `400 "[ERROR: Error updating function]"` for a missing function and for invalid code, so the 0.11 profile maps it to no sentinel. The `Function` ticket must decide existence with a get, as ADR-0001 already requires, and surface the detail as is.
- The profiles are tested directly, against the fixtures, because the resource `Client` interface (seam S1) does not exist yet. Spec #3 wants profiles tested only through S1, so these tests move behind it when the first resource lands.
- Each resource's contract should record its fixtures and replace its capture scenario. `endpoint-restrictions` and `endpoint-restrictions-control` stay captured until every resource has a contract, and each resource contract should also run on the restricted instance.
- Renovate holds a new Open WebUI minor for approval, but it would bump an existing `OWUI_MATRIX_*` variable: a new minor is added by hand as its own variable. A per-minor `allowedVersions` rule would make this automatic.
- The connector runs the credential check on every connection, including each managed resource's reconcile: one extra GET per reconcile. If the request counter shows it matters, cache the check alongside the version.
- The Renovate GitHub app must be installed on the repository by the maintainer for the configuration to take effect. Dependabot security updates are still on, so the two can open overlapping PRs. The maintainer can turn Dependabot off once Renovate runs.
- `npm:renovate` cannot go in `mise.toml`: mise's npm trust policy refuses a transitive dependency (`@yarnpkg/libzip@3.2.2`) whose provenance attestation was dropped. CI runs the validator from the Renovate image, pinned by digest. Revisit when upstream republishes with provenance.
- Trivy fails the build on any HIGH or CRITICAL CVE with a fix, so a CVE published upstream can turn an unrelated PR red until the dependency is bumped. This is the release requirement of spec #3, applied to every PR.
- The e2e suite runs on amd64 in CI and on arm64 in the devcontainer on Apple silicon; the image of each arch is built for the host only.
- The `crossplane` CLI in `mise.toml` (2.5.0) only builds and pushes the package, while the cluster runs Crossplane 2.4.3, the version spec #3 tests. A Renovate rule makes a new Crossplane chart minor wait for approval.
- `hack/openwebui/up.sh` pulls `v<version>-slim`, while the matrix annotates the bare version for Renovate (`docker` versioning). Whether Renovate's tag lookup follows the `v…-slim` family should be checked on its first Open WebUI PR.
- On a 3.7 GiB Podman VM, kind, Crossplane and one Open WebUI fit; starting a second Open WebUI next to them froze the VM.
- `hack/helpers/addtype.sh` is committed without the executable bit, and it needs `GOMPLATE` set, so `make provider.addtype` fails as documented in the README. Running `GOMPLATE=$(command -v gomplate) bash ./hack/helpers/addtype.sh` with the same variables works.
- `shellcheck` is not installed in the devcontainer or CI, so the shell scripts are not linted.
