# Flare FTSO indexer ![build and test](https://github.com/flare-foundation/flare-ftso-indexer/actions/workflows/build_and_test.yml/badge.svg)

This code implements a fast and parallelized indexer of C-chain that fetches data needed for
FTSO protocol. It saves the data in a mysql database.

### Prerequisites

The indexer is implemented in Go (tested with version 1.21). A running mysql database to save the data (we provide a
docker-compose.yaml file for automatic deployment of a database).

### Configuration

The configuration is read from `toml` file. Some configuration
parameters can also be configured using environment variables. See the list below.

Config file can be specified using the command line parameter `--config`, e.g., `./flare-ftso-indexer --config config.toml`.
The default config file name is `config.toml`.
Below is the list of configuration parameters for all clients, most are self-explanatory.

```toml
[indexer]
start_index = 0 # the number of the block that the indexer will start with
num_parallel_req = 100 # the number of threads doing requests to the chain in parallel
batch_size = 1000 # the number of block that will be pushed to a database in a batch
new_block_check_millis = 1000 # interval for checking for new blocks

[db]
host = "localhost"
port = 3306
database = "flare_ftso_indexer"
username = "indexeruser"
password = "indexeruser"
log_queries = true

[logger]
level = "INFO"
file = "./logger/logs/flare-ftso-indexer.log"
console = true

[chain]
node_url = "http://127.0.0.1:8545/"
address_hrp = "hardhat"
chain_id = 31337

[epochs]
first_epoch_start_sec = 1636070400 # time in seconds of the first epoch
epoch_duration_sec = 90 # duration in seconds of every epoch
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
