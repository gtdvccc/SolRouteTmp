package orca

import (
	"fmt"
	"math"
	"math/big"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"lukechampine.com/uint128"
)

// WhirlpoolTickArrayBitmapExtensionType - Whirlpool version of tick array bitmap extension
type WhirlpoolTickArrayBitmapExtensionType struct {
	PoolId                  solana.PublicKey
	PositiveTickArrayBitmap [][]uint64
	NegativeTickArrayBitmap [][]uint64
}

// WhirlpoolTickArray - Whirlpool version of tick array, based on CLMM but uses Whirlpool-specific size
type WhirlpoolTickArray struct {
	_                    [8]byte              `bin:"skip"`         // padding
	PoolId               solana.PublicKey     `bin:"fixed"`        // 32 bytes
	StartTickIndex       int32                `bin:"le"`           // 4 bytes
	Ticks                []WhirlpoolTickState `bin:"array,len=88"` // TICK_ARRAY_SIZE=88 for Whirlpool
	InitializedTickCount uint8                // 1 byte
	_                    [115]byte            `bin:"skip"` // padding
}

// WhirlpoolTickState - Whirlpool version of tick state, similar structure to CLMM but fields may differ
type WhirlpoolTickState struct {
	Tick                    int32              `bin:"le"`   // 4 bytes
	LiquidityNet            int64              `bin:"le"`   // 8 bytes
	_                       [8]byte            `bin:"skip"` // skip high 8 bytes
	LiquidityGross          uint128.Uint128    `bin:"le"`   // 16 bytes
	FeeGrowthOutsideX64A    uint128.Uint128    `bin:"le"`   // 16 bytes
	FeeGrowthOutsideX64B    uint128.Uint128    `bin:"le"`   // 16 bytes
	RewardGrowthsOutsideX64 [3]uint128.Uint128 `bin:"le"`   // 48 bytes
	_                       [52]byte           `bin:"skip"` // padding
}

// Decode parses Whirlpool tick array data
func (t *WhirlpoolTickArray) Decode(data []byte) error {
	decoder := bin.NewBinDecoder(data)

	// Decode initial padding
	var padding [8]byte
	err := decoder.Decode(&padding)
	if err != nil {
		return fmt.Errorf("failed to decode padding: %w", err)
	}

	// Decode pool ID
	err = decoder.Decode(&t.PoolId)
	if err != nil {
		return fmt.Errorf("failed to decode pool ID: %w", err)
	}

	// Decode start tick index
	err = decoder.Decode(&t.StartTickIndex)
	if err != nil {
		return fmt.Errorf("failed to decode start tick index: %w", err)
	}

	// Decode ticks array
	t.Ticks = make([]WhirlpoolTickState, TICK_ARRAY_SIZE)
	for i := 0; i < TICK_ARRAY_SIZE; i++ {
		err = decoder.Decode(&t.Ticks[i])
		if err != nil {
			return fmt.Errorf("failed to decode tick %d: %w", i, err)
		}
	}

	// Decode initialized tick count
	err = decoder.Decode(&t.InitializedTickCount)
	if err != nil {
		return fmt.Errorf("failed to decode initialized tick count: %w", err)
	}

	return nil
}

// Whirlpool version utility functions - Copied from CLMM implementation with adjusted parameters

// getTickCount returns the number of ticks in tick array - Whirlpool uses 88 instead of 60
func getWhirlpoolTickCount(tickSpacing int64) int64 {
	return tickSpacing * TICK_ARRAY_SIZE // TICK_ARRAY_SIZE = 88 for Whirlpool
}

// getTickArrayStartIndex gets the start index of tick array
func getWhirlpoolTickArrayStartIndex(tick int64, tickSpacing int64) int64 {
	return tick - tick%getWhirlpoolTickCount(tickSpacing)
}

// GetWhirlpoolTickArrayStartIndexByTick gets tick array start index by tick (exported version)
func GetWhirlpoolTickArrayStartIndexByTick(tickIndex int64, tickSpacing int64) int64 {
	return getWhirlpoolTickArrayStartIndexByTick(tickIndex, tickSpacing)
}

// getWhirlpoolTickArrayStartIndexByTick gets tick array start index by tick
func getWhirlpoolTickArrayStartIndexByTick(tickIndex int64, tickSpacing int64) int64 {
	ticksInArray := getWhirlpoolTickCount(tickSpacing)
	start := math.Floor(float64(tickIndex) / float64(ticksInArray))
	return int64(start * float64(ticksInArray))
}

// maxTickInTickarrayBitmap Whirlpool version of maximum tick
func maxWhirlpoolTickInTickarrayBitmap(tickSpacing int64) int64 {
	return TICK_ARRAY_BITMAP_SIZE * getWhirlpoolTickCount(tickSpacing)
}

// TickArrayOffsetInBitmap calculates tick array offset in bitmap
func WhirlpoolTickArrayOffsetInBitmap(tickArrayStartIndex int64, tickSpacing int64) int64 {
	m := abs(tickArrayStartIndex)
	tickArrayOffsetInBitmap := m / getWhirlpoolTickCount(tickSpacing)

	if tickArrayStartIndex < 0 && m != 0 {
		tickArrayOffsetInBitmap = TICK_ARRAY_BITMAP_SIZE - tickArrayOffsetInBitmap
	}

	return tickArrayOffsetInBitmap
}

// abs returns the absolute value of integer
func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// getFirstInitializedWhirlpoolTickArray - Whirlpool version of getting first initialized tick array
func (pool *WhirlpoolPool) getFirstInitializedWhirlpoolTickArray(zeroForOne bool, exTickArrayBitmap *WhirlpoolTickArrayBitmapExtensionType) (int64, solana.PublicKey, error) {
	// 1. Calculate start index of tick array containing current tick
	startIndex := getWhirlpoolTickArrayStartIndexByTick(int64(pool.TickCurrentIndex), int64(pool.TickSpacing))

	// 2. For simplified implementation, temporarily return calculated start index
	// TODO: Implement complete bitmap lookup logic, refer to CLMM implementation

	// 3. Construct tick array address (using real PDA derivation)
	tickArrayPDA, err := DeriveWhirlpoolTickArrayPDA(pool.PoolId, startIndex)
	if err != nil {
		return 0, solana.PublicKey{}, fmt.Errorf("failed to derive tick array PDA: %w", err)
	}

	return startIndex, tickArrayPDA, nil
}

// isOverflowDefaultWhirlpoolTickarrayBitmap checks if exceeding default bitmap range
func isOverflowDefaultWhirlpoolTickarrayBitmap(tickSpacing int64, tickarrayStartIndexs []int64) bool {
	tickRange := whirlpoolTickRange(tickSpacing)
	maxTickBoundary := tickRange.maxTickBoundary
	minTickBoundary := tickRange.minTickBoundary

	for _, tickIndex := range tickarrayStartIndexs {
		if tickIndex >= maxTickBoundary || tickIndex < minTickBoundary {
			return true
		}
	}
	return false
}

// whirlpoolTickRange gets Whirlpool tick range
func whirlpoolTickRange(tickSpacing int64) struct {
	maxTickBoundary int64
	minTickBoundary int64
} {
	maxTickBoundary := maxWhirlpoolTickInTickarrayBitmap(tickSpacing)
	minTickBoundary := -maxTickBoundary

	if maxTickBoundary > MAX_TICK {
		maxTickBoundary = getWhirlpoolTickArrayStartIndex(MAX_TICK, tickSpacing) + getWhirlpoolTickCount(tickSpacing)
	}
	if minTickBoundary < MIN_TICK {
		minTickBoundary = getWhirlpoolTickArrayStartIndex(MIN_TICK, tickSpacing)
	}
	return struct {
		maxTickBoundary int64
		minTickBoundary int64
	}{
		maxTickBoundary: maxTickBoundary,
		minTickBoundary: minTickBoundary,
	}
}

// Whirlpool version bitmap operation functions - Reuse CLMM logic

// MergeWhirlpoolTickArrayBitmap merges tick array bitmap
func MergeWhirlpoolTickArrayBitmap(bns []uint64) *big.Int {
	result := new(big.Int)

	// Iterate through array
	for i, bn := range bns {
		// Convert uint64 to big.Int
		bnBig := new(big.Int).SetUint64(bn)

		// Shift by 64 * i bits
		bnBig.Lsh(bnBig, uint(64*i))

		// OR with result
		result.Or(result, bnBig)
	}

	return result
}

// IsZero checks if big.Int is zero
func IsZero(bitNum int, data *big.Int) bool {
	return data.Sign() == 0
}

// TrailingZeros calculates the number of trailing zeros
func TrailingZeros(bitNum int, data *big.Int) *int {
	if IsZero(bitNum, data) {
		return nil
	}

	count := 0
	temp := new(big.Int).Set(data)

	for temp.Bit(count) == 0 {
		count++
		if count >= bitNum {
			return nil
		}
	}

	return &count
}

// LeadingZeros calculates the number of leading zeros
func LeadingZeros(bitNum int, data *big.Int) *int {
	if IsZero(bitNum, data) {
		return nil
	}

	// Get position of highest bit
	bitLen := data.BitLen()

	if bitLen == 0 {
		return nil
	}

	// Calculate leading zeros
	leadingZeros := bitNum - bitLen
	if leadingZeros < 0 {
		leadingZeros = 0
	}

	return &leadingZeros
}

// MostSignificantBit gets the most significant bit
func MostSignificantBit(bitNum int, data *big.Int) *int {
	// Check if zero
	if IsZero(bitNum, data) {
		return nil
	}
	// Return number of leading zeros
	return LeadingZeros(bitNum, data)
}

// DeriveWhirlpoolTickArrayPDA derives PDA address for Whirlpool tick array
// Based on Whirlpool source code implementation: seeds = ["tick_array", whirlpool_pubkey, start_tick_index.to_string()]
func DeriveWhirlpoolTickArrayPDA(whirlpoolPubkey solana.PublicKey, startTickIndex int64) (solana.PublicKey, error) {
	// Convert start_tick_index to string byte array, consistent with Whirlpool source code
	// Source code: start_tick_index.to_string().as_bytes()
	startTickIndexStr := fmt.Sprintf("%d", startTickIndex)
	startTickIndexBytes := []byte(startTickIndexStr)

	// Build seeds
	seeds := [][]byte{
		[]byte(TICK_ARRAY_SEED), // "tick_array"
		whirlpoolPubkey.Bytes(), // whirlpool address (32 bytes)
		startTickIndexBytes,     // start_tick_index string bytes
	}

	// Derive PDA
	pda, _, err := solana.FindProgramAddress(seeds, ORCA_WHIRLPOOL_PROGRAM_ID)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("failed to find program address for tick array: %w", err)
	}

	return pda, nil
}

// getOfficialTickArrayStartIndex implements official Whirlpool TickUtil.getStartTickIndex
// Reference: whirlpools/legacy-sdk/whirlpool/src/utils/public/tick-utils.ts:58
func getOfficialTickArrayStartIndex(tickIndex int64, tickSpacing int64, offset int64) (int64, error) {
	// Use precise integer division to match JavaScript Math.floor behavior
	// JavaScript Math.floor(-1.1) = -2, Go math.Floor(-1.1) = -2.0
	dividend := tickIndex
	divisor := tickSpacing * TICK_ARRAY_SIZE
	
	var realIndex int64
	if dividend >= 0 {
		realIndex = dividend / divisor
	} else {
		// For negative numbers, ensure we floor towards negative infinity
		realIndex = dividend / divisor
		if dividend%divisor != 0 {
			realIndex-- // Floor towards negative infinity
		}
	}
	
	startTickIndex := (realIndex + offset) * tickSpacing * TICK_ARRAY_SIZE

	// Bounds check like official implementation but more lenient for edge cases
	ticksInArray := TICK_ARRAY_SIZE * tickSpacing
	minTickIndex := MIN_TICK - ((MIN_TICK%ticksInArray)+ticksInArray)

	// Only validate bounds if they are severely out of range
	// Allow some flexibility for edge cases as official implementation sometimes returns arrays
	// that might be slightly out of normal bounds during sequence generation
	maxBoundary := MAX_TICK + ticksInArray // Allow some buffer
	minBoundary := minTickIndex - ticksInArray // Allow some buffer

	if startTickIndex < minBoundary {
		return 0, fmt.Errorf("startTickIndex is extremely out of bounds (too small) - %d (min boundary: %d)", startTickIndex, minBoundary)
	}
	if startTickIndex > maxBoundary {
		return 0, fmt.Errorf("startTickIndex is extremely out of bounds (too large) - %d (max boundary: %d)", startTickIndex, maxBoundary)
	}

	return startTickIndex, nil
}

// DeriveMultipleWhirlpoolTickArrayPDAs derives multiple tick array PDA addresses
// Based on official Whirlpool implementation
// Reference: whirlpools/legacy-sdk/whirlpool/src/utils/swap-utils.ts:getTickArrayPublicKeysWithStartTickIndex
func DeriveMultipleWhirlpoolTickArrayPDAs(whirlpoolPubkey solana.PublicKey, currentTick int64, tickSpacing int64, aToB bool) (tickArray0, tickArray1, tickArray2 solana.PublicKey, err error) {
	// Apply shift like official implementation
	var shift int64
	if aToB {
		shift = 0
	} else {
		// Per official Whirlpool SDK, shift by +tickSpacing for B->A
		// TickUtil.getStartTickIndex(currentTick + tickSpacing)
		shift = tickSpacing
	}

	tickArrayAddresses := make([]solana.PublicKey, 0, 3)
	offset := int64(0)

	// Generate up to 3 tick arrays like official implementation
	// Follow official implementation: stop early if calculation fails
	for i := 0; i < 3; i++ {
		// Calculate start index using official algorithm
		startIndex, err := getOfficialTickArrayStartIndex(currentTick+shift, tickSpacing, offset)
		if err != nil {
			// Like official implementation, stop generating more tick arrays on error
			// but return what we have so far if we have at least one
			if len(tickArrayAddresses) > 0 {
				break
			}
			return solana.PublicKey{}, solana.PublicKey{}, solana.PublicKey{}, fmt.Errorf("failed to calculate startIndex for tick_array0: %w", err)
		}

		// Derive tick array PDA
		tickArrayPDA, err := DeriveWhirlpoolTickArrayPDA(whirlpoolPubkey, startIndex)
		if err != nil {
			return solana.PublicKey{}, solana.PublicKey{}, solana.PublicKey{}, fmt.Errorf("failed to derive tick_array%d: %w", i, err)
		}

		tickArrayAddresses = append(tickArrayAddresses, tickArrayPDA)

		// Update offset for next iteration
		if aToB {
			offset = offset - 1 // A->B: move to lower tick arrays
		} else {
			offset = offset + 1 // B->A: move to higher tick arrays
		}
	}

	// Ensure we have at least one tick array
	if len(tickArrayAddresses) == 0 {
		return solana.PublicKey{}, solana.PublicKey{}, solana.PublicKey{}, fmt.Errorf("failed to generate any valid tick arrays")
	}

	// Fill missing arrays with empty PublicKeys
	for len(tickArrayAddresses) < 3 {
		tickArrayAddresses = append(tickArrayAddresses, solana.PublicKey{})
	}

	return tickArrayAddresses[0], tickArrayAddresses[1], tickArrayAddresses[2], nil
}

// floorDivision implements integer division (floor), consistent with floor_division in Whirlpool source code
func floorDivision(dividend, divisor int32) int32 {
	if (dividend < 0) != (divisor < 0) && dividend%divisor != 0 {
		return dividend/divisor - 1
	}
	return dividend / divisor
}

// DeriveWhirlpoolOraclePDA derives PDA address for Whirlpool Oracle
// Based on Solana PDA derivation rules: seeds = ["oracle", whirlpool_pubkey]
func DeriveWhirlpoolOraclePDA(whirlpoolPubkey solana.PublicKey) (solana.PublicKey, error) {
	// Build seeds
	seeds := [][]byte{
		[]byte("oracle"),        // "oracle"
		whirlpoolPubkey.Bytes(), // whirlpool address (32 bytes)
	}

	// Derive PDA
	pda, _, err := solana.FindProgramAddress(seeds, ORCA_WHIRLPOOL_PROGRAM_ID)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("failed to find program address for oracle: %w", err)
	}

	return pda, nil
}
