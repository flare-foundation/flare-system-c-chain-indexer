# Smoke test

A minimal end-to-end check that the indexer builds, starts, connects to a live
C-chain node, and indexes data — intended for local verification and CI.

It builds the image from source, runs it in FSP mode (`history_epochs = 0`, so
only the last ~2 hours of blocks) against a C-chain RPC node, and asserts that
the indexer reaches its synced state (`GET /health` → `200`) and has written
transactions and logs to a throwaway MySQL. The stack is torn down
automatically.

## Run

```bash
RPC_URL="https://<host>/ext/bc/C/rpc?x-apikey=<key>" ./test/smoke/smoke-test.sh
```

Exit code `0` means pass.

`RPC_URL` is the only setting — point it at the network you want to test (Coston
recommended). Use a **keyed** endpoint: public endpoints are rate-limited and
the test will likely time out during the FSP event backfill.

Requires Docker and outbound access to the RPC endpoint.
