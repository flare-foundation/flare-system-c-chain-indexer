# Flare FTSO indexer ![build and test](https://github.com/flare-foundation/flare-ftso-indexer/actions/workflows/build_and_test.yml/badge.svg)

This code implements a fast and parallelized indexer of C-chain that fetches data needed for
various Flare protocols. It saves the data in a MySQL database.

### Prerequisites

The indexer is implemented in Go (tested with version 1.21). A running MySQL database to save the data is needed (we provide a
docker-compose.yaml file for automatic deployment of a database).

### Configuration

The configuration is read from a `toml` file. Config file can be specified using the command line parameter `--config`, e.g., `./flare-ftso-indexer --config config.toml`.
The default config file name is `config.toml`.
Below is the list of configuration parameters, most are self-explanatory. Note that the chain node, to which the indexer connects
(parameter `node_url`), needs to allow many simultaneous request if the indexer is about to index big amount of data.

```toml
[indexer]
start_index = 0 # the number of the block that the indexer will start with
stop_index = 0 # the number of the block that the indexer will stop with; set 0 or skip to index indefinitely
num_parallel_req = 100 # the number of threads doing requests to the chain in parallel
batch_size = 1000 # the number of blocks that will be pushed to a database in a batch (should be divisible by num_parallel_req)
log_range = 10 # the size of the interval of blocks used to request logs in each request; suggested value is log_range = batch_size / num_parallel_req; note that a blockchain node might have an upper bound on this
new_block_check_millis = 1000 # interval for checking for new blocks

[[indexer.collect_transactions]]
contract_address = "22474d350ec2da53d717e30b96e9a2b7628ede5b" # address of the contract (can be "undefined")
func_sig = "f14fcbc8" # signature of the function on the contract  (can be "undefined")
status=true # boolean indicating if it should be checked if the transaction succeeded
collect_events=true # boolean indicating if the logs of the emitted events should be saved to the database

[[indexer.collect_transactions]]
contract_address = "22474d350ec2da53d717e30b96e9a2b7628ede5b"
func_sig = "4369af80"
status = true
collect_events = true

[[indexer.collect_logs]]
contract_address = "b682deef4f8e298d86bfc3e21f50c675151fb974" # address of the contract calling the log (can be "undefined")
topic = "undefined" # topic0 of the log  (can be "undefined")

[db]
host = "localhost"
port = 3306
database = "flare_ftso_indexer"
username = "root"
password = "root"
log_queries = false
drop_table_at_start = true
history_drop = 604800 # Enable deleting the transactions and logs in DB that are older (timestamp of the block) than history_drop (in seconds); set 0 or skip to turn off

[logger]
level = "INFO"
file = "./logger/logs/flare-ftso-indexer.log"
console = true

[chain]
node_url = "http://127.0.0.1:8545/"  # or NODE_URL environment variable
# api_key = ...  or NODE_API_KEY environment variable
# chain_type = 1 # default Avalanche based chain=1, Ethereum based chain=2

[timeout]
backoff_max_elapsed_time_seconds = 300 # optional, defaults to 300s = 5 minutes. Affects how long the indexer will keep retrying in case of a complete outage of the node provider. Set to 0 to retry indefinitely.
timeout_milis = 1000  # optional, defaults to 1000ms = 1s. Try increasing if you see timeout errors often.
```

### Database

In `database/docker` we provide a simple database. Navigate to the folder and run

```
docker-compose up
```

### Running FTSO indexer

Simply run

```
go run main.go --config config.toml
```

or build and run the binaries with

```
go build
./flare-ftso-indexer --config config.toml
```

### Tests

There is an integration test which checks the historical indexing against known transactions and
logs on Coston2. To run this test you will need a MySQL server and a Coston2 node - ideally one that
is not rate-limited. The test is configured via environment variables, see `.env.example`
for an example configuration. With the appropriate environment vars set the test can be run with:

```
$ go test ./main_test.go
```

A unit test using a mocked chain node is also provided at `indexer/indexer_test.go`. This test is
also configured via environment variables, with an example at `indexer/.env.example`, and may be run with:

```
go test ./indexer
```

### Benchmarks

File `benchmarks/songbird_test.go` implements a benchmark test that indexes the (not-yet-scaled) FTSO
protocol on the songbird network. It requests for 10000 blocks and analyses them (see `config.songbird.toml`
for the other parameters).
Run the following (where 10x can be replaced by any other number of repeats)

```
go test -benchmem -run=^$ -benchtime 10x -bench ^BenchmarkBlockRequests$ flare-ftso-indexer/benchmarks
```
