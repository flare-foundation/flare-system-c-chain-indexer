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

The configuration is read from a `toml` file. Config file can be specified using the command line parameter `--config`, e.g., `./flare-ftso-indexer --config config.toml`.
The default config file name is `config.toml`.
Use [`config.example.toml`](config.example.toml) as the single config template. It includes full mode/indexer/db/chain/logger/timeout examples and comments.

#### Mode selection

- `indexer.mode = "fsp"`: use this when running as part of the FSP provider stack. Required FSP transactions/logs are hardcoded and auto-applied, so you do not need to specify `[[indexer.collect_transactions]]` or `[[indexer.collect_logs]]` in config (you can still add extra entries; they are merged and deduplicated). FSP startup also does selective indexing for required reward-epoch metadata windows instead of blindly indexing full historical ranges, which makes startup significantly faster. In this mode, `indexer.start_index` and `db.history_drop` are ignored; retention is derived from `indexer.history_epochs`.
- `indexer.mode = "full"`: use this for a generic C-chain indexer. In this mode you should define what to index via `[[indexer.collect_transactions]]` and `[[indexer.collect_logs]]`.

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
go build -o flare-ftso-indexer ./cmd/indexer
./flare-ftso-indexer --config config.toml
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

File `benchmarks/songbird_test.go` contains a benchmark test for indexing the FTSO protocol on the Songbird network. It processes 10,000 blocks and analyzes them. The test configuration is specified in `benchmarks/config_banchmark.toml`. To run the benchmark (replacing 10x with any desired number of repetitions), use:

```bash
go test -benchmem -run=^$ -benchtime 10x -bench ^BenchmarkBlockRequests$ ./benchmarks
```
