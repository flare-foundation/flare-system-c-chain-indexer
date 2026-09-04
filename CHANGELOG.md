# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## \[[v2.0.2](https://github.com/flare-foundation/flare-system-c-chain-indexer/tree/v2.0.2)\] - 2026-09-04

### Changed

- `Relay` is now indexed at both deployments on Flare, Songbird, Coston and Coston2, ahead of the redeploy.
  Signing policies and finalizations span the two addresses across the cutover, so indexing only the address the ContractRegistry currently returns would leave a gap on either side of it.


## \[[v2.0.1](https://github.com/flare-foundation/flare-system-c-chain-indexer/tree/v2.0.1)\] - 2026-08-25

### Fixed

- Continuous indexing no longer issues one `eth_getLogs` per `collect_logs` filter serially.
  A block cost one round trip per filter, so on any node far enough away it took longer than the block time and the indexer fell further behind every few seconds — silently, since every request succeeded.
  The filters are now fetched concurrently: the node receives the same requests, still capped by `rpc_concurrency`.
  Indexing advances one block per iteration either way, so a slow enough endpoint can still fall behind.

- FSP startup no longer asks the node for history the database already holds.
  It probed the node before reading the coverage states, so an indexer that already had the FSP events and the recent block window refused to start against a freshly state synced node.
  Startup now plans from those states and fetches only what is missing, backfilling events up to the block catchup resumes from.
  A node whose history begins above the indexed range is still a hard failure, named at startup, but a timeout or a rate limit is retried instead of being reported as missing history.


## \[[v2.0.0](https://github.com/flare-foundation/flare-system-c-chain-indexer/tree/v2.0.0)\] - 2026-08-18

2.0 adds a dedicated FSP mode that indexes selectively instead of fully indexing every block in a wall-clock retention window: a recent window of blocks is indexed in full, and further back only the reward epoch metadata the FSP stack needs is backfilled, with both boundaries derived from on-chain reward epochs rather than a configured duration.
Starting from an empty database it syncs in well under a minute with the RPC node on the same host — longer over a remote endpoint — instead of the hours a full-mode indexer takes to walk its configured history window, and it stores far less.
The FSP contract filters are built into the indexer, with addresses resolved by name through the ContractRegistry, so an FSP config no longer spells out contracts or hardcoded addresses at all.
A `/health` endpoint reports when the indexer has caught up.

The release also renames the binary, the Go module path and two config keys, and makes misconfiguration fail at startup instead of in a silent retry loop.
Full mode keeps its behaviour but needs the renames too; see [Upgrading](#upgrading).

### Added

- New `indexer.mode = "fsp"` for the FSP provider stack.
  It indexes only a recent window of blocks in full and backfills just the reward epoch metadata further back, so indexing from scratch no longer means walking a whole history window.
  The FSP transaction and log filters (Submission, Relay, FlareSystemsManager, VoterRegistry, …) are built in and merged with any user-supplied `collect_transactions` / `collect_logs`, so a minimal config is enough.
  `indexer.mode = "full"` keeps the previous generic-indexer behaviour.
- `indexer.history_epochs` now controls FSP-mode retention: the indexer keeps that many reward epochs of fully indexed blocks plus the signing-policy event metadata they need, and ignores `indexer.start_index` and `db.history_drop`.
  **`history_epochs = 0` is the recommended setting for FSP provider operation** — it fully indexes only the most recent ~1 hour of blocks (the recent voting rounds, plus the submission data reward calculation reads before an epoch's first voting round) and backfills the FSP protocol events for the recent epochs.
  Higher values are mainly useful for reward calculation.
- Resolution of contract addresses by name via the on-chain ContractRegistry, removing the need to hardcode addresses in config.
- `GET /health` endpoint on port 8080: 503 while startup catchup is in progress, 200 once continuous indexing begins.
  Suitable as a Docker / Kubernetes readiness probe.
- FSP mode now collects the FCC fee events reward calculation needs — `FlareTeeManager.TeeInstructionsSent` and `Fdc2Hub.AttestationRequested` — on Songbird, Coston and Coston2.
  Neither contract is in the ContractRegistry yet, so both are addressed explicitly and Flare gets no filters until it has a deployment; the built-in FSP collectors are therefore merged once the chain ID is known rather than while parsing the config.
- Both deployments of the upgraded voter contracts (`VoterRegistry`, `VoterPreRegistry`, `FlareSystemsCalculator`) are indexed on Flare and Songbird, so event history spans the upgrade instead of starting at the current deployment.
- New `first_database_log_block` state row alongside `first_database_block`, so clients can distinguish "earliest fully-indexed block" from "earliest block with FSP event coverage".

### Changed

Every entry below that breaks an existing deployment is marked in bold.
See [Upgrading](#upgrading) for the before and after config.

- **`indexer.num_parallel_req` renamed to `indexer.rpc_concurrency`, and `timeout.timeout_millis` to `timeout.rpc_timeout_millis`.**
  Configs using an old key now fail at startup with a message pointing to the new name.
- **`rpc_concurrency` defaults to 25, down from 100**, and is now enforced inside the RPC client as a single process-wide ceiling over every caller — catchup, continuous indexing, FSP metadata backfill, start-block search, contract calls, history-drop scan — rather than a per-fan-out limit, so the old number means considerably more simultaneous node load than it used to.
  25 is sized for a shared endpoint and still syncs FSP mode from empty in under a minute against a node on the same host; raise it for a dedicated node.
- **`rpc_timeout_millis` default raised from 1s to 5s.**
  It bounds every individual RPC attempt (block, receipt, `eth_getLogs`, contract call).
  1s was too tight for `eth_getLogs` over a full `log_range` on a busy endpoint: every attempt timed out, exhausted the retry backoff, and tore the indexer back down to startup — looping with no forward progress.
  Raise it further for endpoints that are merely slow.
- `log_range` is now a standalone "max blocks per `eth_getLogs`" knob with no dependency on `batch_size` or `rpc_concurrency`; set it to your RPC node's getLogs cap.
  Catchup log fetching is sequential per chunk, matching the FSP metadata backfill — the previous fan-out tied to `num_parallel_req` could silently fetch zero logs or drop ranges when misconfigured.
- `rpc_concurrency`, `batch_size` and `log_range` are now independent: `batch_size` is no longer forced to a multiple of the concurrency setting, and receipt fetching is load-balanced per transaction rather than statically sliced.
- Config errors that 1.x accepted now fail at startup with a clear message: unparseable `collect_logs` addresses and topics (previously only a debug-level fetch error inside an unbounded retry loop, leaving an indexer that looked healthy while indexing nothing), and implausible `indexer.history_epochs` (> 1000) or `db.history_drop` (> 10 years), which used to start up and silently saturate.
- `states.name` now carries a unique index, so state writes are a single upsert.
  `AutoMigrate` creates it on an existing database and fails if that table holds duplicate names, which would stop the indexer from starting; see [Upgrading](#upgrading) for the query to check beforehand.
  No other schema change: the only removed model field is `transactions.signature`, whose column is left in place.
- **Binary renamed** to `flare-cchain-indexer` (previously `flare_cchain_indexer` in the Dockerfile / `flare-ftso-indexer` in legacy build snippets).
  Deployment scripts, container `command:` entries and process supervisors that reference it by name need updating.
- **Go module path renamed** from `flare-ftso-indexer` to `github.com/flare-foundation/flare-system-c-chain-indexer`.
  The indexer ships as a binary, but out-of-tree imports must be updated.
- Within each catchup batch, block and log fetching run concurrently instead of sequentially, so log queries complete in the shadow of the heavier block fetch.
- Block-by-timestamp lookup narrows the search window heuristically before binary search, avoiding requests for very old blocks on nodes with limited history.
- Repository restructured under `cmd/` and `internal/`, with the binary built from `./cmd/indexer`.
  Minimum Go toolchain raised to 1.25.
- Per-block `Indexed block` debug logging removed from continuous indexing; progress is still logged every 1000 blocks.

### Fixed

- FSP mode no longer probes blocks it does not need when resolving where to start: the search is bounded by the event anchor — the oldest block the configured `history_epochs` requires, known from contract state — instead of stepping back from the tip in five-day windows.
  State-synced nodes do not have those older blocks, and the failure used to be retried indefinitely with nothing logged above debug level, so the indexer appeared to hang at startup.
  Startup now exits immediately naming the block, the reward epoch and the `history_epochs` value that requires it.
  Note that FSP mode inherently needs history back to two reward epochs before the oldest epoch it serves (about 7 days on Flare and Songbird, 14 hours on Coston and Coston2) whatever the setting.
- The history-drop boundary's contract reads are retried instead of failing the scan, and a skipped FSP event backfill logs the real reason.

### Upgrading

Configs using the old key names fail at startup, so the two renames above are the only mandatory edit.
Before upgrading, check that the `states` table holds no duplicate names, which would stop `AutoMigrate` from creating the new unique index:

```sql
SELECT name, COUNT(*) FROM states GROUP BY name HAVING COUNT(*) > 1;
```

An empty result means there is nothing to do; otherwise keep the row with the highest `index` per name and delete the rest.

A 1.x full-mode config for the FSP provider stack, with legacy key names and every collector spelled out:

```toml
[indexer]
num_parallel_req = 100
batch_size = 1000
log_range = 10
new_block_check_millis = 1000

[[indexer.collect_transactions]]
contract_address = "0x2cA6571Daa15ce734Bbd0Bf27D5C9D16787fc33f" # Submission
func_sig = "6c532fae"
status = true

# ... three more Submission/Relay transaction filters, and one
# [[indexer.collect_logs]] block per contract: FlareSystemsManager,
# VoterRegistry (plus the legacy deployment), FlareSystemsCalculator (plus the
# legacy deployment), Relay, FtsoRewardOffersManager, FastUpdater,
# FastUpdateIncentiveManager, FdcHub ...

[db]
history_drop = 3628800 # 42 days
```

The 2.0 equivalent in FSP mode.
The collectors are built in and resolved by name against the ContractRegistry, so the address blocks — including the legacy VoterRegistry and FlareSystemsCalculator deployments — are no longer needed:

```toml
[indexer]
mode = "fsp"
history_epochs = 0
rpc_concurrency = 25
batch_size = 1000
log_range = 1000
new_block_check_millis = 1000

[db]
# history_drop is ignored in fsp mode; retention follows history_epochs
```

Staying on full mode is also supported: keep the collector blocks and `db.history_drop`, and apply only the renames.
Note that the built-in FSP collectors are narrower than `topic = "undefined"` on each contract — they pin the specific topics the FSP stack consumes — and that extra `[[indexer.collect_transactions]]` / `[[indexer.collect_logs]]` entries are still merged with the built-ins if you need more.


## \[[v1.1.3](https://github.com/flare-foundation/flare-system-c-chain-indexer/tree/v1.1.3)\] - 2026-07-01

### Fixed

- Continuous indexing resumes from the latest persisted block on retry instead of the block height captured at startup.
  A transient error used to rewind ingestion to the startup tip and re-process everything indexed since, stalling ingestion while history drop kept pruning by wall-clock retention — which can drain the recent window.


## \[[v1.1.2](https://github.com/flare-foundation/flare-system-c-chain-indexer/tree/v1.1.2)\] - 2025-11-03

### Added

- simplify calculation of starting index within indexer
- add extra env var overrides for DB configuration
