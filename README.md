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

The indexer is implemented in Go (tested with version 1.25). A running MySQL database to save the data is needed (we provide a
docker-compose.yaml file for automatic deployment of a database).

### Configuration

The configuration is read from a `toml` file. Config file can be specified using the command line parameter `--config`, e.g., `./flare-cchain-indexer --config config.toml`.
The default config file name is `config.toml`.
Use [`config.example.toml`](config.example.toml) as the single config template. It includes full mode/indexer/db/chain/logger/timeout examples and comments.

#### Mode selection

- `indexer.mode = "fsp"`: use this when running as part of the FSP provider stack. Required FSP transactions/logs are hardcoded and auto-applied, so you do not need to specify `[[indexer.collect_transactions]]` or `[[indexer.collect_logs]]` in config (you can still add extra entries; they are merged and deduplicated). FSP startup indexes only the recent data needed for FSP operation instead of the full block history, which makes startup significantly faster. In this mode, `indexer.start_index` and `db.history_drop` are ignored; retention follows the on-chain start data of the oldest reward epoch implied by `indexer.history_epochs`, so it stays correct even when reward epoch starts are delayed. With `indexer.history_epochs = 0` (the recommended provider setting) only the last ~1 hour of blocks are fully indexed — enough for the recent voting rounds, plus the submission data reward calculation reads before an epoch's first voting round — while FSP metadata events (signing policies, voter registrations, reward offers) are backfilled independently from two reward epochs back. If `indexer.history_epochs` reaches epochs the current FlareSystemsManager deployment has no start data for (e.g. right after a redeploy), startup catches up from the oldest epoch that has data and logs an error, while history drop keeps honoring the configured window (deleting nothing until it again lies within recorded epochs) — so the full window fills back in on its own as epochs pass.
- `indexer.mode = "full"`: use this for a generic C-chain indexer. In this mode you should define what to index via `[[indexer.collect_transactions]]` and `[[indexer.collect_logs]]`.

#### Node history (FSP mode)

FSP mode backfills metadata events from two reward epochs before the oldest epoch it serves, so the node needs block history reaching back that far — roughly 7 days on Flare and Songbird, 14 hours on Coston and Coston2, regardless of `indexer.history_epochs`. A node that was state synced more recently keeps only the blocks after its sync point; startup detects that and exits naming the block it needs, instead of failing later during the backfill.

#### Contract addressing

Contracts in `[[indexer.collect_transactions]]` and `[[indexer.collect_logs]]` can be specified either by `contract_address = "0x..."` or by `contract_name = "FlareSystemsManager"`. When a name is provided, the indexer resolves it to an address at startup via the on-chain ContractRegistry, so addresses that differ across networks (or change between deployments) do not need to be hardcoded in config. Most FSP built-in collectors use name-based resolution. The exception is the FCC fee events (`FlareTeeManager.TeeInstructionsSent`, `Fdc2Hub.AttestationRequested`), which reward calculation reads: those two contracts are not in the ContractRegistry yet, so they are addressed explicitly per network — collected on Songbird, Coston and Coston2, and nowhere else until Flare has a deployment. They will move to name-based resolution once registered. Being round logs, they are indexed from the oldest epoch `indexer.history_epochs` serves onward, not backfilled deeper.

#### Performance and RPC tuning

Three parameters control how the indexer talks to the RPC node. Most deployments only need to set `log_range`; the others have sensible defaults.

- **`log_range`** — max blocks per `eth_getLogs` request. **Set this to your RPC node's getLogs limit.** Many providers cap the block range (commonly 1000–10000) or the number of returned results; if `log_range` exceeds that cap, log requests fail. Use a conservative value on shared/public endpoints and a larger one on your own node to reduce the number of log requests. This is the only knob you usually need to know your node for.
- **`rpc_concurrency`** — max simultaneous RPC calls of every kind, enforced process-wide: block, receipt and log (`eth_getLogs`) fetches share this single budget, as do contract calls and history-drop lookups. This is the main throughput dial, since block fetching dominates catchup. Raise it to speed up catchup against a dedicated or underutilized node; lower it if a shared or rate-limited endpoint returns 429s or times out — note that lowering it also throttles log fetching. The default of 25 is sized for a shared endpoint; leave it otherwise.
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

### Upgrading from 1.x to 2.0

#### Breaking changes

- **`indexer.num_parallel_req` is now `indexer.rpc_concurrency`.** The old key fails at startup with a message pointing to the new one, rather than being silently ignored. It is also no longer a per-fan-out limit: it caps every simultaneous RPC call — blocks, receipts, `eth_getLogs`, contract calls, history-drop lookups — process-wide. Carrying an old value of 100 straight over therefore puts more load on the node than it used to; 20–50 is plenty (a fresh FSP sync on Flare mainnet took 36s at 25).
- **`timeout.timeout_millis` is now `timeout.rpc_timeout_millis`**, and its default rose from 1s to 5s. Also fails at startup if the old key is present.
- **`log_range` means max blocks per `eth_getLogs` request**, and log fetching is sequential per filter. It is no longer tied to `num_parallel_req`, `batch_size` or a parallel fan-out. A small value that worked in 1.x — the old suggestion was `batch_size / num_parallel_req` — now issues that many requests one after another, per configured filter: with `batch_size = 1000`, `log_range = 10` and 15 filters, a single batch makes ~1500 sequential requests. Set it to your RPC node's getLogs cap, commonly 1000–10000.
- **The binary is `flare-cchain-indexer`** (was `flare_cchain_indexer`). Only matters if you override the container command or run the binary directly; the image's own `CMD` is updated.
- **`states.name` gains a unique index.** If a 1.x database somehow holds two rows with the same state name, `AutoMigrate` fails and the indexer will not start. Check before upgrading:
  ```sql
  SELECT name, COUNT(*) FROM states GROUP BY name HAVING COUNT(*) > 1;
  ```
  An empty result means there is nothing to do. Otherwise keep the row with the highest `index` per name and delete the rest. Nothing else in the schema changes: the only removed model field is `transactions.signature`, and the column is left in place rather than dropped.
- **In FSP mode `indexer.start_index` and `db.history_drop` are ignored** (the latter logs a warning). Retention follows `indexer.history_epochs`.

#### Old and new configuration

A 1.x full-mode config for the FSP provider stack — legacy key names, every collector spelled out:

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

The 2.0 equivalent in FSP mode. The collectors are built in and resolved by name against the ContractRegistry, so the address blocks — including the legacy VoterRegistry and FlareSystemsCalculator deployments — are no longer needed:

```toml
[indexer]
mode = "fsp"
history_epochs = 0
rpc_concurrency = 25
batch_size = 1000
log_range = 1000
new_block_check_millis = 1000

[db]
# history_drop is ignored in FSP mode; retention follows history_epochs
```

Staying on full mode is also supported: keep the collector blocks and `db.history_drop`, and apply only the renames above.

Note that the built-in FSP collectors are a narrower filter than `topic = "undefined"` on each contract — they pin the specific topics the FSP stack consumes. Extra `[[indexer.collect_transactions]]` and `[[indexer.collect_logs]]` entries are still merged with the built-ins if you need more.

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
