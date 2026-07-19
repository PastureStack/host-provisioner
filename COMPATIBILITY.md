# Compatibility Contract

The migration preserves machine-driver, physical-host, agent, registration-token, dynamic-schema, and publish-reply event contracts consumed by existing control-plane data.

Preferred settings use `PLATFORM_*` and `PASTURESTACK_HOME`. Historical `CATTLE_*` variables, generated `RancherClient` types, legacy bootstrap container names and mount paths, `io.rancher.*` labels, and vendored `github.com/rancher/*` imports remain only where required for event, state, or inherited dependency compatibility.

Operator lifecycle messages support `en-US` and `zh-TW`. Event names, transition messages, driver schemas, machine fields, labels, and API errors are not translated.

Before release, validate driver activation and checksum failure, machine create/bootstrap/remove, host-template secrets, node-agent discovery, retries, jail isolation, rollback, and cleanup in an isolated VM environment.

Machine names remain optional and fall back to the immutable external identifier. Legitimate legacy names, including spaces and non-ASCII characters, are preserved. Names that could be interpreted as paths, control characters, or command options are rejected before any command or file-system operation. Unusual historical external identifiers receive a deterministic local storage key without changing their control-plane value.

Restored configuration archives accept regular files and directories only. Every entry must be a local relative path within the machine storage root; absolute paths, parent traversal, backslash aliases, symbolic links, hard links, devices, and other special files are rejected.
