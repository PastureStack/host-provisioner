# Security Policy

## Supported state

This repository is under migration review and is not release-ready.

## Security boundaries

- Machine-driver downloads are executable code and must have reviewed URLs and checksums.
- Driver cache and installation paths are rooted with `os.Root`; cached names and archive entries are validated before any filesystem operation. Archives are parsed in-process, accept one regular driver binary, and reject traversal, links, devices, and special files.
- Cloud credentials, registration tokens, API keys, host templates, and machine configuration are sensitive.
- Host filesystem mounts, the Docker socket, and bootstrap containers grant elevated access.
- Do not commit credentials, driver binaries, private endpoints, host state, or live event payloads.

## Reporting

Report suspected vulnerabilities through this repository's private security advisory channel. Do not include cloud credentials or production host data in a public issue.

## Build-image scan decisions

The source tree and shipped CGO-disabled product binary must contain no Critical or High vulnerability and no secret. The disposable Ubuntu builder is also scanned, but is not shipped or run as the product.

Critical or High records in the builder are accepted only when every record is an unfixed `affected` finding for the Ubuntu `linux-libc-dev` package. That package contains user-space API headers needed by GCC and Go race tests, not the vulnerable Linux kernel implementation. The gate reports the raw count and fails closed for any other package, fixed finding, status, package URL, or secret. This categorical rule follows the build boundary and does not require a version-specific list that becomes stale on each Ubuntu package refresh.
