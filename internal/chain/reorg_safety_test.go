package chain

import (
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256d"
	"github.com/bams-repo/fairchain/internal/consensus/pow"
	"github.com/bams-repo/fairchain/internal/crypto"
	bitcoindiff "github.com/bams-repo/fairchain/internal/difficulty/bitcoin"
	fcparams "github.com/bams-repo/fairchain/internal/params"
	"github.com/bams-repo/fairchain/internal/store"
	"github.com/bams-repo/fairchain/internal/types"
	"github.com/bams-repo/fairchain/internal/utxo"
)

// setupReorgTestChain creates a chain with CoinbaseMaturity=1 so coinbase
// outputs are spendable after one confirmation, making it practical to
// construct transactions that exercise UTXO validation during reorgs.
func setupReorgTestChain(t *testing.T) (*Chain, *fcparams.ChainParams) {
	t.Helper()

	p := &fcparams.ChainParams{}
	*p = *fcparams.Regtest
	p.CoinbaseMaturity = 1

	cfg := fcparams.GenesisConfig{
		NetworkName:     "regtest",
		CoinbaseMessage: []byte("reorg test genesis"),
		Timestamp:       1700000000,
		Bits:            p.InitialBits,
		Version:         1,
		Reward:          p.InitialSubsidy,
		RewardScript:    []byte{0x00},
	}
	genesis := fcparams.BuildGenesisBlock(cfg)
	if err := pow.New(sha256d.New(), bitcoindiff.New()).MineGenesis(&genesis); err != nil {
		t.Fatalf("mine genesis: %v", err)
	}
	genesisHash := crypto.HashBlockHeader(&genesis.Header)
	fcparams.InitGenesis(p, genesis, genesisHash)

	dir := t.TempDir()
	s, err := store.NewFileStore(
		filepath.Join(dir, "blocks"),
		filepath.Join(dir, "blocks", "index"),
		filepath.Join(dir, "chainstate"),
		p.NetworkMagic,
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	engine := pow.New(sha256d.New(), bitcoindiff.New())
	c := New(p, engine, s, nil)
	if err := c.Init(); err != nil {
		t.Fatalf("init chain: %v", err)
	}

	return c, p
}

type chainSnapshot struct {
	tipHash   types.Hash
	tipHeight uint32
	tipWork   *big.Int
	utxoCount int
	utxoValue uint64
}

func takeSnapshot(c *Chain) chainSnapshot {
	h, height := c.Tip()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return chainSnapshot{
		tipHash:   h,
		tipHeight: height,
		tipWork:   new(big.Int).Set(c.tipWork),
		utxoCount: c.utxoSet.Count(),
		utxoValue: c.utxoSet.TotalValue(),
	}
}

func assertSnapshot(t *testing.T, c *Chain, snap chainSnapshot, label string) {
	t.Helper()
	cur := takeSnapshot(c)
	if cur.tipHash != snap.tipHash {
		t.Fatalf("%s: tip hash changed: got %s, want %s", label, cur.tipHash.ReverseString()[:16], snap.tipHash.ReverseString()[:16])
	}
	if cur.tipHeight != snap.tipHeight {
		t.Fatalf("%s: tip height changed: got %d, want %d", label, cur.tipHeight, snap.tipHeight)
	}
	if cur.tipWork.Cmp(snap.tipWork) != 0 {
		t.Fatalf("%s: tip work changed", label)
	}
	if cur.utxoCount != snap.utxoCount {
		t.Fatalf("%s: UTXO count changed: got %d, want %d", label, cur.utxoCount, snap.utxoCount)
	}
	if cur.utxoValue != snap.utxoValue {
		t.Fatalf("%s: UTXO total value changed: got %d, want %d", label, cur.utxoValue, snap.utxoValue)
	}
}

func mineBlockOnParentWithValue(t *testing.T, parentHash types.Hash, parentHeader *types.BlockHeader, parentHeight uint32, p *fcparams.ChainParams, tag string, coinbaseValue uint64) *types.Block {
	t.Helper()

	newHeight := parentHeight + 1
	scriptSig := minimalBIP34ScriptSig(newHeight, []byte(tag))

	coinbase := types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{{
			PreviousOutPoint: types.CoinbaseOutPoint,
			SignatureScript:  scriptSig,
			Sequence:         0xFFFFFFFF,
		}},
		Outputs: []types.TxOutput{
			{Value: coinbaseValue, PkScript: []byte{0x00}},
		},
	}

	merkle, _ := crypto.ComputeMerkleRoot([]types.Transaction{coinbase})
	header := types.BlockHeader{
		Version:    1,
		PrevBlock:  parentHash,
		MerkleRoot: merkle,
		Timestamp:  parentHeader.Timestamp + 1,
		Bits:       p.InitialBits,
		Nonce:      0,
	}

	target := crypto.CompactToHash(header.Bits)
	engine := pow.New(sha256d.New(), bitcoindiff.New())
	found, _ := engine.SealHeader(&header, target, 10_000_000)
	if !found {
		t.Fatalf("could not mine block (tag=%s)", tag)
	}

	return &types.Block{Header: header, Transactions: []types.Transaction{coinbase}}
}

func buildChainOnParent(t *testing.T, parentHash types.Hash, parentHeader *types.BlockHeader, parentHeight uint32, p *fcparams.ChainParams, count int, tagPrefix string) []*types.Block {
	t.Helper()
	blocks := make([]*types.Block, 0, count)
	prevHash := parentHash
	prevHeader := parentHeader
	prevHeight := parentHeight
	for i := 0; i < count; i++ {
		b := mineBlockOnParent(t, prevHash, prevHeader, prevHeight, p, fmt.Sprintf("%s-%d", tagPrefix, i))
		blocks = append(blocks, b)
		prevHash = crypto.HashBlockHeader(&b.Header)
		prevHeader = &b.Header
		prevHeight++
	}
	return blocks
}

// submitAsSideChain submits blocks and expects them all to be stored as side
// chain blocks (not triggering a reorg). Returns the last submitted block's hash.
func submitAsSideChain(t *testing.T, c *Chain, blocks []*types.Block) {
	t.Helper()
	for i, b := range blocks {
		_, err := c.ProcessBlock(b)
		if err == nil {
			t.Fatalf("side chain block %d was accepted as main chain (expected side chain)", i)
		}
		if !errors.Is(err, ErrSideChain) {
			t.Fatalf("side chain block %d: unexpected error: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Scenario 1: Reorg with inflated coinbase on the new chain.
//
// The inflated coinbase block is the LAST block on the side chain — it's the
// one that gives the side chain more work than the main chain, so it's
// included in the reorg path. The trial phase must reject it and leave the
// node on the old chain with zero side effects.
// ---------------------------------------------------------------------------
func TestReorgSafety_InvalidCoinbaseValue(t *testing.T) {
	c, p := setupReorgTestChain(t)

	// Main chain: 5 blocks.
	for i := 0; i < 5; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("main block %d: %v", i, err)
		}
	}
	snap := takeSnapshot(c)

	// Side chain: fork from genesis, build 4 valid blocks (stored as side chain,
	// not enough work to trigger reorg since main has 5).
	genesisHash := p.GenesisHash
	genesisHeader := &p.GenesisBlock.Header
	side := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 4, "side")
	submitAsSideChain(t, c, side)

	// The 5th side chain block would give equal work (5 vs 5), and the 6th
	// would give more work. Make the 6th block have an inflated coinbase.
	lastValid := side[len(side)-1]
	lastValidHash := crypto.HashBlockHeader(&lastValid.Header)
	fifthSide := mineBlockOnParent(t, lastValidHash, &lastValid.Header, 4, p, "side-4")
	fifthSideHash := crypto.HashBlockHeader(&fifthSide.Header)

	// Submit 5th — may or may not trigger reorg depending on tie-breaker.
	c.ProcessBlock(fifthSide)

	// If the 5th triggered a reorg (equal work, lower hash), re-snapshot.
	_, tipH := c.Tip()
	if tipH == 5 && takeSnapshot(c).tipHash != snap.tipHash {
		// Reorg happened on valid blocks — that's fine. Re-snapshot.
		snap = takeSnapshot(c)
	}

	// 6th block: inflated coinbase (2x subsidy).
	inflated := mineBlockOnParentWithValue(t, fifthSideHash, &fifthSide.Header, 5, p, "inflated", p.CalcSubsidy(6)*2)
	_, err := c.ProcessBlock(inflated)
	if err == nil {
		t.Fatal("expected rejection of inflated coinbase block during reorg")
	}
	t.Logf("correctly rejected: %v", err)

	assertSnapshot(t, c, snap, "after inflated coinbase reorg attempt")
}

// ---------------------------------------------------------------------------
// Scenario 2: Reorg with zero-value coinbase output on the new chain.
// Zero-value outputs are rejected by ValidateTransactionInputs.
// ---------------------------------------------------------------------------
func TestReorgSafety_ZeroValueOutput(t *testing.T) {
	c, p := setupReorgTestChain(t)

	for i := 0; i < 5; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("main block %d: %v", i, err)
		}
	}
	snap := takeSnapshot(c)

	genesisHash := p.GenesisHash
	genesisHeader := &p.GenesisBlock.Header
	side := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 5, "zero")
	// Submit first 4 as side chain.
	submitAsSideChain(t, c, side[:4])
	// 5th may trigger equal-work reorg.
	c.ProcessBlock(side[4])

	_, tipH := c.Tip()
	if tipH == 5 && takeSnapshot(c).tipHash != snap.tipHash {
		snap = takeSnapshot(c)
	}

	// 6th block: zero-value coinbase.
	lastSide := side[len(side)-1]
	lastSideHash := crypto.HashBlockHeader(&lastSide.Header)
	zeroBlock := mineBlockOnParentWithValue(t, lastSideHash, &lastSide.Header, 5, p, "zero-val", 0)
	_, err := c.ProcessBlock(zeroBlock)
	if err == nil {
		t.Fatal("expected rejection of zero-value coinbase block")
	}
	t.Logf("correctly rejected: %v", err)

	assertSnapshot(t, c, snap, "after zero-value output reorg attempt")
}

// ---------------------------------------------------------------------------
// Scenario 3: Multiple failed reorg attempts don't corrupt state.
// Each attempt builds a side chain with an inflated coinbase at the tip.
// ---------------------------------------------------------------------------
func TestReorgSafety_RepeatedFailedReorgs(t *testing.T) {
	c, p := setupReorgTestChain(t)

	for i := 0; i < 5; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("main block %d: %v", i, err)
		}
	}
	snap := takeSnapshot(c)

	genesisHash := p.GenesisHash
	genesisHeader := &p.GenesisBlock.Header

	for attempt := 0; attempt < 5; attempt++ {
		// Each attempt: 5 valid blocks + 1 inflated (total 6 > main's 5).
		side := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 5, fmt.Sprintf("fail%d", attempt))
		for _, b := range side {
			c.ProcessBlock(b)
		}

		lastSide := side[len(side)-1]
		lastSideHash := crypto.HashBlockHeader(&lastSide.Header)
		bad := mineBlockOnParentWithValue(t, lastSideHash, &lastSide.Header, 5, p, fmt.Sprintf("bad%d", attempt), p.CalcSubsidy(6)*10)
		c.ProcessBlock(bad)
	}

	// The 5 valid side chain blocks may have triggered reorgs (equal or more work).
	// But the inflated 6th block should always be rejected. The chain should be
	// at height 5 on whichever valid chain won the tie-breaker.
	_, tipH := c.Tip()
	if tipH != 5 {
		t.Fatalf("expected tip at height 5, got %d", tipH)
	}

	// UTXO count should be 5 (one coinbase per block, genesis excluded).
	if c.utxoSet.Count() != 5 {
		t.Fatalf("UTXO count: got %d, want 5", c.utxoSet.Count())
	}
	_ = snap
}

// ---------------------------------------------------------------------------
// Scenario 4: Failed reorg preserves chainstate on disk.
// After a failed reorg, close and reopen the chain — verify consistency.
// ---------------------------------------------------------------------------
func TestReorgSafety_DiskConsistencyAfterFailure(t *testing.T) {
	p := &fcparams.ChainParams{}
	*p = *fcparams.Regtest
	p.CoinbaseMaturity = 1

	cfg := fcparams.GenesisConfig{
		NetworkName:     "regtest",
		CoinbaseMessage: []byte("disk test genesis"),
		Timestamp:       1700000000,
		Bits:            p.InitialBits,
		Version:         1,
		Reward:          p.InitialSubsidy,
		RewardScript:    []byte{0x00},
	}
	genesis := fcparams.BuildGenesisBlock(cfg)
	if err := pow.New(sha256d.New(), bitcoindiff.New()).MineGenesis(&genesis); err != nil {
		t.Fatalf("mine genesis: %v", err)
	}
	genesisHash := crypto.HashBlockHeader(&genesis.Header)
	fcparams.InitGenesis(p, genesis, genesisHash)

	dir := t.TempDir()
	s, err := store.NewFileStore(
		filepath.Join(dir, "blocks"),
		filepath.Join(dir, "blocks", "index"),
		filepath.Join(dir, "chainstate"),
		p.NetworkMagic,
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	engine := pow.New(sha256d.New(), bitcoindiff.New())
	c := New(p, engine, s, nil)
	if err := c.Init(); err != nil {
		t.Fatalf("init chain: %v", err)
	}

	// Build main chain: 5 blocks.
	for i := 0; i < 5; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("main block %d: %v", i, err)
		}
	}
	snap := takeSnapshot(c)

	// Side chain: 5 valid + 1 inflated. The valid blocks may trigger a
	// tie-breaker reorg, so snapshot after submitting them.
	genesisHeader := &p.GenesisBlock.Header
	side := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 5, "disk-side")
	for _, b := range side {
		c.ProcessBlock(b)
	}
	// Re-snapshot after valid side chain blocks (may have reorged).
	snap = takeSnapshot(c)

	lastSide := side[len(side)-1]
	lastSideHash := crypto.HashBlockHeader(&lastSide.Header)
	bad := mineBlockOnParentWithValue(t, lastSideHash, &lastSide.Header, 5, p, "disk-bad", p.CalcSubsidy(6)*10)
	c.ProcessBlock(bad)

	// Verify in-memory state is preserved after failed reorg.
	assertSnapshot(t, c, snap, "in-memory after failed reorg")

	// Close and reopen.
	s.Close()

	s2, err := store.NewFileStore(
		filepath.Join(dir, "blocks"),
		filepath.Join(dir, "blocks", "index"),
		filepath.Join(dir, "chainstate"),
		p.NetworkMagic,
	)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s2.Close()

	c2 := New(p, engine, s2, nil)
	if err := c2.Init(); err != nil {
		t.Fatalf("reinit chain: %v", err)
	}

	snap2 := takeSnapshot(c2)
	if snap2.tipHeight != snap.tipHeight {
		t.Fatalf("disk tip height mismatch: got %d, want %d", snap2.tipHeight, snap.tipHeight)
	}
	if snap2.utxoCount != snap.utxoCount {
		t.Fatalf("disk UTXO count mismatch: got %d, want %d", snap2.utxoCount, snap.utxoCount)
	}
	if snap2.utxoValue != snap.utxoValue {
		t.Fatalf("disk UTXO value mismatch: got %d, want %d", snap2.utxoValue, snap.utxoValue)
	}
}

// ---------------------------------------------------------------------------
// Scenario 5: Successful reorg preserves UTXO integrity.
// ---------------------------------------------------------------------------
func TestReorgSafety_SuccessfulReorgUtxoIntegrity(t *testing.T) {
	c, p := setupReorgTestChain(t)

	for i := 0; i < 3; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("main block %d: %v", i, err)
		}
	}

	// Fork from height 1, build 4 blocks (more work).
	forkHash := c.hashByHeight[1]
	forkHeader, _ := c.store.GetHeader(forkHash)
	side := buildChainOnParent(t, forkHash, forkHeader, 1, p, 4, "utxo-check")

	for _, b := range side {
		c.ProcessBlock(b)
	}

	_, tipH := c.Tip()
	if tipH != 5 {
		t.Fatalf("expected tip at 5 after reorg, got %d", tipH)
	}

	// Heights 1..5 each produce one coinbase output (genesis excluded).
	if c.utxoSet.Count() != 5 {
		t.Fatalf("UTXO count after reorg: got %d, want 5", c.utxoSet.Count())
	}

	var expectedValue uint64
	for h := uint32(1); h <= 5; h++ {
		expectedValue += p.CalcSubsidy(h)
	}
	if c.utxoSet.TotalValue() != expectedValue {
		t.Fatalf("UTXO total value: got %d, want %d", c.utxoSet.TotalValue(), expectedValue)
	}
}

// ---------------------------------------------------------------------------
// Scenario 6: Deep reorg (20 blocks).
// ---------------------------------------------------------------------------
func TestReorgSafety_DeepReorg(t *testing.T) {
	c, p := setupReorgTestChain(t)

	for i := 0; i < 20; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("main block %d: %v", i, err)
		}
	}

	genesisHash := p.GenesisHash
	genesisHeader := &p.GenesisBlock.Header
	side := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 22, "deep")

	for _, b := range side {
		c.ProcessBlock(b)
	}

	_, tipH := c.Tip()
	if tipH != 22 {
		t.Fatalf("expected tip at 22 after deep reorg, got %d", tipH)
	}

	if c.utxoSet.Count() != 22 {
		t.Fatalf("UTXO count after deep reorg: got %d, want 22", c.utxoSet.Count())
	}
}

// ---------------------------------------------------------------------------
// Scenario 7: Two successive reorgs — each must leave the UTXO set consistent.
// Chain A (5 blocks) → Chain B reorgs (7 blocks) → Chain C reorgs (9 blocks).
// All three chains fork from genesis so there are no orphan issues.
// ---------------------------------------------------------------------------
func TestReorgSafety_SuccessiveReorgs(t *testing.T) {
	c, p := setupReorgTestChain(t)

	genesisHash := p.GenesisHash
	genesisHeader := &p.GenesisBlock.Header

	// Chain A: 5 blocks.
	chainA := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 5, "chainA")
	for _, b := range chainA {
		c.ProcessBlock(b)
	}
	_, tipH := c.Tip()
	if tipH != 5 {
		t.Fatalf("expected tip at 5 after chain A, got %d", tipH)
	}
	if c.utxoSet.Count() != 5 {
		t.Fatalf("UTXO count after chain A: got %d, want 5", c.utxoSet.Count())
	}

	// Chain B: 7 blocks from genesis (more work → reorg).
	chainB := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 7, "chainB")
	for _, b := range chainB {
		c.ProcessBlock(b)
	}
	_, tipH = c.Tip()
	if tipH != 7 {
		t.Fatalf("expected tip at 7 after chain B reorg, got %d", tipH)
	}
	if c.utxoSet.Count() != 7 {
		t.Fatalf("UTXO count after chain B: got %d, want 7", c.utxoSet.Count())
	}

	// Chain C: 9 blocks from genesis (more work → reorg again).
	chainC := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 9, "chainC")
	for _, b := range chainC {
		c.ProcessBlock(b)
	}
	_, tipH = c.Tip()
	if tipH != 9 {
		t.Fatalf("expected tip at 9 after chain C reorg, got %d", tipH)
	}
	if c.utxoSet.Count() != 9 {
		t.Fatalf("UTXO count after chain C: got %d, want 9", c.utxoSet.Count())
	}

	// Verify exact subsidy accounting.
	var expectedValue uint64
	for h := uint32(1); h <= 9; h++ {
		expectedValue += p.CalcSubsidy(h)
	}
	if c.utxoSet.TotalValue() != expectedValue {
		t.Fatalf("UTXO total value: got %d, want %d", c.utxoSet.TotalValue(), expectedValue)
	}
}

// ---------------------------------------------------------------------------
// Scenario 8: MaxReorgDepth boundary — too deep is rejected, at-limit succeeds.
// ---------------------------------------------------------------------------
func TestReorgSafety_MaxReorgDepthBoundary(t *testing.T) {
	p := &fcparams.ChainParams{}
	*p = *fcparams.Regtest
	p.CoinbaseMaturity = 1
	p.MaxReorgDepth = 10

	cfg := fcparams.GenesisConfig{
		NetworkName:     "regtest",
		CoinbaseMessage: []byte("maxdepth test"),
		Timestamp:       1700000000,
		Bits:            p.InitialBits,
		Version:         1,
		Reward:          p.InitialSubsidy,
		RewardScript:    []byte{0x00},
	}
	genesis := fcparams.BuildGenesisBlock(cfg)
	if err := pow.New(sha256d.New(), bitcoindiff.New()).MineGenesis(&genesis); err != nil {
		t.Fatalf("mine genesis: %v", err)
	}
	genesisHash := crypto.HashBlockHeader(&genesis.Header)
	fcparams.InitGenesis(p, genesis, genesisHash)

	dir := t.TempDir()
	s, err := store.NewFileStore(
		filepath.Join(dir, "blocks"),
		filepath.Join(dir, "blocks", "index"),
		filepath.Join(dir, "chainstate"),
		p.NetworkMagic,
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	engine := pow.New(sha256d.New(), bitcoindiff.New())
	c := New(p, engine, s, nil)
	if err := c.Init(); err != nil {
		t.Fatalf("init chain: %v", err)
	}

	for i := 0; i < 12; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("main block %d: %v", i, err)
		}
	}
	snap := takeSnapshot(c)

	// Attempt reorg from genesis (depth 12 > limit 10).
	// ProcessBlock wraps ErrReorgTooDeep inside ErrSideChain, so check for either.
	genesisHeader := &p.GenesisBlock.Header
	deepSide := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 14, "tooDeep")
	sawTooDeep := false
	for _, b := range deepSide {
		_, err := c.ProcessBlock(b)
		if errors.Is(err, ErrReorgTooDeep) || errors.Is(err, ErrSideChain) {
			if errors.Is(err, ErrReorgTooDeep) {
				sawTooDeep = true
			}
		}
	}
	if !sawTooDeep {
		// The ErrReorgTooDeep may be wrapped in ErrSideChain — check that
		// the chain tip didn't move (the reorg was rejected).
		assertSnapshot(t, c, snap, "after too-deep reorg rejection (via side chain)")
	} else {
		assertSnapshot(t, c, snap, "after too-deep reorg rejection")
	}

	// Reorg from height 3 (depth 9 <= limit 10) — should succeed.
	forkHash := c.hashByHeight[3]
	forkHeader, _ := c.store.GetHeader(forkHash)
	okSide := buildChainOnParent(t, forkHash, forkHeader, 3, p, 11, "okDepth")
	for _, b := range okSide {
		c.ProcessBlock(b)
	}
	_, tipH := c.Tip()
	if tipH != 14 {
		t.Fatalf("expected tip at 14 after valid reorg, got %d", tipH)
	}
}

// ---------------------------------------------------------------------------
// Scenario 9: Reorg then extend — verify the chain continues normally.
// ---------------------------------------------------------------------------
func TestReorgSafety_ReorgThenExtend(t *testing.T) {
	c, p := setupReorgTestChain(t)

	for i := 0; i < 5; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("main block %d: %v", i, err)
		}
	}

	forkHash := c.hashByHeight[2]
	forkHeader, _ := c.store.GetHeader(forkHash)
	side := buildChainOnParent(t, forkHash, forkHeader, 2, p, 5, "ext")
	for _, b := range side {
		c.ProcessBlock(b)
	}

	_, tipH := c.Tip()
	if tipH != 7 {
		t.Fatalf("expected tip at 7 after reorg, got %d", tipH)
	}

	// Extend the new chain with 3 more blocks via normal ProcessBlock.
	for i := 0; i < 3; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("extend block %d: %v", i, err)
		}
	}

	_, tipH = c.Tip()
	if tipH != 10 {
		t.Fatalf("expected tip at 10 after extension, got %d", tipH)
	}
	if c.utxoSet.Count() != 10 {
		t.Fatalf("UTXO count: got %d, want 10", c.utxoSet.Count())
	}
}

// ---------------------------------------------------------------------------
// Scenario 10: Reorg crossing a retarget boundary.
// ---------------------------------------------------------------------------
func TestReorgSafety_RetargetBoundaryCrossing(t *testing.T) {
	c, p := setupRetargetChain(t, 5, 60*time.Second)

	baseTime := c.params.GenesisBlock.Header.Timestamp

	for i := 1; i <= 8; i++ {
		tipHash, tipHeight := c.Tip()
		tipHeader, _ := c.TipHeader()
		ts := baseTime + uint32(i)*60
		block := mineBlockWithTimestamp(t, tipHash, tipHeader, tipHeight, p, ts, fmt.Sprintf("main-%d", i), c.getAncestorUnsafe)
		_, err := c.ProcessBlock(block)
		if err != nil {
			t.Fatalf("main chain block %d: %v", i, err)
		}
	}

	forkHash := c.hashByHeight[3]
	forkHeader, _ := c.store.GetHeader(forkHash)
	forkHeight := uint32(3)

	sideHeaders := make(map[uint32]*types.BlockHeader)
	for h := uint32(0); h <= forkHeight; h++ {
		hash := c.hashByHeight[h]
		hdr, _ := c.store.GetHeader(hash)
		sideHeaders[h] = hdr
	}
	sideGetAncestor := func(h uint32) *types.BlockHeader {
		if hdr, ok := sideHeaders[h]; ok {
			return hdr
		}
		return c.getAncestorUnsafe(h)
	}

	prevHash := forkHash
	prevHeader := forkHeader
	prevHeight := forkHeight
	var sideBlocks []*types.Block

	for i := 4; i <= 10; i++ {
		ts := forkHeader.Timestamp + uint32(i-3)*90
		block := mineBlockWithTimestamp(t, prevHash, prevHeader, prevHeight, p, ts, fmt.Sprintf("retarget-side-%d", i), sideGetAncestor)
		blockHash := crypto.HashBlockHeader(&block.Header)
		sideHeaders[uint32(i)] = &block.Header
		sideBlocks = append(sideBlocks, block)
		prevHash = blockHash
		prevHeader = &block.Header
		prevHeight = uint32(i)
	}

	for _, b := range sideBlocks {
		c.ProcessBlock(b)
	}

	_, tipH := c.Tip()
	if tipH != 10 {
		t.Fatalf("expected tip at 10 after retarget reorg, got %d", tipH)
	}
	if c.utxoSet.Count() != 10 {
		t.Fatalf("UTXO count after retarget reorg: got %d, want 10", c.utxoSet.Count())
	}
}

// ---------------------------------------------------------------------------
// Scenario 11: Verify cloneUtxoSet produces an independent copy.
// ---------------------------------------------------------------------------
func TestCloneUtxoSetIndependence(t *testing.T) {
	original := utxo.NewSet()
	var hash1, hash2 types.Hash
	hash1[0] = 0x01
	hash2[0] = 0x02

	original.Add(hash1, 0, &utxo.UtxoEntry{Value: 100, PkScript: []byte{0xAA}, Height: 1})
	original.Add(hash2, 0, &utxo.UtxoEntry{Value: 200, PkScript: []byte{0xBB}, Height: 2})

	clone := cloneUtxoSet(original)

	clone.Remove(hash1, 0)
	var hash3 types.Hash
	hash3[0] = 0x03
	clone.Add(hash3, 0, &utxo.UtxoEntry{Value: 300, PkScript: []byte{0xCC}, Height: 3})

	if original.Count() != 2 {
		t.Fatalf("original count changed: got %d, want 2", original.Count())
	}
	if original.Get(hash1, 0) == nil {
		t.Fatal("original lost hash1 entry")
	}
	if original.Get(hash3, 0) != nil {
		t.Fatal("original gained hash3 entry from clone mutation")
	}
	if clone.Count() != 2 {
		t.Fatalf("clone count: got %d, want 2", clone.Count())
	}
	if clone.Get(hash1, 0) != nil {
		t.Fatal("clone still has hash1 after removal")
	}
	if clone.Get(hash3, 0) == nil {
		t.Fatal("clone missing hash3 after add")
	}
}

// ---------------------------------------------------------------------------
// Scenario 12: Valid reorg succeeds, then an invalid reorg attempt against
// the new chain is rejected without corrupting state.
// ---------------------------------------------------------------------------
func TestReorgSafety_ValidReorgThenInvalidReorg(t *testing.T) {
	c, p := setupReorgTestChain(t)

	genesisHash := p.GenesisHash
	genesisHeader := &p.GenesisBlock.Header

	// Chain A: 5 blocks from genesis.
	chainA := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 5, "validA")
	for _, b := range chainA {
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("chain A block: %v", err)
		}
	}
	_, tipH := c.Tip()
	if tipH != 5 {
		t.Fatalf("expected tip at 5, got %d", tipH)
	}

	// Chain B: 5 valid blocks + 1 inflated (total 6 > 5 → triggers reorg).
	// The inflated block is the one that gives B more work, so it's in the
	// reorg path and must be caught by the trial phase.
	chainB := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 5, "invalidB")
	for _, b := range chainB {
		c.ProcessBlock(b)
	}
	// Snapshot after the valid B blocks (may have reorged on tie-breaker).
	snap := takeSnapshot(c)

	lastB := chainB[len(chainB)-1]
	lastBHash := crypto.HashBlockHeader(&lastB.Header)
	bad := mineBlockOnParentWithValue(t, lastBHash, &lastB.Header, 5, p, "bad-B", p.CalcSubsidy(6)*5)
	_, err := c.ProcessBlock(bad)
	if err == nil {
		t.Fatal("expected rejection of inflated coinbase on chain B")
	}
	t.Logf("correctly rejected: %v", err)

	assertSnapshot(t, c, snap, "after invalid second reorg attempt")
}

// ---------------------------------------------------------------------------
// Scenario 13 (bonus): Verify UTXO total value is exactly correct after
// multiple reorgs with different subsidy schedules.
// ---------------------------------------------------------------------------
func TestReorgSafety_SubsidyAccountingAfterReorgs(t *testing.T) {
	c, p := setupReorgTestChain(t)

	// Build 10 blocks on main chain.
	for i := 0; i < 10; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("main block %d: %v", i, err)
		}
	}

	// Reorg to a chain from genesis with 12 blocks.
	genesisHash := p.GenesisHash
	genesisHeader := &p.GenesisBlock.Header
	side := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 12, "subsidy")
	for _, b := range side {
		c.ProcessBlock(b)
	}

	_, tipH := c.Tip()
	if tipH != 12 {
		t.Fatalf("expected tip at 12, got %d", tipH)
	}

	// Verify exact subsidy accounting.
	var expectedValue uint64
	for h := uint32(1); h <= 12; h++ {
		expectedValue += p.CalcSubsidy(h)
	}
	if c.utxoSet.TotalValue() != expectedValue {
		t.Fatalf("UTXO total value: got %d, want %d", c.utxoSet.TotalValue(), expectedValue)
	}
	if c.utxoSet.Count() != 12 {
		t.Fatalf("UTXO count: got %d, want 12", c.utxoSet.Count())
	}
}
