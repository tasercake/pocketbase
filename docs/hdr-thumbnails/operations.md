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

If the helper is not under the service working directory, configure systemd with:

```text
Environment=HDRTHUMB_HELPER=/opt/pocketbase/.deps/hdrthumb/bin/hdrthumb-helper
```

Then run:

```sh
sudo systemctl daemon-reload
sudo systemctl restart pocketbase
```

## Cache behavior

HDR-required thumbnails for detected HDR sources are stored separately from standard thumbnails:

```text
thumbs_hdr_{filename}/{size}_{filename}
```

Standard thumbnails remain under:

```text
thumbs_{filename}/{size}_{filename}
```

This separation prevents a previously generated SDR thumbnail from satisfying an HDR-required request.

## Clearing and regenerating thumbnails

Delete the relevant thumbnail prefix from the active storage backend, then request the file URL again.

For local filesystem storage, remove the matching directory below the record files path. For S3/R2 storage, delete keys matching:

```text
{collectionId}/{recordId}/thumbs_hdr_{filename}/
```

The next `?thumb=` request regenerates and stores the thumbnail through the configured storage backend.

## Verification checklist

- `systemctl status pocketbase` is healthy.
- `photos.image` has `hdrThumbs=true` and `hdrThumbsPolicy=require`.
- `curl -fL '?thumb=1200x0'` returns `image/jpeg`.
- `hdrthumb-helper probe` succeeds on the downloaded thumbnail.
- R2 contains `thumbs_hdr_{filename}/1200x0_{filename}`.
- Original object hash matches the pre-deploy hash.
- A non-HDR image thumbnail still returns successfully.
