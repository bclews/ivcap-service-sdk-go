# Go SDK for IVCAP Services

This library is a simple SDK for developing services to be deployed on IVCAP.

## Publishing artifacts

When publishing to the platform, `Environment.Publish` requires an artifact with status `ready` before attaching optional metadata. Check the returned error and exit non-zero if publication fails. `PublishAsync` does not return publication errors to the caller.

With `LocalMode(true)`, `Publish` writes a local file instead, skips metadata, and currently ignores read and write errors.

Readers with unknown lengths use chunked transfer encoding, which may fail on older gateways. The SDK does not buffer these readers. A non-empty `bytes.Buffer` provides a known content length and avoids this limitation.

## Configuration

Set `IVCAP_BASE_URL` to the platform API address (default: `http://ivcap.local`). The deprecated `IVCAP_STORAGE_URL` is used only when `IVCAP_BASE_URL` is unset or empty.

Earlier SDK versions uploaded to a sidecar storage endpoint that is no longer supported. If you still use `IVCAP_STORAGE_URL`, its value must now point to the platform API, not the old storage endpoint.
