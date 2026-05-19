# HDR Thumbnail Implementation TODO

## Rules

- [ ] Do not edit `SCOPE.md`.
- [ ] Do not edit `PLAN.md` except by explicit user/orchestrator instruction.
- [ ] Implementation subagents must not edit this `TODO.md`.
- [ ] Reviewer subagents must not edit files.

## Milestone 1: Fixtures and Detection

- [ ] Select 2–3 HDR fixture images from current `tasercake-cms` `photos` collection.
- [ ] Add fixture image files under `tests/data/hdr/`.
- [ ] Add metadata snapshots under `tests/data/hdr/`.
- [ ] Add `docs/hdr-thumbnails/fixture-analysis.md`.
- [ ] Add pure-Go HDR detection package skeleton under `tools/hdrthumb/`.
- [ ] Add pure-Go detector tests for fixture images.

## Milestone 2: File Field Policy and Admin UI

- [ ] Extend `core.FileField` with HDR thumbnail policy fields.
- [ ] Add defaulting/validation/backward-compatible import/export behavior.
- [ ] Add Go tests for field policy validation and schema compatibility.
- [ ] Add Admin UI controls for HDR policy on file fields.
- [ ] Add/update UI metadata/help for new file-field options.

## Milestone 3: Thumbnail Routing, Errors, Cache, and Storage

- [ ] Add `CreateThumbWithOptions` or equivalent thumbnail options path.
- [ ] Pass effective file-field HDR policy from `apis/file.go` to filesystem thumbnail generation.
- [ ] Implement typed HDR-required errors.
- [ ] Change API behavior so HDR-required failures return non-2xx without fallback/original serving.
- [ ] Add HDR-aware cache namespace/path selection.
- [ ] Update file delete/replace cleanup for HDR cache namespaces.
- [ ] Add MIME eligibility/inline serving support for supported HDR formats.
- [ ] Add tests for routing, failures, cache behavior, original preservation, view behavior, and cleanup.

## Milestone 4: Native HDR Backend and Deterministic Build

- [ ] Add deterministic native dependency scripts under `scripts/hdrthumb/`.
- [ ] Add build-tagged HDR backend stubs and disabled-backend behavior.
- [ ] Build or wrap `libultrahdr` programmatically.
- [ ] Implement first working HDR thumbnail generation path for detected fixture format.
- [ ] Add HDR backend tests proving generated thumbnail remains HDR-capable.

## Milestone 5: Documentation and Deployment

- [ ] Add `docs/hdr-thumbnails/overview.md`.
- [ ] Add `docs/hdr-thumbnails/build.md`.
- [ ] Add `docs/hdr-thumbnails/testing.md`.
- [ ] Add `docs/hdr-thumbnails/operations.md`.
- [ ] Verify default build/tests still pass.
- [ ] Verify HDR build/tests pass.
- [ ] Deploy HDR-enabled binary to `tasercake-cms`.
- [ ] Enable `photos.image` HDR policy = require.
- [ ] Verify live `?thumb=1200x0` URL returns HDR-capable thumbnail from R2-backed storage.

## Milestone 6: PR and Review

- [ ] Push final implementation branch.
- [ ] Open GitHub PR.
- [ ] Run one reviewer subagent to leave comments directly on the PR.
- [ ] Address required PR review feedback.
