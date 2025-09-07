package protocol

import (
	"context"
	"fmt"

	"github.com/Solana-ZH/solroute/pkg"
	"github.com/Solana-ZH/solroute/pkg/pool/orca"
	"github.com/Solana-ZH/solroute/pkg/sol"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// OrcaWhirlpoolProtocol implements Protocol interface, providing Orca Whirlpool V2 protocol support
//
// Orca Whirlpool is a concentrated liquidity-based automated market maker (CLMM) protocol,
// supporting capital efficiency optimized liquidity provision and trading.
//
// Program ID: whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc
//
// Main features:
// - Concentrated liquidity management
// - Multi-tier fee structure
// - Tick-based price mechanism
// - SwapV2 instruction support
type OrcaWhirlpoolProtocol struct {
	SolClient *sol.Client
}

// NewOrcaWhirlpool creates a new Orca Whirlpool protocol instance
//
// Parameters:
//   - solClient: Solana client for blockchain interaction
//
// Returns:
//   - *OrcaWhirlpoolProtocol: protocol instance
func NewOrcaWhirlpool(solClient *sol.Client) *OrcaWhirlpoolProtocol {
	return &OrcaWhirlpoolProtocol{
		SolClient: solClient,
	}
}

// FetchPoolsByPair gets Whirlpool pool list by token pair
// Reference raydiumClmm.go implementation, adjust field name mapping
func (p *OrcaWhirlpoolProtocol) FetchPoolsByPair(ctx context.Context, baseMint string, quoteMint string) ([]pkg.Pool, error) {
	accounts := make([]*rpc.KeyedAccount, 0)

	// Query pools for baseMint -> quoteMint
	programAccounts, err := p.getWhirlpoolAccountsByTokenPair(ctx, baseMint, quoteMint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pools with base token %s: %w", baseMint, err)
	}
	accounts = append(accounts, programAccounts...)

	// Query pools for quoteMint -> baseMint
	programAccounts, err = p.getWhirlpoolAccountsByTokenPair(ctx, quoteMint, baseMint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pools with base token %s: %w", quoteMint, err)
	}
	accounts = append(accounts, programAccounts...)

	res := make([]pkg.Pool, 0)
	for _, v := range accounts {
		data := v.Account.Data.GetBinary()
		layout := &orca.WhirlpoolPool{}
		if err := layout.Decode(data); err != nil {
			continue
		}
		layout.PoolId = v.Pubkey

		// Add pool quality checks similar to CLMM's IsSwapEnabled check
		// Filter out unhealthy pools at search time to prevent selection of problematic pools
		if healthy, err := layout.IsHealthy(); !healthy {
			// Log the reason but don't fail completely - just skip this pool
			fmt.Printf("Skipping unhealthy pool %s: %v\n", layout.PoolId.String(), err)
			continue
		}

		// Basic pool state validation before adding to results
		if err := layout.ValidatePoolState(); err != nil {
			fmt.Printf("Skipping invalid pool %s: %v\n", layout.PoolId.String(), err)
			continue
		}

		// Critical tick array validation at search time to prevent 6038 errors
		// Check for missing tick arrays that would definitely cause transaction failures
		if err := p.validateCriticalTickArrays(ctx, layout); err != nil {
			fmt.Printf("Skipping pool with critical tick array issues %s: %v\n", layout.PoolId.String(), err)
			continue
		}

		res = append(res, layout)
	}
	return res, nil
}

// getWhirlpoolAccountsByTokenPair queries Whirlpool accounts for specified token pair
// Reference getCLMMPoolAccountsByTokenPair method from raydiumClmm.go
func (p *OrcaWhirlpoolProtocol) getWhirlpoolAccountsByTokenPair(ctx context.Context, baseMint string, quoteMint string) (rpc.GetProgramAccountsResult, error) {
	baseKey, err := solana.PublicKeyFromBase58(baseMint)
	if err != nil {
		return nil, fmt.Errorf("invalid base mint address: %w", err)
	}
	quoteKey, err := solana.PublicKeyFromBase58(quoteMint)
	if err != nil {
		return nil, fmt.Errorf("invalid quote mint address: %w", err)
	}

	// Whirlpool account discriminator (from external/orca/whirlpool/generated/discriminators.go)
	whirlpoolDiscriminator := [8]byte{63, 149, 209, 12, 225, 128, 99, 9}

	var knownPoolLayout orca.WhirlpoolPool
	result, err := p.SolClient.RpcClient.GetProgramAccountsWithOpts(ctx, orca.ORCA_WHIRLPOOL_PROGRAM_ID, &rpc.GetProgramAccountsOpts{
		Filters: []rpc.RPCFilter{
			{
				// First filter Whirlpool discriminator (ensure only querying Whirlpool accounts)
				Memcmp: &rpc.RPCFilterMemcmp{
					Offset: 0, // Discriminator at beginning of account data
					Bytes:  whirlpoolDiscriminator[:],
				},
			},
			{
				DataSize: uint64(knownPoolLayout.Span()),
			},
			{
				Memcmp: &rpc.RPCFilterMemcmp{
					Offset: knownPoolLayout.Offset("TokenMintA"), // Note: CLMM uses TokenMint0
					Bytes:  baseKey.Bytes(),
				},
			},
			{
				Memcmp: &rpc.RPCFilterMemcmp{
					Offset: knownPoolLayout.Offset("TokenMintB"), // Note: CLMM uses TokenMint1
					Bytes:  quoteKey.Bytes(),
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pools: %w", err)
	}

	return result, nil
}

// FetchPoolByID gets single Whirlpool pool by pool ID
// Reference raydiumClmm.go implementation
func (p *OrcaWhirlpoolProtocol) FetchPoolByID(ctx context.Context, poolId string) (pkg.Pool, error) {
	poolIdKey, err := solana.PublicKeyFromBase58(poolId)
	if err != nil {
		return nil, fmt.Errorf("invalid pool id: %w", err)
	}

	account, err := p.SolClient.RpcClient.GetAccountInfo(ctx, poolIdKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool account %s: %w", poolId, err)
	}

	data := account.Value.Data.GetBinary()
	layout := &orca.WhirlpoolPool{}
	if err := layout.Decode(data); err != nil {
		return nil, fmt.Errorf("failed to decode pool data for %s: %w", poolId, err)
	}
	layout.PoolId = poolIdKey

	return layout, nil
}

// validatePoolTickArrays validates pool's tick array integrity to prevent 6038 errors
func (p *OrcaWhirlpoolProtocol) validatePoolTickArrays(ctx context.Context, pool *orca.WhirlpoolPool) error {
	// Check both directions (A->B and B->A) to ensure tick arrays are valid
	directions := []bool{true, false}
	
	for _, aToB := range directions {
		// Get required tick array addresses
		tickArray0, tickArray1, tickArray2, err := orca.DeriveMultipleWhirlpoolTickArrayPDAs(
			pool.PoolId,
			int64(pool.TickCurrentIndex),
			int64(pool.TickSpacing),
			aToB,
		)
		if err != nil {
			return fmt.Errorf("failed to derive tick array PDAs: %w", err)
		}
		
		// Check if primary tick array exists and is valid
		tickArrayAddrs := []solana.PublicKey{tickArray0, tickArray1, tickArray2}
		results, err := p.SolClient.RpcClient.GetMultipleAccountsWithOpts(ctx, tickArrayAddrs, &rpc.GetMultipleAccountsOpts{
			Commitment: rpc.CommitmentProcessed,
		})
		if err != nil {
			// If we can't query tick arrays, it's better to skip this pool
			return fmt.Errorf("failed to query tick arrays: %w", err)
		}
		
		// At least the first tick array must exist
		if results.Value[0] == nil {
			return fmt.Errorf("primary tick array missing for direction aToB=%v", aToB)
		}
		
		// Try to decode the primary tick array to ensure it's valid
		tickArray := &orca.WhirlpoolTickArray{}
		if err := tickArray.Decode(results.Value[0].Data.GetBinary()); err != nil {
			return fmt.Errorf("invalid tick array data: %w", err)
		}
		
		// Basic sanity check on tick array data
		if err := p.validateTickArraySanity(tickArray, pool); err != nil {
			return fmt.Errorf("tick array failed sanity check: %w", err)
		}
	}
	
	return nil
}

// validateTickArraySanity performs basic sanity checks on tick array data
func (p *OrcaWhirlpoolProtocol) validateTickArraySanity(tickArray *orca.WhirlpoolTickArray, pool *orca.WhirlpoolPool) error {
	// Check for abnormally large liquidity_net values that could cause underflow
	for i, tick := range tickArray.Ticks {
		if tick.LiquidityNet < -1e15 { // Much stricter threshold than in IsHealthy
			return fmt.Errorf("tick %d has abnormal liquidity_net: %d", i, tick.LiquidityNet)
		}
		// Also check for suspiciously large positive values
		if tick.LiquidityNet > 1e15 {
			return fmt.Errorf("tick %d has suspiciously large liquidity_net: %d", i, tick.LiquidityNet)
		}
	}
	
	return nil
}

// validateCriticalTickArrays performs essential tick array validations to prevent 6038 errors
// Checks both directions and all required tick arrays to catch missing arrays
func (p *OrcaWhirlpoolProtocol) validateCriticalTickArrays(ctx context.Context, pool *orca.WhirlpoolPool) error {
	// Check both directions to catch missing arrays that would cause 6038 errors
	directions := []bool{true, false} // A->B and B->A
	
	for _, aToB := range directions {
		// Get required tick array addresses
		tickArray0, tickArray1, tickArray2, err := orca.DeriveMultipleWhirlpoolTickArrayPDAs(
			pool.PoolId,
			int64(pool.TickCurrentIndex),
			int64(pool.TickSpacing),
			aToB,
		)
		if err != nil {
			return fmt.Errorf("failed to derive tick array PDAs for direction aToB=%v: %w", aToB, err)
		}
		
		// Check all three tick arrays - missing arrays are the main cause of 6038 errors
		tickArrayAddrs := []solana.PublicKey{tickArray0, tickArray1, tickArray2}
		results, err := p.SolClient.RpcClient.GetMultipleAccountsWithOpts(ctx, tickArrayAddrs, &rpc.GetMultipleAccountsOpts{
			Commitment: rpc.CommitmentProcessed,
		})
		if err != nil {
			return fmt.Errorf("failed to query tick arrays for direction aToB=%v: %w", aToB, err)
		}
		
		// Primary tick array must exist
		if results.Value[0] == nil {
			return fmt.Errorf("primary tick array missing for direction aToB=%v", aToB)
		}
		
		// For proper swap execution, we need at least the first two tick arrays
		// Missing tick array 1 or 2 often causes 6038 errors
		missingArrays := 0
		for i := 1; i < len(results.Value); i++ {
			if results.Value[i] == nil {
				missingArrays++
			}
		}
		
		// If more than one tick array is missing, this pool is problematic
		if missingArrays > 1 {
			return fmt.Errorf("too many missing tick arrays (%d) for direction aToB=%v", missingArrays, aToB)
		}
		
		// Try to decode the primary tick array to ensure it's valid
		tickArray := &orca.WhirlpoolTickArray{}
		if err := tickArray.Decode(results.Value[0].Data.GetBinary()); err != nil {
			return fmt.Errorf("primary tick array corrupted for direction aToB=%v: %w", aToB, err)
		}
		
		// Check for extremely problematic liquidity values that cause underflow
		for _, tick := range tickArray.Ticks {
			if tick.LiquidityNet < -1e18 {
				return fmt.Errorf("tick array has critically bad liquidity_net: %d for direction aToB=%v", tick.LiquidityNet, aToB)
			}
		}
	}
	
	return nil
}
