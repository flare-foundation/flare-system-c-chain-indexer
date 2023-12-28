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
collect_transactions = [ # specify which types of transactions should be indexed
    [
        "22474d350ec2da53d717e30b96e9a2b7628ede5b", # address of the contract (can be "undefined")
        "f14fcbc8", # signature of the function on the contract  (can be "undefined")
        true, # boolean indicating if it should be checked if the transaction succeeded
        true, # boolean indicating if the logs of the emitted events should be saved to the database
    ],
    [
        "22474d350ec2da53d717e30b96e9a2b7628ede5b",
        "4369af80",
        true,
        true,
    ]
]
collect_logs = [ # specify which types of logs should be indexed (besides those obtained from the transactions specified above)
    [
        "b682deef4f8e298d86bfc3e21f50c675151fb974", # address of the contract calling the log (can be "undefined")
        "undefined", # topic0 of the log  (can be "undefined")
    ],
]

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
node_url = "http://127.0.0.1:8545/"
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

See `indexer/indexer_test.go` for a test run of the indexer on a mocked chain provided in the `testing` folder.

```
cd indexer
go test -v
```

### Benchmarks

File `benchmarks/songbird_test.go` implements a benchmark test that indexes the (not-yet-scaled) FTSO
protocol on the songbird network. It requests for 10000 blocks and analyses them (see `config.songbird.toml`
for the other parameters).
Run the following (where 10x can be replaced by any other number of repeats)

```
go test -benchmem -run=^$ -benchtime 10x -bench ^BenchmarkBlockRequests$ flare-ftso-indexer/benchmarks
```
