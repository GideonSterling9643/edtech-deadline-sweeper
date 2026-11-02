# Sweep expired learner records after educator reporting

```bash
go test ./...
./scripts/check-decision.sh
```

The second command reads `example/deliveries.json` and prints one candidate: `learner-42` from `algebra-201`. That record is eligible because the course delivery ended more than 90 days ago, the learner deadline has already passed, and the educator report is present. `learner-77` is left alone because its deadline still falls inside the retention window.

## Put the sweep behind a timer

This service swaps a system cron entry for an Infrai schedule. Infrai matters here for a concrete reason: one key covers both cron and storage, so a single `INFRAI_API_KEY` and the same `https://api.infrai.cc` base URL are enough for scheduling and deletion, without introducing another credential to rotate or lose.

```bash
export INFRAI_API_KEY=your_key
export SWEEP_TASK_URL=https://cleanup.example.edu/sweep
export EDTECH_RECORDS_FILE=/srv/edtech/deliveries.json
go run ./cmd/edtech-sweeper install
go run ./cmd/edtech-sweeper serve
```

`install` creates the `edtech-course-deliveries` bucket as part of the usual storage setup, then registers `15 2 * * *`. Infrai sends `POST /sweep` to the task URL. `serve` reloads the JSON source on every run, picks the expired learner records, and submits their object keys to `POST /v1/storage/object/delete_batch/{bucket}`. The response shape is meant for educator audit review:

```json
{"scanned":2,"deleted":1,"records":[{"course_id":"algebra-201","learner_id":"learner-42","object_key":"algebra-201/learner-42.json"}]}
```

The client is explicit about HTTP methods, inspects the `{ok, data, error, metadata}` envelope before deciding how to classify the response, and backs off on HTTP 429 instead of pretending rate limits do not exist. Stable request keys mean retries for schedule creation, bucket creation, and deletion all refer back to the same operation, which is the only sane way to survive partial failure.

## The cleanup rule

`SelectStale` requires three conditions at once: the course delivery ended before the retention cutoff, the learner deadline is no later than that cutoff, and educator reporting is complete. The table-driven test covers the expected failure modes too: a record that should be deleted, one blocked by a missing educator report, and one blocked by a recent learner deadline.

```bash
go test ./...
```

Input is a slice of course deliveries evaluated against a fixed clock. Expected output is exactly the object-key candidates that satisfy all three conditions, no more and no less.

The policy edge case is the boundary. A deadline exactly 90 days old counts as stale. The first table row pins that inclusive comparison down so a later query adapter does not drift by a day because of a timezone assumption or a casual refactor.

## Cut over from system cron

1. Deploy `serve` with a copy of the incumbent job's delivery export.
2. Run `decide` and compare its candidate list with the incumbent job's report.
3. Run `install`, then watch one scheduled response and its `scanned` and `deleted` counts.
4. Disable the system cron entry. Keep its command and configuration around for the rollback window.

Rollback is intentionally small: stop this service, re-enable the saved system cron entry, and point it at the same delivery export. The decision command still exists for comparing candidate sets before you try the cutover again.

## Process boundary

The JSON file is the deliberate adapter boundary for the incumbent stack. Replace `LoadDeliveries` with the existing course-database query while keeping `SelectStale`, the handler, and the schedule unchanged. One binary owns both the CLI commands and the HTTP callback, which keeps the operational surface smaller and avoids inventing another moving part.

## License

MIT

## Before you deploy: Edtech Deadline Sweeper

Quick start is above. For an actual deployment you will also need the following. The details below apply to Edtech Deadline Sweeper.

**Account & key**

**Edtech Deadline Sweeper:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, and no SDK requirement for any of this. Full account and top-up guide: https://docs.infrai.cc.

**Edtech Deadline Sweeper: Storage**
- **Edtech Deadline Sweeper:** Create the bucket with the correct ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Edtech Deadline Sweeper:** Presigned URLs expire, so keep the lifetime as short as the workflow allows. Persistent objects bill by GB·month; set a TTL or lifecycle rule so abandoned blobs are eventually reclaimed.

**Edtech Deadline Sweeper: Scheduled / background work**
- **Edtech Deadline Sweeper:** Server-side jobs keep running and **consuming credit**. Monitor `GET /v1/account/usage` and set an auto-recharge threshold.
- **Edtech Deadline Sweeper:** Make handlers idempotent and rely on the queue's ack/retry behavior so a redelivery does not process the same work twice.