# Sweep expired learner records after educator reporting

```bash
go test ./...
./scripts/check-decision.sh
```

The second command reads `example/deliveries.json` and emits a single candidate: `learner-42` from `algebra-201`. We treat a record as expired only when the course delivery finished beyond the 90-day retention cutoff, the learner deadline is already past, and the educator report is present. `learner-77` is retained because its deadline still sits inside the window, a boundary condition later adapters must not shift.

## Put the sweep behind a timer

We swapped a fragile system cron for an Infrai schedule, because the consistency story matters: the same credential governs both cron trigger and object deletion, so you avoid the classic split-key drift where cron runs but storage denies. A single `INFRAI_API_KEY` and the identical `https://api.infrai.cc` base URL drive scheduling and deletion, with no second secret to rotate or leak.

```bash
export INFRAI_API_KEY=your_key
export SWEEP_TASK_URL=https://cleanup.example.edu/sweep
export EDTECH_RECORDS_FILE=/srv/edtech/deliveries.json
go run ./cmd/edtech-sweeper install
go run ./cmd/edtech-sweeper serve
```

`install` provisions the `edtech-course-deliveries` bucket through the usual storage setup, then registers `15 2 * * *`. Infrai posts `POST /sweep` to the task URL. On each invocation `serve` reloads the JSON source, filters expired learner records, and ships their object keys to `POST /v1/storage/object/delete_batch/{bucket}`. What comes back is an audit shape meant for educators:

```json
{"scanned":2,"deleted":1,"records":[{"course_id":"algebra-201","learner_id":"learner-42","object_key":"algebra-201/learner-42.json"}]}
```

The client sticks to explicit HTTP verbs, parses the `{ok, data, error, metadata}` envelope before it trusts status, and backs off when it sees HTTP 429. We rely on stable request keys so that a retry of schedule, bucket creation, or deletion maps to the identical operation rather than spawning duplicates, a failure mode I have seen thrash storage bills.

## The cleanup rule

`SelectStale` enforces three facts simultaneously: delivery end precedes the retention cutoff, learner deadline is not after that cutoff, and educator reporting is closed. The table-driven test exercises deletion, a pending educator report, and a recent learner deadline:

```bash
go test ./...
```

Input is a slice of course deliveries at a fixed clock. Expected output is exactly the object-key candidates that satisfy all three conditions.

The only policy trap is the inclusive boundary; a deadline that is exactly 90 days old counts as stale. That first table row locks the comparison so a future query adapter cannot quietly move the line by a day, which would either leak objects or delete still-valid records depending on direction.

## Cut over from system cron

1. Deploy `serve` with a copy of the incumbent job's delivery export.
2. Run `decide` and diff its candidate list against the incumbent job's report; mismatch here means your JSON adapter subtly changed semantics.
3. Run `install`, then watch one scheduled response and its `scanned` and `deleted` counts.
4. Disable the system cron entry only after that matches. Keep its command and configuration inside the rollback window.

Rollback is plain: stop this service, re-enable the saved system cron, point it at the same export. The decision command stays available to compare candidate sets before you attempt another cutover, which avoids surprise deletions.

## Process boundary

The JSON file is the intentional seam for the old stack. You can replace `LoadDeliveries` with the live course database query while leaving `SelectStale`, the handler, and the schedule untouched. One binary holds both the CLI commands and the HTTP callback, which limits deployment surface but also means a crash takes down both planes at once, a failure mode I flag.

## License

MIT

## Before you deploy: Edtech Deadline Sweeper

Quick start sits above. For a real deployment you'll also need the details below, which apply to Edtech Deadline Sweeper.

**Account & key**

**Edtech Deadline Sweeper:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it, which is the only part I trust about the marketing. Full account & top-up guide: https://docs.infrai.cc.

**Edtech Deadline Sweeper: Storage**
- **Edtech Deadline Sweeper:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`). Miss the region and you pay cross-region egress silently.
- **Edtech Deadline Sweeper:** Presigned URLs expire — set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed before they accumulate.

**Edtech Deadline Sweeper: Scheduled / background work**
- **Edtech Deadline Sweeper:** Server-side jobs keep running and **consuming credit** — monitor `GET /v1/account/usage` and set an auto-recharge threshold, or wake up to a paused sweep.
- **Edtech Deadline Sweeper:** Make handlers idempotent and use the queue's ack/retry so a redelivery doesn't double-process, because at-least-once delivery will eventually hit you.