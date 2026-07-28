# HDR thumbnail operations

## Enabling a file field

In the PocketBase admin UI, edit the file field and set:

- Generate HDR thumbnails for HDR images: enabled.
- HDR thumbnail policy: `Require HDR output`.

For the target deployment this is `photos.image` with `hdrThumbs=true` and `hdrThumbsPolicy=require`.

## Deployment to `tasercake-cms`

1. Build and test locally or on the VM.
2. Copy or build an HDR-enabled binary on the VM.
3. Back up data before replacing the service binary:

```sh
sudo systemctl stop pocketbase
sudo tar -C /var/lib/pocketbase -czf /var/backups/pocketbase/pb_data.$(date +%Y%m%d%H%M%S).tgz pb_data
sudo cp /opt/pocketbase/pocketbase /opt/pocketbase/pocketbase.backup.$(date +%Y%m%d%H%M%S)
sudo cp ./pocketbase /opt/pocketbase/pocketbase
sudo systemctl start pocketbase
```

4. Run the versioned gallery backfill after the new binary and helper are available:

```sh
pocketbase gallery-thumbs backfill --workers=4
```

The backfill is idempotent. It processes published photos with bounded concurrency, resumes missing or invalid variants on retry, and never deletes an older generation.

If the helper is not under the service working directory, configure systemd with:

```text
Environment=HDRTHUMB_HELPER=/opt/pocketbase/.deps/hdrthumb/bin/hdrthumb-helper
```

Then run:

```sh
sudo systemctl daemon-reload
sudo systemctl restart pocketbase
```

## Versioned cache behavior

Progressive Ultra HDR gallery variants use an immutable generation path:

```text
thumbs_hdr_{filename}/uhdr-pjpeg-v1/{size}_{filename}
```

Objects include generation metadata and:

```text
Cache-Control: public, max-age=31536000, immutable
```

Legacy HDR thumbnails remain under:

```text
thumbs_hdr_{filename}/{size}_{filename}
```

Newly published or updated photos eagerly generate their complete set on the write request. A small `_ready` manifest is written only after all `400x0`, `1200x0`, and `2000x0` objects pass Ultra HDR full decode, progressive-primary, multi-scan, ICC, MPF/XMP/ISO metadata, dimensions, chroma, highlights, clipping, and cache validation. Gallery reads use legacy URLs until that manifest exists. Reads check the persistent manifest and object attributes instead of synchronously rebuilding or downloading every variant. After readiness, both gallery CDN URLs and normal `/api/files` requests switch to the versioned generation.

## Rollback and cleanup

Rollback can point clients at retained legacy objects. Do not purge legacy or prior generation objects during deployment or backfill. CDN TTLs and active clients may still reference them.

Cleanup is a separate, explicit later operation after compatibility validation and the rollback window. Scope deletion to a named obsolete generation; never delete the whole `thumbs_hdr_{filename}/` prefix.

## Verification checklist

- PocketBase service is healthy.
- `photos.image` has `hdrThumbs=true` and `hdrThumbsPolicy=require`.
- `pocketbase gallery-thumbs backfill --workers=4` reports `failed=0`.
- R2 contains all three `uhdr-pjpeg-v1` objects for every published photo.
- Objects have immutable cache control and `pocketbase-thumb-generation=uhdr-pjpeg-v1` metadata.
- First primary SOF is `FFC2`, with multiple primary SOS scans and 4:2:0 sampling.
- Gain-map JPEG remains baseline and `hdrthumb-helper probe` succeeds.
- MPF, XMP, and ISO 21496 metadata remain present.
- Original object hash and retained legacy object hashes are unchanged.
