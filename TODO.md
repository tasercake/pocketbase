# HDR Thumbnail Implementation TODO

## Rules

- [ ] Do not edit `SCOPE.md`.
- [ ] Do not edit `PLAN.md` except by explicit user/orchestrator instruction.
- [ ] Implementation subagents must not edit this `TODO.md`.
- [ ] Reviewer subagents must not edit files.

## Milestone 1: Fixtures and Detection

- [x] Select 2–3 HDR fixture images from current `tasercake-cms` `photos` collection.
- [x] Add fixture image files under `tests/data/hdr/`.
- [x] Add metadata snapshots under `tests/data/hdr/`.
- [x] Add `docs/hdr-thumbnails/fixture-analysis.md`.
- [x] Add pure-Go HDR detection package skeleton under `tools/hdrthumb/`.
- [x] Add pure-Go detector tests for fixture images.

## Milestone 2: File Field Policy and Admin UI

- [x] Extend `core.FileField` with HDR thumbnail policy fields.
- [x] Add defaulting/validation/backward-compatible import/export behavior.
- [x] Add Go tests for field policy validation and schema compatibility.
- [x] Add Admin UI controls for HDR policy on file fields.
- [x] Add/update UI metadata/help for new file-field options.

## Milestone 3: Thumbnail Routing, Errors, Cache, and Storage

- [x] Add `CreateThumbWithOptions` or equivalent thumbnail options path.
- [x] Pass effective file-field HDR policy from `apis/file.go` to filesystem thumbnail generation.
- [x] Implement typed HDR-required errors.
- [x] Change API behavior so HDR-required failures return non-2xx without fallback/original serving.
- [x] Add HDR-aware cache namespace/path selection.
- [x] Update file delete/replace cleanup for HDR cache namespaces.
- [x] Add MIME eligibility/inline serving support for supported HDR formats.
- [x] Add tests for routing, failures, cache behavior, original preservation, view behavior, and cleanup.

## Milestone 4: Native HDR Backend and Deterministic Build

- [x] Add deterministic native dependency scripts under `scripts/hdrthumb/`.
- [x] Add build-tagged HDR backend stubs and disabled-backend behavior.
- [x] Build or wrap `libultrahdr` programmatically.
- [x] Implement first working HDR thumbnail generation path for detected fixture format.
- [x] Add HDR backend tests proving generated thumbnail remains HDR-capable.

## Milestone 5: Documentation and Deployment

- [x] Add `docs/hdr-thumbnails/overview.md`.
- [x] Add `docs/hdr-thumbnails/build.md`.
- [x] Add `docs/hdr-thumbnails/testing.md`.
- [x] Add `docs/hdr-thumbnails/operations.md`.
- [x] Verify default build/tests still pass.
- [x] Verify HDR build/tests pass.
- [x] Deploy HDR-enabled binary to `tasercake-cms`.
- [x] Enable `photos.image` HDR policy = require.
- [x] Verify live `?thumb=1200x0` URL returns HDR-capable thumbnail from R2-backed storage.

## Milestone 6: PR and Review

- [x] Push final implementation branch.
- [x] Open GitHub PR.
- [x] Run one reviewer subagent to leave comments directly on the PR.
- [x] Address required PR review feedback.
