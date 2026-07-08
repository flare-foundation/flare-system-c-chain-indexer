<!-- LOGO -->

<div align="center">
  <a href="https://flare.network/" target="blank">
    <img src="https://content.flare.network/Flare-2.svg" width="300" alt="Flare Logo" />
  </a>
  <br />
  Flare C-Chain Indexer
  <br />
  <a href="#PROJECT_NAME">About</a>
  ·
  <a href="CONTRIBUTING.md">Contributing</a>
  ·
  <a href="SECURITY.md">Security</a>
  ·
  <a href="CHANGELOG.md">Changelog</a>
</div>

# Flare C-Chain Indexer

This code implements a fast and parallelized indexer of C-chain that fetches data needed for
various Flare protocols. It saves the data in a MySQL database.

### Prerequisites

The indexer is implemented in Go (tested with version 1.24). A running MySQL database to save the data is needed (we provide a
docker-compose.yaml file for automatic deployment of a database).

### Configuration

The configuration is read from a `toml` file. Config file can be specified using the command line parameter `--config`, e.g., `./flare-cchain-indexer --config config.toml`.
The default config file name is `config.toml`.
Use [`config.example.toml`](config.example.toml) as the single config template. It includes full mode/indexer/db/chain/logger/timeout examples and comments.

#### Mode selection

- `indexer.mode = "fsp"`: use this when running as part of the FSP provider stack. Required FSP transactions/logs are hardcoded and auto-applied, so you do not need to specify `[[indexer.collect_transactions]]` or `[[indexer.collect_logs]]` in config (you can still add extra entries; they are merged and deduplicated). FSP startup indexes only the recent data needed for FSP operation instead of the full block history, which makes startup significantly faster. In this mode, `indexer.start_index` and `db.history_drop` are ignored; retention follows the on-chain start data of the oldest reward epoch implied by `indexer.history_epochs`, so it stays correct even when reward epoch starts are delayed.
- `indexer.mode = "full"`: use this for a generic C-chain indexer. In this mode you should define what to index via `[[indexer.collect_transactions]]` and `[[indexer.collect_logs]]`.

#### Contract addressing

Contracts in `[[indexer.collect_transactions]]` and `[[indexer.collect_logs]]` can be specified either by `contract_address = "0x..."` or by `contract_name = "FlareSystemsManager"`. When a name is provided, the indexer resolves it to an address at startup via the on-chain ContractRegistry, so addresses that differ across networks (or change between deployments) do not need to be hardcoded in config. FSP mode's built-in collectors all use name-based resolution.

#### Performance and RPC tuning

Three parameters control how the indexer talks to the RPC node. Most deployments only need to set `log_range`; the others have sensible defaults.

- **`log_range`** — max blocks per `eth_getLogs` request. **Set this to your RPC node's getLogs limit.** Many providers cap the block range (commonly 1000–10000) or the number of returned results; if `log_range` exceeds that cap, log requests fail. Use a conservative value on shared/public endpoints and a larger one on your own node to reduce the number of log requests. This is the only knob you usually need to know your node for.
- **`rpc_concurrency`** — max simultaneous RPC calls of every kind, enforced process-wide: block, receipt and log (`eth_getLogs`) fetches share this single budget, as do contract calls and history-drop lookups. This is the main throughput dial, since block fetching dominates catchup. Raise it to speed up catchup against a dedicated or underutilized node; lower it if a shared or rate-limited endpoint returns 429s or times out — note that lowering it also throttles log fetching. Leave the default otherwise.
- **`batch_size`** — the unit of work: how many blocks are fetched, processed, and committed together. Each batch is written in a single database transaction, so `batch_size` is effectively the DB commit size (and the in-memory working set, since the batch's blocks, transactions, and logs are held at once). Within that transaction, rows are inserted in fixed chunks of 1000 — a separate, non-configurable value, not `batch_size`. It does **not** change RPC request sizes: blocks and receipts are always one call each (there is no JSON-RPC request batching), and the per-request log range is governed by `log_range`. It is a memory-vs-checkpoint trade — larger batches mean fewer, larger DB commits and more data held in memory at once, and a crash re-processes up to `batch_size` blocks. Most users should leave it at the default.

Within a batch, block fetching and log fetching run concurrently (they have no data dependency, though they share the `rpc_concurrency` budget), and the indexer issues one `eth_getLogs` per configured log filter, tiled into `log_range`-sized chunks when `batch_size` exceeds `log_range`.

#### Startup and history (full mode)

The behavior described in this section applies to **full mode** only. FSP mode derives its start block and retention from `indexer.history_epochs` and the corresponding epochs' on-chain start data, and ignores both `indexer.start_index` and `db.history_drop`.

If the C chain indexer has been previously run and there is existing data in the database,
subsequent runs will resume indexing from after the last indexed block. Only when starting with an
empty database will the indexer have to decide where to start. Normally this will be based on the
history drop configuration parameter - if the history drop is 14 days for example then the indexer
will start indexing from the block that was mined 14 days ago. If the history drop is disabled (set
to 0) then the indexer will start from the configured `start_index` block or from block 0 if not
set.

In case the indexer has been previously run and you need more historical data, you can increase
the history drop parameter or disable history drop and set the start_index to the desired
starting block. You can manually delete all existing data from the database in order to trigger
re-indexing from the new starting block. You can also set the `drop_table_at_start` parameter to
true to have the indexer drop existing tables at startup and force re-indexing - though remember to
set it back to false afterwards to avoid losing data on subsequent runs.

### Database

In `internal/database/docker` we provide a simple database. Navigate to the folder and run

```bash
docker-compose up
```

### Running indexer

Simply run

```bash
go run ./cmd/indexer --config config.toml
```

or build and run the binaries with

```bash
go build -o flare-cchain-indexer ./cmd/indexer
./flare-cchain-indexer --config config.toml
```

### Health endpoint

The indexer exposes `GET /health` on port `8080`.

- Returns `503` while startup catchup/backfill is still running.
- Returns `200` after startup is complete and the indexer has entered continuous indexing mode.

Example:

```bash
curl -i http://localhost:8080/health
```

### Tests

There is an integration test which checks the historical indexing against known transactions and
logs on Coston2. To run this test you will need a MySQL server and a Coston2 node, preferably one that is not rate-limited.
The integration test is configured via `test/config_test.toml`. You can execute it with:

```bash
$ go test ./cmd/indexer
```

Additionally, a mocked-chain integration test is available in `test/indexer_test.go`. It uses `test/config_test.toml` for configuration. You can run it using:

```bash
go test ./test
```

To run tests with coverage analysis across all packages, save the results to `coverage.out`, and convert the report into an interactive HTML file run:

```bash
go test -v -coverpkg=./... -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Benchmarks

File `benchmarks/songbird_test.go` contains a benchmark test for indexing the FTSO protocol on the Songbird network. It processes 10,000 blocks and analyzes them. The test configuration is specified in `benchmarks/config_benchmark.toml`. To run the benchmark (replacing 10x with any desired number of repetitions), use:

```bash
go test -benchmem -run=^$ -benchtime 10x -bench ^BenchmarkBlockRequests$ ./benchmarks
```
