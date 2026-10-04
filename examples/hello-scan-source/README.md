# Hello Scan Source

A minimal `scan_source.v1` plugin for Silo Autoscan. Another tool appends
changed paths to a change-log file, one per line, and each poll returns the
lines added since the previous poll.

It demonstrates:

- a self-describing binary: embedded manifest, checksum computed at runtime
- a setup descriptor in `metadata.scan_source` with one per-source setting
  (`changes_file`) that the Add-source flow renders as a text field
- reading that setting from `source_config` on every `PollChanges`
- marker handling: an empty marker starts from now, the marker is the number
  of lines consumed, and a marker past the end of the file (truncated or
  rotated log) resynchronizes instead of replaying the file
- structured `changes`: a line ending in `/` is a `SUBTREE` change, anything
  else is a `FILE` change

See [docs/scan-source.md](../../docs/scan-source.md) for the full contract.

## Build

Build for the platform your Silo server runs on, usually Linux:

```sh
GOOS=linux GOARCH=amd64 go build -o hello-scan-source ./examples/hello-scan-source
```

## Inspect the manifest

On a matching platform:

```sh
./hello-scan-source manifest
```

## Test

```sh
go test ./examples/hello-scan-source
```

## Try it in Silo

1. Upload the binary in **Admin → Plugins**.
2. In **Admin → Libraries → Autoscan**, enable Autoscan, choose **Add
   source**, pick **Change log file**, and enter the log path as the Silo
   server sees it, for example `/data/silo-changes.log`.
3. Wait for one poll so the source records its starting position. Lower the
   global poll interval in the Autoscan settings to make this quicker.
4. Append a path inside one of your libraries:

   ```sh
   echo "/data/movies/Example (2024)/Example (2024).mkv" >> /data/silo-changes.log
   ```

   After the next poll, the source's activity log shows the change and the
   scan it queued.

The file must be readable from inside the Silo server's environment, such as
its container.
