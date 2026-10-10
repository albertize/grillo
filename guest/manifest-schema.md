# Portable guest boot inventory — schema 1

This is the D0 boot-loading contract, not a published release format, SBOM,
attestation or publisher signature. Original documentation is Apache-2.0.

## Layout and JSON

The installed inventory lives at
`lib/grillo/guest/<guest-abi>/manifest.json` relative to the application prefix.
Its artifact paths are relative to **the directory containing that manifest**,
never the invocation CWD or source checkout. Example:

```json
{
  "schema_version": 1,
  "guest_abi": "1.1",
  "platform": "linux/amd64",
  "artifacts": [
    {
      "name": "initramfs",
      "version": "<source/build-version>",
      "path": "initramfs.cpio.gz",
      "sha256": "<64 hexadecimal characters>",
      "size": 12345
    },
    {
      "name": "kernel",
      "version": "<kernel/build-version>",
      "path": "bzImage",
      "sha256": "<64 hexadecimal characters>",
      "size": 67890
    }
  ]
}
```

Digests and sizes above are placeholders, not verification evidence.

Requirements enforced by the loader:

- Schema version is exactly `1`. Unknown fields, duplicate JSON fields, trailing
  documents, input over 1 MiB and nesting over eight levels are rejected.
- Platform is exactly `linux/amd64`. Guest ABI is exactly the host's current
  protocol identifier (`1.1` at this delivery). This conservative metadata check
  is independent of package SemVer; actual guest handshake authentication and
  protocol negotiation still run at boot. No inferred backward compatibility.
- Exactly two distinct named artifacts: `kernel` and `initramfs`, with distinct
  paths, nonempty component versions, valid SHA-256 and size in `(0, 2 GiB]`.
  Agent/runc are transitively covered by the opaque image digest; their individual
  provenance/licenses still need the separate release component inventory.
- Paths are canonical relative slash-separated paths. Absolute paths, `.`/`..`,
  empty components, backslashes, NUL and drive-style colon syntax are rejected.
- Artifact reads reject special files and observed symlinks in the directory/
  artifact path. Portable reads use `os.Root` confinement; a traversal race
  cannot authorize a read outside that opened root. Hash reads are size-bounded.
- Moving the directory does not change serialized expectations. The loader
  anchors paths in private in-memory state; no build-time host path is serialized.

The backend pins inventory expectations at open, then verifies kernel/initramfs
into private operation snapshots before starting helpers or QEMU. Mutating the
source files or rewriting the inventory after open cannot silently replace the
pinned digests. Per-boot keys are overlaid only onto the private verified image;
see [ADR 0009](../docs/adr/0009-verified-boot-and-private-key-overlay.md).

## Development builder

Stage both files first; the builder does not copy, download or execute artifacts:

```sh
go run ./guest/manifest -portable -out "$guest_dir/manifest.json" \
  "kernel=$kernel_version=$guest_dir/bzImage" \
  "initramfs=$image_version=$guest_dir/initramfs.cpio.gz"
```

`guest_dir` should be an absolute, operation-owned staging directory. The tool
requires both source paths beneath it, hashes bounded regular files, sorts by
name and atomically publishes the inventory. It refuses extra fixture/key entries,
but **does not inspect opaque image contents or prove absence of embedded keys**.
Do not distribute the existing T07 fixture image by wrapping it in this manifest.

## Legacy compatibility

Unversioned development manifests retain their existing build-time path meaning.
Installed daemon defaults and installed `doctor` reject them. Only an explicit
`--artifact-manifest` / `GRILLO_GUEST_MANIFEST` developer override permits the
legacy schema. A declared but invalid schema, ABI or platform is rejected even
with that override; there is no fallback to legacy, another guest, downloads or
host containers. The direct component fixtures continue to use legacy inventories.

## Trust and remaining gates

The local inventory supplies operator-trusted digest expectations, not publisher
identity. A same-UID host attacker who controls the inventory before open can
replace its expectations. These checks do not expand same-user or compromised-
guest guarantees, audit the opaque image, resolve redistribution obligations or
make arbitrary QEMU/helper versions compatible.

Real moved-directory boot and regression evidence is in
[the D0 manifest report](../docs/experiments/d0-portable-manifest.md). Fixture-free
runtime image construction, secret scanning, complete installed Compose/Helm E2E,
helper integrity/provenance and clean-host acceptance remain open.
