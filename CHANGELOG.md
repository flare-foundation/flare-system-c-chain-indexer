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
  operation**: the indexer fully indexes only the last ~2 hours of blocks and
  backfills only the required metadata events for the three most recent reward
  epochs. Higher values are mainly useful for reward calculation.
- Resolution of contract addresses by name via the on-chain ContractRegistry,
  removing the need to hardcode addresses in config.
- `GET /health` endpoint on port 8080: returns 503 while startup catchup is in
  progress and 200 once the indexer reaches continuous-indexing mode. Suitable
  as a Docker / Kubernetes readiness probe.
- New `first_database_fsp_event_block` state row exposed alongside
  `first_database_block`, so clients can distinguish "earliest fully-indexed
  block" from "earliest block with FSP event coverage" and reason about
  available history.

### Changed

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
- Minimum Go toolchain version raised to 1.24.


## \[[v1.1.2](https://github.com/flare-foundation/flare-system-c-chain-indexer/tree/v1.1.2)\] - 2025-11-03

### Added

- simplify calculation of starting index within indexer
- add extra env var overrides for DB configuration
