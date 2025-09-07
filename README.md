# SolRoute SDK

SolRoute is a Go SDK that serves as the fundamental infrastructure for building DEX routing services on Solana. Unlike solutions that rely on third-party APIs, SolRoute directly interacts with the Solana blockchain.

## Features

- **Protocol Support**

  - Raydium CPMM V4 (`675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8`)
  - Raydium CPMM (`CPMMoo8L3F4NbTegBCKVNunggL7H1ZpdTHKxQB5qKP1C`)
  - Raydium CLMM (`CAMMCzo5YL8w4VFF8KVHrK22GGUsp5VTaW7grrKgrWqK`)
  - PumpSwap AMM (`pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA`)
  - Meteora DLMM (`LBUZKhRxPF3XUpBCjp4YzTKgLccjZhTSDM9YuVaPwxo`)
  - Orca Whirlpool (`whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc`)

- **Core Functionality**
  - Pool discovery and management
  - Quote generation
  - Cross-DEX routing and optimal path finding
  - Transaction instruction building

## Quick Start

Use the test cases in the `tests` directory to quickly experience pool discovery and swap execution.

### 1. Installation

```bash
go get github.com/Solana-ZH/solroute
```

### 2. Environment variables

Config in your system:

```bash
# Required: private key for signing (base58)
export SOLANA_PRIVATE_KEY="your_private_key"

# Optional (defaults to mainnet)
export SOLANA_RPC_URL="https://api.mainnet-beta.solana.com"
export SOLANA_WS_RPC_URL="wss://api.mainnet-beta.solana.com"
```

Or config .env in root of project to load variables.

### 3. Run tests

```bash
# Run all tests
go test ./tests

# Run swap tests


# Run the main flow (pool discovery → quoting → instruction building; simulation by default)
go test -v ./tests -run TestQueryPoolAndSwap
go test -v ./tests  -run  TestUSDCToSOLSwap
go test -v ./tests  -run  TestSOLToUSDCSwap

# Run discovery or quoting only (no transaction submission)
go test -v ./tests -run TestQueryPoolsOnly
go test -v ./tests -run TestGetBestQuote
```

### 4. Test Swap Overview

- Default mode is simulation (`simulate` defaults to `true` in `tests/swap_test.go`); no on-chain tx, instructions are logged and validated only.
- To send REAL transactions: set `isSimulate = false` in `setupTestSuite` within `tests/swap_test.go`, or pass `false` as the last argument to `SendTx`. Real transactions incur mainnet fees; ensure your wallet has sufficient SOL.
- Token accounts: relevant SPL token accounts are required before swapping. Helper methods are provided: `CoverWsol`, `CloseWsol`, and `SelectOrCreateSPLTokenAccount`. For background, see the Solana docs:
  https://solana.com/developers/cookbook/tokens/get-token-account

## Installation

```bash
go get github.com/Solana-ZH/solroute
```

## Project Structure

```
solroute/
├── pkg/
│   ├── api/         # Core interfaces
│   ├── pool/        # Pool implementations
│   ├── protocol/    # DEX implementations
│   ├── router/      # Routing engine
│   └── sol/         # Solana client
├── tests/           # Contains integration and unit tests to ensure the reliability of swapping and routing logic.
```

## Contribution

Contributed by [yimingWOW](https://github.com/Solana-ZH) from [Solar](https://www.solar.team/).

Contributions are welcome! Please feel free to submit a Pull Request.

### Work in progress

[SeanTong11](https://github.com/SeanTong11/SolRoute) will add Orca's whirpool for SolRoute

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
# SolRouteTmp
