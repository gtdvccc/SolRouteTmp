package tests

import (
	"context"
	"os"
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/Solana-ZH/solroute/pkg/pool/orca"
	"github.com/Solana-ZH/solroute/pkg/pool/raydium"
	"github.com/Solana-ZH/solroute/pkg/protocol"
	"github.com/Solana-ZH/solroute/pkg/router"
	"github.com/Solana-ZH/solroute/pkg/sol"
	"github.com/Solana-ZH/solroute/utils"
	"github.com/gagliardetto/solana-go"
	ata "github.com/gagliardetto/solana-go/programs/associated-token-account"
	computebudget "github.com/gagliardetto/solana-go/programs/compute-budget"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// Token addresses
	usdcTokenAddr = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	// usdc on raydium devnet
	dUsdcTokenAddr = "USDCoctVLVnvTXBEuP9s8hntucdJokbo17RwHuNXemT"
	// usdc on whirlpool devnet
	devUsdcTokenAddr = "BRjpCHtyQLNCo8gqRUr8jtdAj5AjPYQaoqbvcZiHok1k"

	// Swap parameters
	defaultAmountIn = 1000000 // 1 sol (9 decimals) - same as main.go
	slippageBps     = 100     // 1% slippage protection

	// Compute Budget configuration
	computeUnitPrice = 1000   // micro lamports per CU
	computeUnitLimit = 120_000 // max CUs
)

type TestSuite struct {
	ctx        context.Context
	privateKey solana.PrivateKey
	solClient  *sol.Client
	router     *router.SimpleRouter
	simulate   bool
	rpcURL     string
	wsURL      string
	cluster    string
}

// solscanTxURL builds explorer link with cluster suffix from the suite runtime cluster.
func (ts *TestSuite) solscanTxURL(sig string) string {
    if ts.cluster != "" && ts.cluster != "mainnet" {
        return "https://solscan.io/tx/" + sig + "?cluster=" + ts.cluster
    }
    return "https://solscan.io/tx/" + sig
}

// setupTestSuite initializes test environment and creates Solana client
func setupTestSuite(t *testing.T) *TestSuite {
    // Load .env first
    utils.LoadEnv()
	// Get private key from environment variable
	privateKeyStr := os.Getenv("SOLANA_PRIVATE_KEY")
	require.NotEmpty(t, privateKeyStr, "SOLANA_PRIVATE_KEY environment variable is required")

	privateKey := solana.MustPrivateKeyFromBase58(privateKeyStr)
	t.Logf("PublicKey: %v", privateKey.PublicKey())

	ctx := context.Background()

	// Get RPC endpoints from environment variables
	rpcUrl := os.Getenv("SOLANA_RPC_URL")
	if rpcUrl == "" {
		rpcUrl = "https://api.mainnet-beta.solana.com"
	}

	wsRpcUrl := os.Getenv("SOLANA_WS_RPC_URL")
	if wsRpcUrl == "" {
		wsRpcUrl = "wss://api.mainnet-beta.solana.com"
	}

	// Enforce RPC and WS belong to the same cluster
	detectCluster := func(u string) string {
		u = strings.ToLower(u)
		if strings.Contains(u, "devnet") {
			return "devnet"
		}
		if strings.Contains(u, "testnet") {
			return "testnet"
		}
		return "mainnet"
	}

	rpcCluster := detectCluster(rpcUrl)
	wsCluster := detectCluster(wsRpcUrl)
	require.Equal(t, rpcCluster, wsCluster, "RPC URL and WS URL clusters must match (got %s vs %s)", rpcCluster, wsCluster)

	// If devnet, override program IDs for Raydium CLMM and Orca Whirlpool
	if rpcCluster == "devnet" {
		raydium.RAYDIUM_CLMM_PROGRAM_ID = raydium.RAYDIUM_CLMM_DEVNET_PROGRAM_ID
		orca.ORCA_WHIRLPOOL_PROGRAM_ID = orca.ORCA_WHIRLPOOL_DEVNET_PROGRAM_ID
	}

	isSimulate := true // Default to true unless explicitly "false"
	if isSimulate {
		t.Log("Running in SIMULATION mode. No transactions will be sent.")
	} else {
		t.Log("Running in LIVE mode. Transactions will be sent.")
	}

	solClient, err := sol.NewClient(ctx, rpcUrl, wsRpcUrl)
	require.NoError(t, err, "Failed to create solana client")

	// Initialize router with Orca Whirlpool protocol (same as main.go)
	testRouter := router.NewSimpleRouter(
		// protocol.NewOrcaWhirlpool(solClient),
		protocol.NewRaydiumClmm(solClient),
	)

	return &TestSuite{
		ctx:        ctx,
		privateKey: privateKey,
		solClient:  solClient,
		router:     testRouter,
		simulate:   isSimulate,
		rpcURL:     rpcUrl,
		wsURL:      wsRpcUrl,
		cluster:    rpcCluster,
	}
}

// teardownTestSuite cleans up resources after testing
func (ts *TestSuite) teardownTestSuite() {
	if ts.solClient != nil {
		ts.solClient.Close()
	}
}

// setupTokenAccounts prepares WSOL and USDC token accounts
func (ts *TestSuite) setupTokenAccounts(t *testing.T) solana.PublicKey {
	// First check native SOL balance for transaction fees
	solBalance, err := ts.solClient.RpcClient.GetBalance(ts.ctx, ts.privateKey.PublicKey(), rpc.CommitmentConfirmed)
	if err != nil {
		t.Logf("Warning: Could not check SOL balance: %v", err)
	} else {
		t.Logf("Native SOL balance: %v lamports (%.4f SOL)", solBalance.Value, float64(solBalance.Value)/1e9)
		// Require at least 0.1 SOL for transaction fees
		if solBalance.Value < 100000000 { // 0.1 SOL
			t.Skip("Insufficient native SOL balance for transaction fees. Need at least 0.1 SOL.")
		}
	}

	// Check WSOL balance and cover if necessary
	balance, err := ts.solClient.GetUserTokenBalance(ts.ctx, ts.privateKey.PublicKey(), sol.WSOL)
	if err != nil {
		// If no WSOL account exists, balance is 0
		if err.Error() == "no token account found" {
			balance = 0
			t.Log("No WSOL account found, will create one by covering WSOL")
		} else {
			require.NoError(t, err, "Failed to get user token balance")
		}
	}
	t.Logf("User WSOL balance: %v", balance)

	// Always ensure we have enough WSOL by covering if balance is low
	if balance < 10000000 {
		t.Log("WSOL balance too low, covering with 10 WSOL...")
		err = ts.solClient.CoverWsol(ts.ctx, ts.privateKey, 10000000)
		require.NoError(t, err, "Failed to cover wsol")
		t.Log("Successfully covered WSOL")
		
		// Verify balance after covering
		newBalance, err := ts.solClient.GetUserTokenBalance(ts.ctx, ts.privateKey.PublicKey(), sol.WSOL)
		if err == nil {
			t.Logf("WSOL balance after covering: %v", newBalance)
		}
	}

	// Get or create USDC token account
	tokenAccount, err := ts.solClient.SelectOrCreateSPLTokenAccount(ts.ctx, ts.privateKey, solana.MustPublicKeyFromBase58(usdcTokenAddr))
	require.NoError(t, err, "Failed to get USDC token account")
	t.Logf("USDC token account: %v", tokenAccount.String())

	return tokenAccount
}

// TestQueryPoolAndSwap is the main test function that replicates main.go logic
func TestQueryPoolAndSwap(t *testing.T) {
	// Setup test environment
	ts := setupTestSuite(t)
	defer ts.teardownTestSuite()

	// Setup token accounts
	usdcTokenAccount := ts.setupTokenAccounts(t)
	assert.NotEqual(t, solana.PublicKey{}, usdcTokenAccount, "USDC token account should not be empty")

	// Query available pools
	pools, err := ts.router.QueryAllPools(ts.ctx, usdcTokenAddr, sol.WSOL.String())
	require.NoError(t, err, "Failed to query all pools")
	require.NotEmpty(t, pools, "Should find at least one pool")

	for _, pool := range pools {
		t.Logf("Found pool: %v", pool.GetID())
	}

	// Find best pool for the swap
	amountIn := math.NewInt(defaultAmountIn)
	bestPool, amountOut, err := ts.router.GetBestPool(ts.ctx, ts.solClient.RpcClient, sol.WSOL.String(), usdcTokenAddr, amountIn)
	require.NoError(t, err, "Failed to get best pool")
	require.NotNil(t, bestPool, "Best pool should not be nil")
	require.True(t, amountOut.GT(math.ZeroInt()), "Amount out should be greater than zero")

	t.Logf("Selected best pool: %v", bestPool.GetID())
	t.Logf("Expected output amount: %v", amountOut)

	// Calculate minimum output amount with slippage
	minAmountOut := amountOut.Mul(math.NewInt(10000 - slippageBps)).Quo(math.NewInt(10000))
	t.Logf("Amount out: %s, Min amount out: %s (slippage: %d bps)", amountOut.String(), minAmountOut.String(), slippageBps)

	// Build swap instructions (swapping WSOL for USDC)
	instructions, err := bestPool.BuildSwapInstructions(ts.ctx, ts.solClient.RpcClient,
		ts.privateKey.PublicKey(), sol.WSOL.String(), amountIn, minAmountOut)
	require.NoError(t, err, "Failed to build swap instructions")
	require.NotEmpty(t, instructions, "Should generate at least one instruction")

	// Prepend compute budget instructions
	cuPriceIx, err := computebudget.NewSetComputeUnitPriceInstruction(computeUnitPrice).ValidateAndBuild()
	require.NoError(t, err, "failed to build CU price instruction")
	cuLimitIx, err := computebudget.NewSetComputeUnitLimitInstruction(computeUnitLimit).ValidateAndBuild()
	require.NoError(t, err, "failed to build CU limit instruction")
	instructions = append([]solana.Instruction{cuPriceIx, cuLimitIx}, instructions...)

	t.Logf("Generated swap instructions count: %v", len(instructions))

	// Prepare transaction components
	signers := []solana.PrivateKey{ts.privateKey}
	res, err := ts.solClient.RpcClient.GetLatestBlockhash(ts.ctx, rpc.CommitmentFinalized)
	require.NoError(t, err, "Failed to get blockhash")

	// Send transaction (this will execute the actual swap)
	sig, err := ts.solClient.SendTx(ts.ctx, res.Value.Blockhash, signers, instructions, ts.simulate)
	require.NoError(t, err, "Failed to send transaction")
	require.NotEmpty(t, sig, "Transaction signature should not be empty")

	t.Logf("Transaction successful: %s", ts.solscanTxURL(sig.String()))
}

// TestQueryPoolsOnly tests pool discovery without executing swap
func TestQueryPoolsOnly(t *testing.T) {
	ts := setupTestSuite(t)
	defer ts.teardownTestSuite()

	// Query available pools without executing swap
	pools, err := ts.router.QueryAllPools(ts.ctx, usdcTokenAddr, sol.WSOL.String())
	require.NoError(t, err, "Failed to query all pools")

	t.Logf("Total pools found: %d", len(pools))

	for i, pool := range pools {
		t.Logf("Pool %d: %v", i+1, pool.GetID())
	}

	// Verify we found pools
	assert.NotEmpty(t, pools, "Should discover at least one pool for USDC/WSOL pair")
}

// TestGetBestQuote tests the quote functionality without executing swap
func TestGetBestQuote(t *testing.T) {
	ts := setupTestSuite(t)
	defer ts.teardownTestSuite()

	// Query pools first
	pools, err := ts.router.QueryAllPools(ts.ctx, usdcTokenAddr, sol.WSOL.String())
	require.NoError(t, err, "Failed to query all pools")
	require.NotEmpty(t, pools, "Should find at least one pool")

	// Test quote functionality
	amountIn := math.NewInt(defaultAmountIn)
	bestPool, amountOut, err := ts.router.GetBestPool(ts.ctx, ts.solClient.RpcClient, sol.WSOL.String(), usdcTokenAddr, amountIn)
	require.NoError(t, err, "Failed to get best pool")
	require.NotNil(t, bestPool, "Best pool should not be nil")

	t.Logf("Best pool ID: %v", bestPool.GetID())
	t.Logf("Input amount: %v WSOL", amountIn)
	t.Logf("Expected output: %v USDC", amountOut)

	// Validate quote makes sense
	assert.True(t, amountOut.GT(math.ZeroInt()), "Output amount should be positive")

	// Calculate slippage protection
	minAmountOut := amountOut.Mul(math.NewInt(10000 - slippageBps)).Quo(math.NewInt(10000))
	assert.True(t, minAmountOut.GT(math.ZeroInt()), "Min amount out should be positive")
	assert.True(t, minAmountOut.LT(amountOut), "Min amount out should be less than expected amount")
}

// TestInstructionGeneration tests swap instruction building without sending transaction
func TestInstructionGeneration(t *testing.T) {
	ts := setupTestSuite(t)
	defer ts.teardownTestSuite()

	// Setup token accounts
	_ = ts.setupTokenAccounts(t)

	// Get best pool and quote
	amountIn := math.NewInt(defaultAmountIn)
	bestPool, amountOut, err := ts.router.GetBestPool(ts.ctx, ts.solClient.RpcClient, sol.WSOL.String(), usdcTokenAddr, amountIn)
	require.NoError(t, err, "Failed to get best pool")
	require.NotNil(t, bestPool, "Best pool should not be nil")

	// Calculate minimum output with slippage
	minAmountOut := amountOut.Mul(math.NewInt(10000 - slippageBps)).Quo(math.NewInt(10000))

	// Build swap instructions
	instructions, err := bestPool.BuildSwapInstructions(ts.ctx, ts.solClient.RpcClient,
		ts.privateKey.PublicKey(), sol.WSOL.String(), amountIn, minAmountOut)
	require.NoError(t, err, "Failed to build swap instructions")
	require.NotEmpty(t, instructions, "Should generate instructions")

	t.Logf("Generated %d instructions for swap", len(instructions))

	// Validate instructions
	for i, instr := range instructions {
		assert.NotNil(t, instr, "Instruction %d should not be nil", i)
		t.Logf("Instruction %d: Program ID %v, %d accounts", i, instr.ProgramID(), len(instr.Accounts()))
	}
}

// TestSOLToUSDCSwap tests SOL->USDC swap (the problematic direction)
func TestSOLToUSDCSwap(t *testing.T) {
	// Setup test environment
	ts := setupTestSuite(t)
	defer ts.teardownTestSuite()

	// Setup token accounts
	usdcTokenAccount := ts.setupTokenAccounts(t)
	assert.NotEqual(t, solana.PublicKey{}, usdcTokenAccount, "USDC token account should not be empty")

	// Query available pools
	pools, err := ts.router.QueryAllPools(ts.ctx, sol.WSOL.String(), usdcTokenAddr)
	require.NoError(t, err, "Failed to query all pools")
	require.NotEmpty(t, pools, "Should find at least one pool")

	t.Logf("Found %d pools for SOL->USDC", len(pools))

	// Find best pool for SOL->USDC swap
	amountIn := math.NewInt(defaultAmountIn) // 0.001 SOL
	bestPool, amountOut, err := ts.router.GetBestPool(ts.ctx, ts.solClient.RpcClient, sol.WSOL.String(), usdcTokenAddr, amountIn)
	require.NoError(t, err, "Failed to get best pool")
	require.NotNil(t, bestPool, "Best pool should not be nil")
	require.True(t, amountOut.GT(math.ZeroInt()), "Amount out should be greater than zero")

	t.Logf("Selected best pool: %v", bestPool.GetID())
	t.Logf("Input: %v WSOL (0.001 SOL)", amountIn)
	t.Logf("Expected output: %v USDC", amountOut)

	// Calculate SOL price in USDC (considering decimals: SOL=9, USDC=6)
	// Price = (amountOut / 10^6) / (amountIn / 10^9) = amountOut * 10^3 / amountIn
	solPriceInUSDC := amountOut.Mul(math.NewInt(1000)).Quo(amountIn)
	t.Logf("Calculated SOL price: %v USDC per SOL", solPriceInUSDC)

	// Validate price reasonableness (should be between $50-$500 per SOL)
	assert.True(t, solPriceInUSDC.GT(math.NewInt(50)), "SOL price should be > $50")
	assert.True(t, solPriceInUSDC.LT(math.NewInt(500)), "SOL price should be < $500")

	// Calculate minimum output amount with slippage
	minAmountOut := amountOut.Mul(math.NewInt(10000 - slippageBps)).Quo(math.NewInt(10000))
	t.Logf("Min amount out with %d bps slippage: %v USDC", slippageBps, minAmountOut)

	// Build swap instructions
	instructions, err := bestPool.BuildSwapInstructions(ts.ctx, ts.solClient.RpcClient,
		ts.privateKey.PublicKey(), sol.WSOL.String(), amountIn, minAmountOut)
	require.NoError(t, err, "Failed to build swap instructions")
	require.NotEmpty(t, instructions, "Should generate at least one instruction")

	// Prepend compute budget instructions
	cuPriceIx, err := computebudget.NewSetComputeUnitPriceInstruction(computeUnitPrice).ValidateAndBuild()
	require.NoError(t, err, "failed to build CU price instruction")
	cuLimitIx, err := computebudget.NewSetComputeUnitLimitInstruction(computeUnitLimit).ValidateAndBuild()
	require.NoError(t, err, "failed to build CU limit instruction")
	instructions = append([]solana.Instruction{cuPriceIx, cuLimitIx}, instructions...)

	t.Logf("Successfully generated %d swap instructions for SOL->USDC", len(instructions))

	if ts.simulate {
		t.Log("Simulation mode: skipping transaction submission.")
		// Log instruction details for debugging
		for i, instr := range instructions {
			t.Logf("Instruction %d: Program %v, %d accounts", i, instr.ProgramID(), len(instr.Accounts()))
		}
		return
	}

	// Prepare transaction components
	signers := []solana.PrivateKey{ts.privateKey}
	res, err := ts.solClient.RpcClient.GetLatestBlockhash(ts.ctx, rpc.CommitmentFinalized)
	require.NoError(t, err, "Failed to get blockhash")

	// Send transaction (this will execute the actual swap)
	sig, err := ts.solClient.SendTx(ts.ctx, res.Value.Blockhash, signers, instructions, ts.simulate)
	require.NoError(t, err, "Failed to send transaction")
	require.NotEmpty(t, sig, "Transaction signature should not be empty")

	t.Logf("Transaction successful: %s", ts.solscanTxURL(sig.String()))
}

// TestUSDCToSOLSwap tests USDC->SOL swap (the working direction)
func TestUSDCToSOLSwap(t *testing.T) {
	// Setup test environment
	ts := setupTestSuite(t)
	defer ts.teardownTestSuite()

	// Setup token accounts
	usdcTokenAccount := ts.setupTokenAccounts(t)
	assert.NotEqual(t, solana.PublicKey{}, usdcTokenAccount, "USDC token account should not be empty")

	// Query available pools
	pools, err := ts.router.QueryAllPools(ts.ctx, usdcTokenAddr, sol.WSOL.String())
	require.NoError(t, err, "Failed to query all pools")
	require.NotEmpty(t, pools, "Should find at least one pool")

	t.Logf("Found %d pools for USDC->SOL", len(pools))

	// Test with equivalent of 0.001 SOL, which is $0.2 USDC at 200 USDC/SOL
	// 0.2 * 10^6 = 200,000
	amountInUSDC := math.NewInt(200000) // $0.2 USDC
	bestPool, amountOut, err := ts.router.GetBestPool(ts.ctx, ts.solClient.RpcClient, usdcTokenAddr, sol.WSOL.String(), amountInUSDC)
	require.NoError(t, err, "Failed to get best pool")
	require.NotNil(t, bestPool, "Best pool should not be nil")
	require.True(t, amountOut.GT(math.ZeroInt()), "Amount out should be greater than zero")

	t.Logf("Selected best pool: %v", bestPool.GetID())
	t.Logf("Input: %v USDC ($0.2)", amountInUSDC)
	t.Logf("Expected output: %v WSOL", amountOut)

	// Calculate implied SOL price: (Input USDC / 10^6) / (Output SOL / 10^9)
	impliedSOLPrice := amountInUSDC.Mul(math.NewInt(1000)).Quo(amountOut)
	t.Logf("Implied SOL price from reverse swap: %v USDC per SOL", impliedSOLPrice)

	// Calculate minimum output amount with slippage
	minAmountOut := amountOut.Mul(math.NewInt(10000 - slippageBps)).Quo(math.NewInt(10000))
	t.Logf("Min amount out with %d bps slippage: %v WSOL", slippageBps, minAmountOut)

	// Build swap instructions
	instructions, err := bestPool.BuildSwapInstructions(ts.ctx, ts.solClient.RpcClient,
		ts.privateKey.PublicKey(), usdcTokenAddr, amountInUSDC, minAmountOut)
	require.NoError(t, err, "Failed to build swap instructions")
	require.NotEmpty(t, instructions, "Should generate at least one instruction")

	// Ensure WSOL ATA exists for receiving, create if missing
	wsolATA, _, err := solana.FindAssociatedTokenAddress(ts.privateKey.PublicKey(), sol.WSOL)
	require.NoError(t, err, "failed to derive WSOL ATA")
	acctInfo, err := ts.solClient.RpcClient.GetAccountInfo(ts.ctx, wsolATA)
	if err != nil || acctInfo.Value == nil || acctInfo.Value.Owner.IsZero() {
		createATAIx, err := ata.NewCreateInstruction(
			ts.privateKey.PublicKey(),
			ts.privateKey.PublicKey(),
			sol.WSOL,
		).ValidateAndBuild()
		require.NoError(t, err, "failed to build create WSOL ATA instruction")
		instructions = append([]solana.Instruction{createATAIx}, instructions...)
	}

	// Prepend compute budget instructions
	cuPriceIx, err := computebudget.NewSetComputeUnitPriceInstruction(1000).ValidateAndBuild()
	require.NoError(t, err, "failed to build CU price instruction")
	cuLimitIx, err := computebudget.NewSetComputeUnitLimitInstruction(300000).ValidateAndBuild()
	require.NoError(t, err, "failed to build CU limit instruction")
	instructions = append([]solana.Instruction{cuPriceIx, cuLimitIx}, instructions...)

	// Append close WSOL ATA to unwrap to native SOL after swap
	closeIx, err := token.NewCloseAccountInstruction(
		wsolATA,
		ts.privateKey.PublicKey(),
		ts.privateKey.PublicKey(),
		[]solana.PublicKey{},
	).ValidateAndBuild()
	require.NoError(t, err, "failed to build close WSOL ATA instruction")
	instructions = append(instructions, closeIx)

	t.Logf("Successfully generated %d swap instructions for USDC->SOL", len(instructions))

	if ts.simulate {
		t.Log("Simulation mode: skipping transaction submission.")
		return
	}

	// Prepare transaction components
	signers := []solana.PrivateKey{ts.privateKey}
	res, err := ts.solClient.RpcClient.GetLatestBlockhash(ts.ctx, rpc.CommitmentFinalized)
	require.NoError(t, err, "Failed to get blockhash")

	// Send transaction (this will execute the actual swap)
	sig, err := ts.solClient.SendTx(ts.ctx, res.Value.Blockhash, signers, instructions, ts.simulate)
	require.NoError(t, err, "Failed to send transaction")
	require.NotEmpty(t, sig, "Transaction signature should not be empty")

	t.Logf("Transaction successful: %s", ts.solscanTxURL(sig.String()))
}

// TestSOLPriceCalculation specifically tests SOL price calculation accuracy
func TestSOLPriceCalculation(t *testing.T) {
	ts := setupTestSuite(t)
	defer ts.teardownTestSuite()

	// Test both directions to compare prices
	t.Log("=== Testing SOL Price Calculation ===")

	// 1. SOL -> USDC direction
	t.Log("\n--- SOL -> USDC Direction ---")
	pools1, err := ts.router.QueryAllPools(ts.ctx, sol.WSOL.String(), usdcTokenAddr)
	require.NoError(t, err, "Failed to query pools for SOL->USDC")
	require.NotEmpty(t, pools1, "Should find pools")

	amountIn1SOL := math.NewInt(1000000000) // 1 SOL (9 decimals)
	bestPool1, amountOut1, err := ts.router.GetBestPool(ts.ctx, ts.solClient.RpcClient, sol.WSOL.String(), usdcTokenAddr, amountIn1SOL)
	require.NoError(t, err, "Failed to get best pool for SOL->USDC")

	// Calculate price: output USDC / input SOL
	priceFromSOLToUSDC := amountOut1.Abs().Mul(math.NewInt(1000)).Quo(amountIn1SOL) // Scale by 1000 for decimal adjustment
	t.Logf("SOL->USDC: 1 SOL = %v USDC", priceFromSOLToUSDC)
	t.Logf("Pool used: %v", bestPool1.GetID())

	// 2. USDC -> SOL direction  
	t.Log("\n--- USDC -> SOL Direction ---")
	pools2, err := ts.router.QueryAllPools(ts.ctx, usdcTokenAddr, sol.WSOL.String())
	require.NoError(t, err, "Failed to query pools for USDC->SOL")
	require.NotEmpty(t, pools2, "Should find pools")

	// Use the calculated price to test reverse swap
	testUSDCAmount := priceFromSOLToUSDC.Mul(math.NewInt(1000000)) // Convert to USDC units (6 decimals)
	bestPool2, amountOut2, err := ts.router.GetBestPool(ts.ctx, ts.solClient.RpcClient, usdcTokenAddr, sol.WSOL.String(), testUSDCAmount)
	require.NoError(t, err, "Failed to get best pool for USDC->SOL")

	// Calculate implied price from reverse swap
	priceFromUSDCToSOL := testUSDCAmount.Mul(math.NewInt(1000)).Quo(amountOut2.Abs())
	t.Logf("USDC->SOL: Input %v USDC should give ~1 SOL", testUSDCAmount)
	t.Logf("USDC->SOL: Got %v WSOL", amountOut2.Abs())
	t.Logf("USDC->SOL: Implied price = %v USDC per SOL", priceFromUSDCToSOL)
	t.Logf("Pool used: %v", bestPool2.GetID())

	// 3. Compare with external reference (Orca website shows ~203 USDC/SOL)
	t.Log("\n--- Price Analysis ---")
	expectedPrice := math.NewInt(203) // Reference price from Orca website
	t.Logf("Reference price (Orca): %v USDC/SOL", expectedPrice)
	t.Logf("Our SOL->USDC price: %v USDC/SOL", priceFromSOLToUSDC)
	t.Logf("Our USDC->SOL price: %v USDC/SOL", priceFromUSDCToSOL)

	// Calculate deviations
	deviationSOLToUSDC := priceFromSOLToUSDC.Sub(expectedPrice).Abs().Mul(math.NewInt(100)).Quo(expectedPrice)
	deviationUSDCToSOL := priceFromUSDCToSOL.Sub(expectedPrice).Abs().Mul(math.NewInt(100)).Quo(expectedPrice)
	
t.Logf("SOL->USDC deviation: %v%%", deviationSOLToUSDC)
	t.Logf("USDC->SOL deviation: %v%%", deviationUSDCToSOL)

	// Validate that our prices are reasonable (within 10% of reference)
	assert.True(t, deviationSOLToUSDC.LT(math.NewInt(10)), "SOL->USDC price should be within 10%% of reference")
	assert.True(t, deviationUSDCToSOL.LT(math.NewInt(10)), "USDC->SOL price should be within 10%% of reference")

	// Check that both directions give similar prices (arbitrage should keep them close)
	priceDifference := priceFromSOLToUSDC.Sub(priceFromUSDCToSOL).Abs()
	priceDifferencePercent := priceDifference.Mul(math.NewInt(100)).Quo(priceFromSOLToUSDC.Add(priceFromUSDCToSOL).Quo(math.NewInt(2)))
	t.Logf("Price difference between directions: %v%%", priceDifferencePercent)
	
	assert.True(t, priceDifferencePercent.LT(math.NewInt(5)), "Prices from both directions should be within 5%% of each other")
}
