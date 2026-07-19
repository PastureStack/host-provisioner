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

The disposable Ubuntu builder is scanned both raw and with the repository's OpenVEX document. A `not_affected` statement is accepted only when the complete raw Critical/High vulnerability-ID and package-PURL set exactly matches every reviewed statement. Any additional finding, changed package version, missing statement, secret, or product finding fails the release gate.

The reviewed `linux-libc-dev` package contains user-space API headers needed by GCC and Go race tests, not the vulnerable Linux kernel implementations. The builder is neither shipped nor run as the product, and no OpenVEX statement is applied to the source tree or product binary.
