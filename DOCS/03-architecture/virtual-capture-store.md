# Virtual capture store — VM-2

Status: storage, configuration, submission/readiness, inspector, asynchronous
preview rendering, and desktop controls are implemented through VM-8. VM-9
adds live-listener integration coverage. Native tray/browser and installer
checks remain release follow-up. Physical remains the default.

## Ownership and lifetime

`internal/capture` contains the concurrency-safe session store. The long-lived
`server.Server` creates one store, exposes it through `CaptureStore()`, and
retains it across HTTP listener Stop/Start/Restart. The desktop application's
`Run` closes the server, preview workers, and store on exit. Nothing is saved
to disk or ordinary request logs.

The store implements the `Destination` methods without dialing a printer.
The router selects this store whenever the saved destination is Virtual,
regardless of supplied printer targets or injected physical transports.
`/print` calls Capture to return the new ID; `/status` checks store availability.
`Options.CaptureStore` supplies the shared application store; standalone routers
create a private store when omitted. Physical startup uses TCP.

## Captured data

Each capture contains a session-unique ID, UTC receipt time, supplied target,
byte count, detected type, and separate capture/preview states. IDs are not
reused after Clear and have a random session prefix across store instances.

Capture clones the payload, without trimming, decoding, or format inspection.
The HTTP caller supplies the bytes of decoded JSON `text`, not the HTTP body.
Virtual submission adds a conservative `^XA`/`^XZ` ZPL hint after storing the
job; it does not parse, validate, or transform the source. Rendering-off or raw
jobs never invoke the external renderer.

List returns newest-first metadata only. Get copies one payload/preview so a
caller cannot mutate internal storage. These snapshots belong to the caller;
clearing history cannot invalidate copies already returned or downloaded.

## Initial limits

Limits are implementation defaults in `capture.DefaultLimits()`, not settings.
One MiB is 1,048,576 bytes.

| Resource | Limit | Behavior |
|---|---|---|
| Virtual HTTP request body | 8 MiB | Limit reads before JSON decoding; account for trailing bytes and bodies without Content-Length; oversized reads return 413 |
| Decoded payload | 1 MiB | Reject oversized capture without evicting existing jobs |
| Supplied target metadata | 1,024 bytes | Reject oversized capture |
| Retained jobs | 100 | Evict oldest on new capture |
| Retained payload + target + preview bytes | 32 MiB | Evict oldest on new capture; reject a preview that cannot fit |
| Cached preview per job | 4 MiB | Reject oversized result while preserving original payload |
| Outstanding preview attempts | 8 | Reject new admission when full; capturing another job still works |
| Preview error description | 512 bytes | Retain a bounded copy separately from payloads |

The wire-body budget allows a 1 MiB payload encoded using six-byte JSON escapes.
These limits apply to the capture destination; physical print limits and payload
handling have not changed. The store budget covers owned byte buffers, not Go
object overhead or caller-owned snapshots; metadata count/length and work
reservations are separately bounded. Transient HTTP decoding also has its own
per-request ceiling. This is not a process-wide memory cap across all clients.

## Preview lifecycle hooks

Preview admission and result storage are supported by a bounded worker queue
and fixed-endpoint Labelary adapter:

1. `BeginPreviewAt` reserves a slot and records the zero-based label index. It rejects duplicate
   attempts and releases the job's previous cached preview for a new attempt.
2. `StartPreview` marks valid work `rendering`.
3. `FinishPreview` always acknowledges completion. A valid success stores a
   bounded copy and marks `ready`; failures mark `failed`, preserving capture.

Clear and eviction cancel associated contexts and invalidate results. A canceled
attempt continues occupying its reservation until FinishPreview acknowledges it;
otherwise repeated clears could admit unlimited slow workers. Workers must
finish canceled attempts too. A stale, repeated, or foreign-store ticket cannot
restore jobs, overwrite a newer preview, or release another attempt's slot.

Preview storage never evicts captured jobs to fit a result. Capture-driven
eviction does release both the evicted payload and its cached preview. Close
permanently rejects capture and cancels/releases all remaining session work.
Disabling rendering cancels admitted attempts but retains successful previews.
Changing enabled rendering settings cancels attempts and invalidates cached
images, without deleting captured payloads.

The renderer uses two workers and an eight-attempt total store reservation
(queued plus active). Requests time out after 15 seconds; response bodies are
limited to 4 MiB. PNG images are decoded only after checking dimensions against
2,000 pixels per side and four million pixels total. Preview bytes also remain
under the existing 4 MiB per-job and 32 MiB total store limits. Label indices
are limited to 0–99. The fixed Labelary HTTPS endpoint receives the raw payload
only in the POST body. Settings are snapshotted when a render is admitted.

## Verification

`internal/capture/store_test.go` covers defensive copies, metadata, byte/count
limits, oldest-first eviction, lifecycle, preview reservations/results, and
concurrent capture/read/clear/preview operations. `internal/server/capture_test.go`
covers decoded JSON bytes, pre-decode streaming limits, escaped payload size,
closed-store rejection, and retention across real listener restarts.

See the [test plan](../07-build-deploy/testing.md) and
[sprint verification record](../08-backlog/virtual-mode-sprint.md) for executed checks.
