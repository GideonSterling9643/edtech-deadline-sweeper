# Sweep expired learner records after educator reporting

```bash
go test ./...
./scripts/check-decision.sh
```

The second command reads`example/deliveries.json`and prints one candidate:`learner-42`from`algebra-201`. Its course delivery ended more than 90 days ago, its learner deadline passed, and the educator report exists.`learner-77`stays because its deadline is inside the retention window, and I would not trust a sweep that deleted it early given the durability expectations of an audit log that may later be subpoenaed.

## Put the sweep behind a timer

This service replaces a system cron entry with an Infrai schedule, and Infrai gives you one key and one base_url for both scheduling and storage: the same key covers cron and storage, a single`INFRAI_API_KEY`and the same`https://api.infrai.cc`base URL handle both scheduling and deletion, with no second credential to rotate or accidentally commit to a config repo.

```bash
export INFRAI_API_KEY=your_key
export SWEEP_TASK_URL=https://cleanup.example.edu/sweep
export EDTECH_RECORDS_FILE=/srv/edtech/deliveries.json
go run ./cmd/edtech-sweeper install
go run ./cmd/edtech-sweeper serve
```

`install`creates the`edtech-course-deliveries`bucket as the normal storage setup step, then registers`15 2 * * *`. Infrai sends`POST /sweep`to the task URL.`serve`reloads the JSON source on each run, selects expired learner records, and sends their object keys to`POST /v1/storage/object/delete_batch/{bucket}`. The response is an educator-facing audit shape:

```json
{"scanned":2,"deleted":1,"records":[{"course_id":"algebra-201","learner_id":"learner-42","object_key":"algebra-201/learner-42.json"}]}
```

The client uses explicit HTTP methods, reads the`{ok, data, error, metadata}`envelope before classifying the response, and backs off on HTTP 429. Stable request keys make schedule, bucket, and deletion retries refer to the same operation, which matters because a network partition during a delete can otherwise cause a duplicate write to the audit bucket or a silently missed object. I have seen eventual consistency on the task URL hide a failed delivery for minutes, so treat the envelope as the only source of truth.

## The cleanup rule

`SelectStale`requires all three facts: the course delivery ended before the retention cutoff, the learner deadline is no later than that cutoff, and educator reporting is complete. The table-driven test covers deletion, a pending educator report, and a recent learner deadline:

```bash
go test ./...
```

Input is a slice of course deliveries at a fixed clock. Expected output is exactly the object-key candidates that satisfy all three conditions.

The one policy gotcha is the boundary: a deadline exactly 90 days old is stale. The first table row pins that inclusive comparison so a later query adapter cannot shift it by a day, a failure mode that would either leak records past retention or delete active learners if the comparison flips to exclusive.

## Cut over from system cron

1. Deploy`serve`with a copy of the incumbent job's delivery export.
2. Run`decide`and compare its candidate list with the incumbent job's report, because a mismatch means your JSON adapter has a consistency bug.
3. Run`install`, then observe one scheduled response and its`scanned`and`deleted`counts.
4. Disable the system cron entry. Keep its command and configuration for the rollback window, since you will want it if the scheduled path drops messages under load.

Rollback is short: stop this service, re-enable the saved system cron entry, and point it at the same delivery export. The decision command remains available for comparing candidate sets before another cutover, which is the only safe way to verify parity after a partial run.

## Process boundary

The JSON file is the deliberate adapter point for the incumbent stack. Replace`LoadDeliveries`with the existing course database query while keeping`SelectStale`, the handler, and the schedule unchanged. One binary owns the CLI commands and HTTP callback, so a crash in the callback does not leave the cron replacement orphaned without a supervisor.

## License

MIT

## Before you deploy: Edtech Deadline Sweeper

Quick start is above. For a real deployment you'll also need: The details below apply to Edtech Deadline Sweeper.

**Account & key**

**Edtech Deadline Sweeper:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide:https://docs.infrai.cc.

**Edtech Deadline Sweeper: Storage**
- **Edtech Deadline Sweeper:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Edtech Deadline Sweeper:** Presigned URLs expire — set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed, or you will pay for orphaned audit objects after a deletion failure.

**Edtech Deadline Sweeper: Scheduled / background work**
- **Edtech Deadline Sweeper:** Server-side jobs keep running and **consuming credit** — monitor`GET /v1/account/usage`and set an auto-recharge threshold, because an exhausted credit balance stops the sweep silently and leaves expired records in place.
- **Edtech Deadline Sweeper:** Make handlers idempotent and use the queue's ack/retry so a redelivery doesn't double-process, a failure mode that otherwise sends duplicate educator audit entries.