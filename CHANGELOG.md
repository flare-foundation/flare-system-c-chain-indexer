# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## Unreleased

### Added

- New `indexer.mode = "fsp"` mode for the FSP provider stack, introduced to
  massively speed up indexing from scratch by collecting only the data required
  for FSP operation. The required FSP transaction and log filters (for example
  Submission, Relay, FlareSystemsManager, and VoterRegistry) are built in and
  merged with any user-supplied `collect_transactions` / `collect_logs`, so a
  minimal config is enough to run an FSP indexer. `indexer.mode = "full"`
  keeps the previous generic-indexer behaviour.
- `indexer.history_epochs` now controls retention in FSP mode. The indexer
  keeps `history_epochs` reward epochs of fully indexed blocks, plus the
  signing-policy event metadata needed for those epochs. 
  In this mode `indexer.start_index` and `db.history_drop` are ignored.
  **`history_epochs = 0` is the recommended setting for FSP provider
  operation**: the indexer fully indexes only the most recent blocks (~1 hour,
  sized to cover the recent voting rounds and the submission data that reward
  calculation reads before an epoch's first voting round) and backfills the FSP
  protocol events needed for the recent reward epochs. Higher values are mainly
  useful for reward calculation.
- Resolution of contract addresses by name via the on-chain ContractRegistry,
  removing the need to hardcode addresses in config.
- `GET /health` endpoint on port 8080: returns 503 while startup catchup is in
  progress and 200 once the indexer reaches continuous-indexing mode. Suitable
  as a Docker / Kubernetes readiness probe.
- FSP mode now collects the FCC fee events reward calculation needs —
  `FlareTeeManager.TeeInstructionsSent` and `Fdc2Hub.AttestationRequested` — on
  Songbird, Coston and Coston2. Both contracts are addressed explicitly for now,
  as neither is in the ContractRegistry yet, so Flare gets no filters until it
  has a deployment. The built-in FSP collectors are consequently merged once the
  chain ID is known rather than while parsing the config.
- New `first_database_log_block` state row exposed alongside
  `first_database_block`, so clients can distinguish "earliest fully-indexed
  block" from "earliest block with FSP event coverage" and reason about
  available history.

### Changed

Every entry below that breaks an existing deployment is marked in bold. See
[Upgrading](#upgrading) for the before and after config.

- Repository structure refactored under `cmd/` and `internal/` to follow
  conventional Go layout. The runnable binary moved to `./cmd/indexer`.
- **Binary renamed** to `flare-cchain-indexer` (previously
  `flare_cchain_indexer` in the Dockerfile / `flare-ftso-indexer` in legacy
  build snippets). Deployment scripts, container `command:` entries, and any
  process supervisors that reference the binary by name need to be updated.
- **Go module path renamed** from `flare-ftso-indexer` to
  `github.com/flare-foundation/flare-system-c-chain-indexer`. The indexer is
  shipped as a binary, but any out-of-tree imports must be updated.
- Block-by-timestamp lookup uses heuristics to narrow the search window before
  binary search, avoiding requests for very old blocks when running against
  RPC nodes with limited history.
- Minimum Go toolchain version raised to 1.25.
- `states.name` now carries a unique index, so state writes are a single upsert.
  `AutoMigrate` creates it on an existing database and fails if that table holds
  duplicate names, which would stop the indexer from starting; see
  [Upgrading](#upgrading) for the query to check beforehand. No other schema
  change: the only removed model field is `transactions.signature`, whose column
  is left in place.
- **`indexer.num_parallel_req` renamed to `indexer.rpc_concurrency`**. Configs
  using the old key now fail at startup with a message pointing to the new name.
- **`indexer.rpc_concurrency` defaults to 25, down from 100.** It is now a single
  process-wide ceiling rather than a per-fan-out limit, so the old number means
  considerably more simultaneous load on the node than it used to. 25 is sized
  for a shared endpoint — a fresh FSP sync on Flare mainnet took 36s at that
  value — and can be raised for a dedicated node.
- **`timeout.timeout_millis` renamed to `timeout.rpc_timeout_millis`, and its
  default raised from 1s to 5s.** It bounds every individual RPC attempt (block,
  receipt, `eth_getLogs`, contract call). Configs using the old key now fail at
  startup with a message pointing to the new name. The old 1s default was too
  tight for `eth_getLogs` over a full `log_range` on a busy or throttled
  endpoint: a log fetch that timed out on every attempt exhausted its retry
  backoff, and that error tore the indexer back down to startup and re-attempted
  the same batch from the last committed block — looping with no forward
  progress. 5s clears measured healthy call latency (well under 1s) with margin
  while still failing a dead endpoint fast enough to retry within the backoff
  window; raise it for endpoints that are merely slow.
- Log fetching in the catchup path is now sequential, fetching `log_range`
  blocks per `eth_getLogs` request (matching the FSP metadata backfill).
  `log_range` is now a standalone "max blocks per `eth_getLogs`" knob with no
  dependency on `batch_size` or `rpc_concurrency`; set it to your RPC node's
  getLogs cap. The previous fan-out tied to `num_parallel_req` could silently
  fetch zero logs or drop ranges when misconfigured.
- `batch_size` is no longer forced to a multiple of the concurrency setting, and
  receipt fetching is now load-balanced per transaction rather than statically
  sliced. `rpc_concurrency`, `batch_size`, and `log_range` are now independent.
- Within each catchup batch, block fetching and log fetching now run
  concurrently instead of sequentially. Log queries depend only on the block
  range, so they complete in the shadow of the heavier block fetch, removing
  their latency from the critical path.
- The `rpc_concurrency` limit is now enforced inside the RPC client itself, so
  it is a single process-wide ceiling covering every caller — catchup,
  continuous indexing, FSP metadata backfill, start-block search, contract
  calls, and the concurrent history-drop scan — rather than a per-fan-out cap.

### Fixed

- FSP mode no longer probes blocks it does not need when resolving where to
  start. The start-block search is bounded by the event anchor — the oldest
  block the configured `history_epochs` requires, known from contract state —
  instead of stepping back from the tip in five-day windows. Nodes that were
  state synced do not have those older blocks, and the resulting failure used
  to be retried indefinitely with nothing logged above debug level, so the
  indexer appeared to hang at startup. If the node cannot serve the anchor
  block, startup now exits immediately naming the block, the reward epoch and
  the `history_epochs` value that requires it. Note that FSP mode inherently
  needs history back to two reward epochs before the oldest epoch it serves
  (about 7 days on Flare and Songbird, 14 hours on Coston and Coston2), so a
  node synced more recently than that cannot serve it whatever the setting.

### Upgrading

Configs using the old key names fail at startup, so the two renames above are the
only mandatory edit. Before upgrading, check that the `states` table holds no
duplicate names, which would stop `AutoMigrate` from creating the new unique
index:

```sql
SELECT name, COUNT(*) FROM states GROUP BY name HAVING COUNT(*) > 1;
```

An empty result means there is nothing to do; otherwise keep the row with the
highest `index` per name and delete the rest.

A 1.x full-mode config for the FSP provider stack, with legacy key names and
every collector spelled out:

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

The 2.0 equivalent in FSP mode. The collectors are built in and resolved by name
against the ContractRegistry, so the address blocks — including the legacy
VoterRegistry and FlareSystemsCalculator deployments — are no longer needed:

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

Staying on full mode is also supported: keep the collector blocks and
`db.history_drop`, and apply only the renames. Note that the built-in FSP
collectors are a narrower filter than `topic = "undefined"` on each contract —
they pin the specific topics the FSP stack consumes. Extra
`[[indexer.collect_transactions]]` and `[[indexer.collect_logs]]` entries are
still merged with the built-ins if you need more.


## \[[v1.1.2](https://github.com/flare-foundation/flare-system-c-chain-indexer/tree/v1.1.2)\] - 2025-11-03

### Added

- simplify calculation of starting index within indexer
- add extra env var overrides for DB configuration
