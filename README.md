# PastureStack Host Provisioner

Host Provisioner handles machine-driver and physical-host lifecycle events, installs reviewed machine drivers, creates hosts, and bootstraps compatible node agents.

PastureStack is an independent community effort to preserve, audit, and modernize the Rancher 1.6 ecosystem. It is not affiliated with or endorsed by Rancher Labs or SUSE.

**Upstream:** [`rancher/go-machine-service`](https://github.com/rancher/go-machine-service). This GitHub fork preserves upstream history, authorship, dates, tags, licenses, and bundled dependency notices; PastureStack maintenance is consolidated into one commit after the preserved upstream boundary.

## Project status

The current reviewed compatibility release is `v0.39.7`, based on the preserved upstream `v0.39.4` boundary and consumed by PastureStack Server `v1.6.410`. Existing Ubuntu 26.04, Go 1.27.0, dependency, driver checksum, jailer, and test maintenance is retained. The build container contains only the tools used by compilation and tests; Docker remains on the outer build runner and is not duplicated inside the container. Product-owned imports, executable names, state defaults, router identities, and operator messages use PastureStack naming. Machine storage is constrained to its configured root and restored configuration archives reject traversal, links, and special files.

## Configuration

Use `PLATFORM_URL`, `PLATFORM_ACCESS_KEY`, `PLATFORM_SECRET_KEY`, and `PASTURESTACK_HOME`. Existing `CATTLE_*` variables remain temporary compatibility aliases for established registration and event contracts. Set `PASTURESTACK_LOCALE=en-US` or `zh-TW` for operator lifecycle messages.

## Build and test

From a Docker-capable Linux host:

```sh
make test
make build
make package
```

Dependencies are locked by `go.mod` and `go.sum`, and builds use the checked-in `vendor/` tree without network access. The machine RPC layer follows the maintained GitLab Docker Machine fork at `v0.16.2-gitlab.52`; only the packages used by Host Provisioner are shipped. The Rancher 1.6 event and API clients are retained as explicit compatibility modules under `third_party/`, so their frozen protocol surface is reviewable without treating abandoned upstream repositories as live package sources.

The build container does not receive the host Docker socket. For the reviewed Server release, build with `VERSION_OVERRIDE=v0.39.7` and `SOURCE_DATE_EPOCH=0`; packaging produces the deterministic Release asset `host-provisioner-0.39.7-linux-amd64.tar.xz`. The archive contains the executable, root license, origin record, and preserved bundled-dependency legal files. PastureStack Server downloads that asset from its versioned GitHub Release and verifies its SHA-256 digest before installation. Users do not need to host a package mirror.

See [COMPATIBILITY.md](COMPATIBILITY.md), [SECURITY.md](SECURITY.md), and [ORIGIN.md](ORIGIN.md).

## License and attribution

The inherited project remains licensed under [Apache License 2.0](LICENSE). Copyright and attribution for inherited work and vendored dependencies remain with their respective authors and contributors. PastureStack contributors claim authorship only for their own changes.
