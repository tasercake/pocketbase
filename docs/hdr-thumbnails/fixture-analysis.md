# HDR thumbnail fixture analysis

Milestone 1 selected the first three published records from the live `photos` collection on `tasercake-cms` (queried through `http://127.0.0.1:18090`, via SSH tunnel when the local proxy was not already running). Originals were downloaded from the public PocketBase file API and stored under `tests/data/hdr/`.

## Summary

All three fixtures are JPEG_R / Ultra HDR gain-map JPEGs. The key evidence is consistent across the set:

- JPEG APP2 marker with `MPF` multi-picture metadata (`exiftool`: `MPF0:NumberOfImages = 2`).
- JPEG APP2 marker with `urn:iso:std:iso:ts:21496:-1`, the ISO 21496 / JPEG_R Ultra HDR namespace.
- XMP gain-map metadata (`exiftool`: `XMP-hdrgm:Version = 1.0`).
- Baseline SDR-compatible JPEG primary image with an embedded gain-map image, so future HDR thumbnail output should remain `image/jpeg` / Ultra HDR JPEG.

## Fixtures

| Fixture                              | PocketBase record ID | Source filename                                | Dimensions | Source MIME | SHA-256                                                            | Detected HDR kind           | Expected output MIME |
| ------------------------------------ | -------------------- | ---------------------------------------------- | ---------- | ----------- | ------------------------------------------------------------------ | --------------------------- | -------------------- |
| `tests/data/hdr/current-photo-1.jpg` | `lm2cd79akflfki0`    | `2026_01_19_20_47_08_b_r8_s4_2_1giuf91py2.jpg` | 2897x3863  | image/jpeg  | `18090b5d9a357320499ff3ecd1b9a885d280f24b3dc089d99cf6a478c10a5761` | Ultra HDR / JPEG_R gain map | image/jpeg           |
| `tests/data/hdr/current-photo-2.jpg` | `67dge9kj935kiha`    | `2026_01_19_20_47_08_b_r8_s4_nn6ltqep9e.jpg`   | 3019x4025  | image/jpeg  | `b742e970315ceed6cf9cc501314de5cbf8bc67c908299066fbdeac258e7005b3` | Ultra HDR / JPEG_R gain map | image/jpeg           |
| `tests/data/hdr/current-photo-3.jpg` | `cx30n2q919ixpao`    | `2026_01_19_21_08_04_b_r8_s4_cu006bs2kp.jpg`   | 2646x1984  | image/jpeg  | `b00422db0e702d27386f3edf38ad93d35864b6c056b1d01663d8d1cd12f30827` | Ultra HDR / JPEG_R gain map | image/jpeg           |

## Per-fixture evidence files

Each fixture has immutable sidecar snapshots:

- `current-photo-N.exiftool.json` from `exiftool -a -G1 -s -json`.
- `current-photo-N.identify.txt` from `identify -verbose`.
- `current-photo-N.markers.txt` from the pure-Go JPEG marker scanner in `tools/hdrthumb`.

Representative marker evidence from `current-photo-1.markers.txt`:

```text
0x0000a189 0xFFE1 APP1  len=944   http://ns.adobe.com/xap/1.0/
0x0000a53b 0xFFE2 APP2  len=88    MPF
0x0000a595 0xFFE2 APP2  len=34    urn:iso:std:iso:ts:21496:-1
```

Representative ExifTool evidence:

```text
XMP-hdrgm:Version = 1.0
MPF0:MPFVersion = 0100
MPF0:NumberOfImages = 2
```

## Expected verification commands

```sh
exiftool -a -G1 -s -json tests/data/hdr/current-photo-1.jpg
identify -verbose tests/data/hdr/current-photo-1.jpg
go test ./tools/hdrthumb
```

For future generated thumbnails, verify that the thumbnail remains `image/jpeg` and still contains MPF + ISO 21496/JPEG_R + gain-map metadata markers instead of a flattened SDR-only JPEG.
