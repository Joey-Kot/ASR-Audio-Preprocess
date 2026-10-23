# Dependency security audit — 2026-10-23

Baseline: `72922741cf434050545ccc31dd17151cc8f79dcc`.

## Result and updates

The enabled MOV demuxer exposes **CVE-2026-107678** when opening a crafted
MP4/M4A/MOV file. The original FFmpeg 8.1.2 and the unmodified 8.1.3 release
both recursively free encryption-init linked lists and repeatedly deserialize
and serialize accumulated `pssh` records. Audio-only decoding does not prevent
the demuxer from parsing this metadata.

Updated FFmpeg to **8.1.3 plus the vendored PR 24593 backport** and Go from
**1.26.4 to 1.27.3**. CI now reads the Go version from `go.mod`; it previously
requested 1.22. Opus **1.5.2** is retained: no applicable published CVE was
identified for that version in this audit.

## Go scan

`go.mod` has no external module dependencies. `govulncheck` 1.9.0 used
`https://vuln.go.dev`, updated 2026-10-21 00:27:29 UTC.

The baseline Go 1.26.4 source scan found **zero reachable vulnerable symbols**,
two imported-package advisories (`GO-2026-4970`, `GO-2026-6604`, both concerning
`os.Root`), and 22 additional standard-library module advisories. The application
does not call the vulnerable `os.Root` functions. These package/module matches
are not counted as exploited or reachable application defects.

After updating to Go 1.27.3, both the default and `-tags libav` scans reported
**no vulnerabilities**, including package/module findings. Go 1.27.3 includes
the latest `crypto/x509` security correction described in the
[Go release history](https://go.dev/doc/devel/release#go1.27.3).

## Native-library triage

Queried NVD's FFmpeg 8.1.2 CPE/version candidates (35 records), recent FFmpeg
keyword records (2026-07-01 through 2026-10-23), and the Opus product/version.
Checked the generated `config.h` / `config_components.h`, upstream fixes, and
the relevant source paths. Keyword results for applications that merely call
FFmpeg were excluded after checking their product identity.

| Finding | Result for this build |
|---|---|
| CVE-2026-107678 | Applicable; fixed by the local three-commit backport. |
| CVE-2026-96611, MOV/HEIF dimension overflow | The cited fix is already in 8.1.2 and 8.1.3; the upstream patch reverse-checks against 8.1.3. |
| CVE-2026-52296, WMA extradata padding | WMA encoders are disabled here; the padding fix is also already present in both releases. |
| CVE-2026-52295, CVE-2026-52297, CVE-2026-66037 | IAMF and its MOV parsing/writing paths are disabled (`CONFIG_IAMF`, `CONFIG_IAMFDEC`, `CONFIG_IAMFENC` are 0). |
| CVE-2025-25468, CVE-2025-25469 | The referenced `d5873be` IAMF failure-path cleanup is present; IAMF is disabled. The NVD description is broader than the referenced change. |
| CVE-2026-107660, CVE-2026-107675, CVE-2026-107677, CVE-2026-107695–107698 | TLS/SFTP/DASH/HLS/RTSP paths are disabled. |
| CVE-2026-107676, HDR10+ serialization | No video output or Matroska muxer is enabled; audio conversion does not call the HDR serializer. |
| CVE-2026-64830–64835, CVE-2026-65703–65706, CVE-2026-66036, CVE-2026-66038–66041, CVE-2026-70628–70632 | Their subtitle, hardware/video filter/decoder, S/PDIF, RTP/ASF, ADX, MACE6, TY or Shorten components are disabled. Enabling concat alone does not enable TY/Shorten. |
| CVE-2025-52290, H.264 reorder-frame handling | The demuxer call is guarded by `CONFIG_H264_DECODER`, which is 0. |
| Opus 1.5.2 | No applicable CVE found; opusfile, PJSIP and other products' Opus integration defects do not describe this library. |

The upgrade also includes upstream fixes in enabled Ogg, MOV, concat and
audio conversion paths listed in the 8.1.3 release `Changelog`.

## Source integrity and backport

Archive: [ffmpeg-8.1.3.tar.gz](https://ffmpeg.org/releases/ffmpeg-8.1.3.tar.gz).

SHA-256: `bd458826a039b48a9606e794554c75eb4c4984b84173f7afa3128eae89336f2b`.

The archive's detached signature was verified against the pinned release-key
fingerprint `FCF986EA15E6E293A5644F10B4322F04D67658D8`. Both FFmpeg and Opus
checksums are now defaults in the bootstrap, rather than depending on CI env.

Vendored patch: `scripts/patches/ffmpeg-8.1.3-cve-2026-107678.patch`.
It backports all three commits from
[upstream PR 24593](https://code.ffmpeg.org/FFmpeg/FFmpeg/pulls/24593):

- `969c7eb091989a664cb7bf66a1f02051cc46cb52`: iterative linked-list free.
- `d1434518fbd03789e3e5dc0657a7a9f1123ce074`: serialized append API and unit test.
- `71403a21b02a92b925e26e609e9bd3e48df451cf`: amortized linear MOV `pssh` append.

The master-branch API version bump and APIchanges entry are omitted for this
private static 8.1.3 build. The implementation is unchanged; the unit test adds
a 100,000-node free case. Both build entry points apply and verify the patch,
accept a fully patched source tree on rebuild, and reject partial/mismatched
sources. CI also tests these three cases and the MOV regression.

## Validation performed

Linux x86_64, Go 1.27.3:

- Built Opus and patched FFmpeg from the pinned archives.
- `make fate-encryption-info`: passed, including upstream append and long-list tests.
- MOV regression: 32,768 + 7 `pssh` records across two tracks; values, counts and order verified.
- Patch application tests: fresh apply, repeat apply, and fail-without-mutation on partial patches passed.
- `go test ./...` and `go test -tags libav -race ./...`: passed.
- Default and libav `govulncheck -show verbose ./...`: no findings after upgrade.
- Static release CLI built successfully. Existing C `sample_fmts` deprecation warnings remain.
- Workflow checked with actionlint; shell syntax and Git whitespace checks passed.

Windows/ARM64 builds require the repository CI; they were not executed locally.
These results cover the pinned application dependencies and configured native
features, using the advisory database snapshots above.

Primary references: [CVE-2026-107678](https://nvd.nist.gov/vuln/detail/CVE-2026-107678),
[MOV dimension fix](https://code.ffmpeg.org/FFmpeg/FFmpeg/commit/059eb2e853c1f62b8df1b73be9318061a7a21b0b),
[WMA padding fix](https://code.ffmpeg.org/FFmpeg/FFmpeg/commit/23227a444de4a8f7696f46660cdd044b460f7e47),
[Opus 1.5.2](https://opus-codec.org/release/stable/2024/04/12/libopus-1_5_2.html).
