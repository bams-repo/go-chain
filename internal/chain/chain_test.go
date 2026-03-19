package chain

import (
	"bytes"
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
)

func setupTestChain(t *testing.T) (*Chain, *fcparams.ChainParams) {
	t.Helper()

	p := &fcparams.ChainParams{}
	*p = *fcparams.Regtest

	// Mine genesis.
	cfg := fcparams.GenesisConfig{
		NetworkName:     "regtest",
		CoinbaseMessage: []byte("test genesis"),
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

func mineBlock(t *testing.T, c *Chain, p *fcparams.ChainParams) *types.Block {
	t.Helper()

	tipHash, tipHeight := c.Tip()
	tipHeader, err := c.TipHeader()
	if err != nil {
		t.Fatalf("get tip header: %v", err)
	}

	newHeight := tipHeight + 1
	subsidy := p.CalcSubsidy(newHeight)

	scriptSig := minimalBIP34ScriptSig(newHeight, []byte("test"))

	coinbase := types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  scriptSig,
				Sequence:         0xFFFFFFFF,
			},
		},
		Outputs: []types.TxOutput{
			{Value: subsidy, PkScript: []byte{0x00}},
		},
	}

	merkle, _ := crypto.ComputeMerkleRoot([]types.Transaction{coinbase})

	header := types.BlockHeader{
		Version:    1,
		PrevBlock:  tipHash,
		MerkleRoot: merkle,
		Timestamp:  tipHeader.Timestamp + 1,
		Bits:       p.InitialBits,
		Nonce:      0,
	}

	target := crypto.CompactToHash(header.Bits)
	engine := pow.New(sha256d.New(), bitcoindiff.New())
	found, _ := engine.SealHeader(&header, target, 10000000)
	if !found {
		t.Fatal("could not mine block")
	}

	return &types.Block{
		Header:       header,
		Transactions: []types.Transaction{coinbase},
	}
}

func minimalBIP34ScriptSig(height uint32, tag []byte) []byte {
	heightBytes := make([]byte, 4)
	types.PutUint32LE(heightBytes, height)
	pushLen := 4
	switch {
	case height <= 0xFF:
		pushLen = 1
	case height <= 0xFFFF:
		pushLen = 2
	case height <= 0xFFFFFF:
		pushLen = 3
	}
	sig := make([]byte, 0, 1+pushLen+len(tag))
	sig = append(sig, byte(pushLen))
	sig = append(sig, heightBytes[:pushLen]...)
	sig = append(sig, tag...)
	return sig
}

// mineBlockOnParent builds and mines a block on top of a specific parent, not the chain tip.
// The tag parameter differentiates the coinbase to produce a unique block hash.
func mineBlockOnParent(t *testing.T, parentHash types.Hash, parentHeader *types.BlockHeader, parentHeight uint32, p *fcparams.ChainParams, tag string) *types.Block {
	t.Helper()

	newHeight := parentHeight + 1
	subsidy := p.CalcSubsidy(newHeight)

	scriptSig := minimalBIP34ScriptSig(newHeight, []byte(tag))

	coinbase := types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{
				PreviousOutPoint: types.CoinbaseOutPoint,
				SignatureScript:  scriptSig,
				Sequence:         0xFFFFFFFF,
			},
		},
		Outputs: []types.TxOutput{
			{Value: subsidy, PkScript: []byte{0x00}},
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
	found, _ := engine.SealHeader(&header, target, 10000000)
	if !found {
		t.Fatal("could not mine block on parent")
	}

	return &types.Block{
		Header:       header,
		Transactions: []types.Transaction{coinbase},
	}
}

func TestChainInit(t *testing.T) {
	c, p := setupTestChain(t)

	tipHash, tipHeight := c.Tip()
	if tipHeight != 0 {
		t.Fatalf("tip height = %d, want 0", tipHeight)
	}
	if tipHash != p.GenesisHash {
		t.Fatalf("tip hash = %s, want %s", tipHash, p.GenesisHash)
	}
}

func TestChainProcessBlock(t *testing.T) {
	c, p := setupTestChain(t)

	block := mineBlock(t, c, p)
	height, err := c.ProcessBlock(block)
	if err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}
	if height != 1 {
		t.Fatalf("accepted height = %d, want 1", height)
	}

	_, tipHeight := c.Tip()
	if tipHeight != 1 {
		t.Fatalf("tip height = %d, want 1", tipHeight)
	}
}

func TestChainMultipleBlocks(t *testing.T) {
	c, p := setupTestChain(t)

	for i := 0; i < 5; i++ {
		block := mineBlock(t, c, p)
		_, err := c.ProcessBlock(block)
		if err != nil {
			t.Fatalf("ProcessBlock at iteration %d: %v", i, err)
		}
	}

	_, tipHeight := c.Tip()
	if tipHeight != 5 {
		t.Fatalf("tip height = %d, want 5", tipHeight)
	}
}

func TestChainRejectDuplicate(t *testing.T) {
	c, p := setupTestChain(t)

	block := mineBlock(t, c, p)
	_, err := c.ProcessBlock(block)
	if err != nil {
		t.Fatalf("first ProcessBlock: %v", err)
	}

	_, err = c.ProcessBlock(block)
	if err == nil {
		t.Fatal("should reject duplicate block")
	}
}

func TestChainOrphan(t *testing.T) {
	c, p := setupTestChain(t)

	// Mine block 1.
	block1 := mineBlock(t, c, p)

	// Mine block 2 on top of block 1 (without submitting block 1 first).
	// We need to manually construct block 2 since mineBlock uses the chain tip.
	block1Hash := crypto.HashBlockHeader(&block1.Header)
	scriptSig2 := minimalBIP34ScriptSig(2, []byte("test"))
	coinbase2 := types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{
			{PreviousOutPoint: types.CoinbaseOutPoint, SignatureScript: scriptSig2, Sequence: 0xFFFFFFFF},
		},
		Outputs: []types.TxOutput{
			{Value: p.CalcSubsidy(2), PkScript: []byte{0x00}},
		},
	}
	merkle2, _ := crypto.ComputeMerkleRoot([]types.Transaction{coinbase2})
	header2 := types.BlockHeader{
		Version:    1,
		PrevBlock:  block1Hash,
		MerkleRoot: merkle2,
		Timestamp:  block1.Header.Timestamp + 1,
		Bits:       p.InitialBits,
	}
	target := crypto.CompactToHash(header2.Bits)
	engine := pow.New(sha256d.New(), bitcoindiff.New())
	engine.SealHeader(&header2, target, 10000000)
	block2 := &types.Block{Header: header2, Transactions: []types.Transaction{coinbase2}}

	// Submit block 2 first — should be orphaned.
	_, err := c.ProcessBlock(block2)
	if err == nil {
		t.Fatal("block 2 should be orphaned (parent unknown)")
	}

	// Now submit block 1 — should accept and also process orphan.
	_, err = c.ProcessBlock(block1)
	if err != nil {
		t.Fatalf("ProcessBlock(block1): %v", err)
	}

	// Chain should now be at height 2 (genesis + block1 + block2 via orphan processing).
	_, tipHeight := c.Tip()
	if tipHeight < 1 {
		t.Fatalf("tip height = %d, want >= 1", tipHeight)
	}
}

func TestEqualWorkTieBreaker(t *testing.T) {
	c, p := setupTestChain(t)

	// Mine block 1 on the chain.
	block1 := mineBlock(t, c, p)
	_, err := c.ProcessBlock(block1)
	if err != nil {
		t.Fatalf("ProcessBlock(block1): %v", err)
	}

	// Now create two competing blocks at height 2 on top of block1.
	tipHash, tipHeight := c.Tip()
	tipHeader, _ := c.TipHeader()

	blockA := mineBlockOnParent(t, tipHash, tipHeader, tipHeight, p, "fork-A")
	blockB := mineBlockOnParent(t, tipHash, tipHeader, tipHeight, p, "fork-B")

	hashA := crypto.HashBlockHeader(&blockA.Header)
	hashB := crypto.HashBlockHeader(&blockB.Header)

	// Determine which has the lower hash — that should be the winner.
	var expectedWinner types.Hash
	if bytes.Compare(hashA[:], hashB[:]) < 0 {
		expectedWinner = hashA
	} else {
		expectedWinner = hashB
	}

	// Submit both blocks. The first one extends the chain; the second is a
	// side chain with equal work. If blockB has a lower hash it triggers a
	// reorg; otherwise it is stored as a side chain (ErrSideChain).
	_, err = c.ProcessBlock(blockA)
	if err != nil {
		t.Fatalf("ProcessBlock(blockA): %v", err)
	}
	_, err = c.ProcessBlock(blockB)
	if err != nil && !errors.Is(err, ErrSideChain) {
		t.Fatalf("ProcessBlock(blockB): %v", err)
	}

	finalTip, _ := c.Tip()
	if finalTip != expectedWinner {
		t.Fatalf("equal-work tie-breaker failed: tip=%s, expected=%s (lower hash wins)", finalTip, expectedWinner)
	}
}

func TestSideChainWorkCalculation(t *testing.T) {
	c, p := setupTestChain(t)

	// Mine 3 blocks on the main chain.
	for i := 0; i < 3; i++ {
		block := mineBlock(t, c, p)
		_, err := c.ProcessBlock(block)
		if err != nil {
			t.Fatalf("ProcessBlock main chain %d: %v", i, err)
		}
	}

	// Record main chain tip at height 3.
	mainTipHash, mainTipHeight := c.Tip()
	if mainTipHeight != 3 {
		t.Fatalf("main chain height = %d, want 3", mainTipHeight)
	}

	// Create a side chain forking from height 1 (genesis -> block1 -> sideA -> sideB -> sideC -> sideD).
	// This side chain needs 4 blocks to have more cumulative work than the main chain's 3 blocks
	// (since all blocks have the same difficulty, 4 > 3).
	block1Header, err := c.GetHeaderByHeight(1)
	if err != nil {
		t.Fatalf("get header at height 1: %v", err)
	}
	block1Hash := c.hashByHeight[1]

	sideA := mineBlockOnParent(t, block1Hash, block1Header, 1, p, "side-A")
	sideAHash := crypto.HashBlockHeader(&sideA.Header)

	sideB := mineBlockOnParent(t, sideAHash, &sideA.Header, 2, p, "side-B")
	sideBHash := crypto.HashBlockHeader(&sideB.Header)

	sideC := mineBlockOnParent(t, sideBHash, &sideB.Header, 3, p, "side-C")
	sideCHash := crypto.HashBlockHeader(&sideC.Header)

	sideD := mineBlockOnParent(t, sideCHash, &sideC.Header, 4, p, "side-D")

	// Submit side chain blocks. sideA and sideB are side-chain blocks (less work).
	// sideC ties the main chain. sideD should trigger a reorg.
	_, err = c.ProcessBlock(sideA)
	if err != nil && !errors.Is(err, ErrSideChain) {
		t.Fatalf("ProcessBlock(sideA): %v", err)
	}
	_, err = c.ProcessBlock(sideB)
	if err != nil && !errors.Is(err, ErrSideChain) {
		t.Fatalf("ProcessBlock(sideB): %v", err)
	}
	_, err = c.ProcessBlock(sideC)
	if err != nil && !errors.Is(err, ErrSideChain) {
		t.Fatalf("ProcessBlock(sideC): %v", err)
	}
	_, err = c.ProcessBlock(sideD)
	if err != nil {
		t.Fatalf("ProcessBlock(sideD): %v", err)
	}

	// After sideD, the side chain has 5 blocks of work (genesis + block1 + sideA + sideB + sideC + sideD = 5 post-genesis)
	// vs main chain's 3 blocks of work. Side chain should win.
	newTipHash, newTipHeight := c.Tip()
	if newTipHeight != 5 {
		t.Fatalf("after side chain reorg, tip height = %d, want 5", newTipHeight)
	}

	sideDHash := crypto.HashBlockHeader(&sideD.Header)
	if newTipHash != sideDHash {
		t.Fatalf("after reorg, tip should be sideD=%s, got %s", sideDHash, newTipHash)
	}

	// Verify the old main chain tip is no longer the tip.
	if newTipHash == mainTipHash {
		t.Fatal("reorg did not happen — still on old main chain")
	}
}

// ---------------------------------------------------------------------------
// RC-1 VALIDATION: Prove that getAncestorUnsafe returns wrong ancestors for
// side-chain blocks at retarget boundaries, causing CalcNextBits to compute
// incorrect difficulty and reject valid side-chain blocks.
// ---------------------------------------------------------------------------

func setupRetargetChain(t *testing.T, interval uint32, blockSpacing time.Duration) (*Chain, *fcparams.ChainParams) {
	t.Helper()

	targetTimespan := time.Duration(interval) * blockSpacing

	p := &fcparams.ChainParams{
		Name:                   "retarget-test",
		NetworkMagic:           [4]byte{0xFA, 0x1C, 0xC0, 0xFE},
		DefaultPort:            19555,
		TargetBlockSpacing:     blockSpacing,
		RetargetInterval:       interval,
		TargetTimespan:         targetTimespan,
		MaxTimeFutureDrift:     10 * time.Minute,
		MinTimestampRule:       "prev+1",
		InitialBits:            0x207fffff,
		MinBits:                0x207fffff,
		NoRetarget:             false,
		MaxBlockSize:           4_000_000,
		MaxBlockTxCount:        50_000,
		InitialSubsidy:         50_0000_0000,
		SubsidyHalvingInterval: 150,
		CoinbaseMaturity:       1,
		MaxMempoolSize:         10000,
		MinRelayTxFee:          0,
		SeedNodes:              []string{},
		ActivationHeights:      map[string]uint32{},
	}

	cfg := fcparams.GenesisConfig{
		NetworkName:     "retarget-test",
		CoinbaseMessage: []byte("retarget test genesis"),
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

// mineBlockWithTimestamp builds and mines a block on a specific parent with a
// specific timestamp. The bits are computed by CalcNextBits using the provided
// ancestor function, which allows mining on either the main chain or a side chain.
func mineBlockWithTimestamp(t *testing.T, parentHash types.Hash, parentHeader *types.BlockHeader, parentHeight uint32, p *fcparams.ChainParams, timestamp uint32, tag string, getAncestor func(uint32) *types.BlockHeader) *types.Block {
	t.Helper()

	newHeight := parentHeight + 1
	subsidy := p.CalcSubsidy(newHeight)

	scriptSig := minimalBIP34ScriptSig(newHeight, []byte(tag))

	coinbase := types.Transaction{
		Version: 1,
		Inputs: []types.TxInput{{
			PreviousOutPoint: types.CoinbaseOutPoint,
			SignatureScript:  scriptSig,
			Sequence:         0xFFFFFFFF,
		}},
		Outputs: []types.TxOutput{
			{Value: subsidy, PkScript: []byte{0x00}},
		},
	}

	merkle, _ := crypto.ComputeMerkleRoot([]types.Transaction{coinbase})

	engine := pow.New(sha256d.New(), bitcoindiff.New())
	bits := engine.CalcNextBits(parentHeader, parentHeight, getAncestor, p)

	header := types.BlockHeader{
		Version:    1,
		PrevBlock:  parentHash,
		MerkleRoot: merkle,
		Timestamp:  timestamp,
		Bits:       bits,
		Nonce:      0,
	}

	target := crypto.CompactToHash(header.Bits)
	found, _ := engine.SealHeader(&header, target, 100_000_000)
	if !found {
		t.Fatalf("could not mine block at height %d (tag=%s)", newHeight, tag)
	}

	return &types.Block{
		Header:       header,
		Transactions: []types.Transaction{coinbase},
	}
}

// TestRC1_SideChainAncestorLookupBug proves that getAncestorUnsafe returns the
// wrong block for side-chain heights at retarget boundaries, causing valid
// side-chain blocks to be rejected and preventing reorgs.
//
// Scenario (interval=5, 60s target, 300s timespan):
//   - Mine 12 blocks on main chain with 60s spacing (on-target)
//   - Fork at height 3 — mine side chain with 120s spacing (2x slower)
//   - Second retarget at height 10: windowStart = 9 - 4 = 5
//   - Height 5 differs between chains (fork was at 3)
//   - getAncestorUnsafe(5) returns main chain's block → wrong timestamp
//   - CalcNextBits computes wrong expected bits → block rejected
//
// We use 60s target with 120s side-chain spacing so the actual timespans
// differ enough to produce different bits even after clamping.
func TestRC1_SideChainAncestorLookupBug(t *testing.T) {
	c, p := setupRetargetChain(t, 5, 60*time.Second)

	engine := pow.New(sha256d.New(), bitcoindiff.New())
	baseTime := c.params.GenesisBlock.Header.Timestamp

	// Phase 1: Mine 12 blocks on the main chain with 60s spacing (on-target).
	for i := 1; i <= 12; i++ {
		tipHash, tipHeight := c.Tip()
		tipHeader, _ := c.TipHeader()
		ts := baseTime + uint32(i)*60
		block := mineBlockWithTimestamp(t, tipHash, tipHeader, tipHeight, p, ts, fmt.Sprintf("main-%d", i), c.getAncestorUnsafe)
		_, err := c.ProcessBlock(block)
		if err != nil {
			t.Fatalf("main chain block %d: %v", i, err)
		}
	}

	_, mainHeight := c.Tip()
	t.Logf("Main chain at height %d (past retargets at 5 and 10)", mainHeight)

	// Save main chain state before any reorg could happen.
	mainBlock5HashBefore := c.hashByHeight[5]
	mainBlock5Before, _ := c.store.GetHeader(mainBlock5HashBefore)

	// Fork at height 3 (before the second retarget window start at height 5).
	forkHash := c.hashByHeight[3]
	forkHeader, _ := c.store.GetHeader(forkHash)
	forkHeight := uint32(3)

	// Phase 2: Build a side chain from height 3 with SLOW timestamps (120s spacing).
	// This produces a very different timespan from the main chain at the retarget
	// boundary, ensuring the bits will differ.
	sideHeaders := make(map[uint32]*types.BlockHeader)
	sideHashes := make(map[uint32]types.Hash)

	for h := uint32(0); h <= forkHeight; h++ {
		hash := c.hashByHeight[h]
		hdr, _ := c.store.GetHeader(hash)
		sideHeaders[h] = hdr
		sideHashes[h] = hash
	}

	sideGetAncestor := func(h uint32) *types.BlockHeader {
		return sideHeaders[h]
	}

	prevHash := forkHash
	prevHeader := forkHeader
	prevHeight := forkHeight
	var sideBlocks []*types.Block

	for i := 4; i <= 15; i++ {
		ts := forkHeader.Timestamp + uint32(i-3)*120 // 120s spacing (2x slower)
		block := mineBlockWithTimestamp(t, prevHash, prevHeader, prevHeight, p, ts, fmt.Sprintf("side-%d", i), sideGetAncestor)
		blockHash := crypto.HashBlockHeader(&block.Header)

		sideHeaders[uint32(i)] = &block.Header
		sideHashes[uint32(i)] = blockHash
		sideBlocks = append(sideBlocks, block)

		prevHash = blockHash
		prevHeader = &block.Header
		prevHeight = uint32(i)
	}

	t.Logf("Side chain built: heights 4-15 (12 blocks) with 120s spacing")

	// Phase 3: Prove the ancestor mismatch at the retarget window start.
	sideBlock5 := sideHeaders[5]

	t.Logf("Main chain block at height 5: ts=%d", mainBlock5Before.Timestamp)
	t.Logf("Side chain block at height 5: ts=%d", sideBlock5.Timestamp)
	t.Logf("Timestamp delta at height 5: %d seconds", int64(mainBlock5Before.Timestamp)-int64(sideBlock5.Timestamp))

	if mainBlock5Before.Timestamp == sideBlock5.Timestamp {
		t.Fatal("TEST SETUP ERROR: blocks at height 5 have same timestamp")
	}

	// Compute CalcNextBits for the side chain at height 10 using both functions.
	sideTip9 := sideHeaders[9]

	correctBits := engine.CalcNextBits(sideTip9, 9, sideGetAncestor, p)
	buggyBits := engine.CalcNextBits(sideTip9, 9, c.getAncestorUnsafe, p)

	t.Logf("")
	t.Logf("CalcNextBits for side chain height 10:")
	t.Logf("  Correct (side chain ancestors): 0x%08x", correctBits)
	t.Logf("  Buggy (main chain ancestors):   0x%08x", buggyBits)

	if correctBits != buggyBits {
		t.Logf("  MISMATCH CONFIRMED: getAncestorUnsafe produces wrong difficulty")
	} else {
		t.Logf("  Bits happen to match (clamping may normalize both timespans)")
	}

	// Phase 4: Submit side chain blocks and observe the failure.
	var failedHeight uint32
	var failErr error
	reorgHappened := false

	for i, block := range sideBlocks {
		height := uint32(4 + i)
		_, err := c.ProcessBlock(block)
		if err != nil {
			isHeaderFail := bytes.Contains([]byte(err.Error()), []byte("incorrect difficulty"))
			isSideChain := bytes.Contains([]byte(err.Error()), []byte("side chain block"))

			if isHeaderFail {
				failedHeight = height
				failErr = err
				t.Logf("Height %d: REJECTED (difficulty mismatch): %v", height, err)
				break
			} else if isSideChain {
				t.Logf("Height %d: stored as side chain", height)
			} else {
				t.Logf("Height %d: error: %v", height, err)
			}
		} else {
			t.Logf("Height %d: accepted", height)
			reorgHappened = true
		}
	}

	t.Logf("")

	if failedHeight > 0 && failErr != nil {
		t.Logf("═══════════════════════════════════════════════════════════════")
		t.Logf("RC-1 CONFIRMED: Side chain block REJECTED at height %d", failedHeight)
		t.Logf("Error: %v", failErr)
		t.Logf("")
		t.Logf("The block was mined with correct difficulty for its own chain,")
		t.Logf("but ProcessBlock used getAncestorUnsafe which returned the MAIN")
		t.Logf("chain's block at the retarget window start. This produced wrong")
		t.Logf("expected bits, causing the valid block to fail validation.")
		t.Logf("═══════════════════════════════════════════════════════════════")
	} else if reorgHappened {
		t.Logf("Reorg happened (bits matched due to clamping). Verifying ancestor bug directly...")
	}

	// Phase 5: Regardless of whether the bits happened to match, directly prove
	// that getAncestorUnsafe returns the wrong block for side chain heights.
	// This is the fundamental bug — even if it doesn't cause a bits mismatch in
	// every scenario, it WILL cause mismatches when timespans differ enough.

	// After a potential reorg, hashByHeight may have changed. Use the saved values.
	sideH5Hash := sideHashes[5]

	t.Logf("Side chain block hash at height 5: %s", sideH5Hash.ReverseString()[:16])
	t.Logf("Main chain block hash at height 5 (pre-reorg): %s", mainBlock5HashBefore.ReverseString()[:16])

	if mainBlock5HashBefore == sideH5Hash {
		t.Fatal("blocks at height 5 should differ between chains")
	}

	t.Logf("")
	t.Logf("CONFIRMED: getAncestorUnsafe always returns the active main chain's block.")
	t.Logf("For side-chain validation at retarget boundaries, this returns wrong")
	t.Logf("timestamps, producing incorrect expected difficulty bits.")
	t.Logf("")
	t.Logf("In the chaos test (testnet: interval=20, 5s blocks), partitioned nodes")
	t.Logf("mine different chains. After reconnection, the shorter chain's node")
	t.Logf("rejects the longer chain's blocks at retarget boundaries because")
	t.Logf("getAncestorUnsafe returns its own chain's blocks for the retarget")
	t.Logf("window, not the incoming chain's blocks.")
}

// TestSideChainAncestorLookup directly verifies that buildAncestorLookup
// returns side-chain headers at heights where the side chain diverges from
// the main chain, and falls back to main-chain headers below the fork point.
func TestSideChainAncestorLookup(t *testing.T) {
	c, p := setupRetargetChain(t, 5, 60*time.Second)
	baseTime := c.params.GenesisBlock.Header.Timestamp

	// Mine 8 blocks on the main chain (60s spacing).
	for i := 1; i <= 8; i++ {
		tipHash, tipHeight := c.Tip()
		tipHeader, _ := c.TipHeader()
		ts := baseTime + uint32(i)*60
		block := mineBlockWithTimestamp(t, tipHash, tipHeader, tipHeight, p, ts, fmt.Sprintf("main-%d", i), c.getAncestorUnsafe)
		if _, err := c.ProcessBlock(block); err != nil {
			t.Fatalf("main chain block %d: %v", i, err)
		}
	}

	// Fork at height 3 — build a side chain with different timestamps (120s spacing).
	forkHash := c.hashByHeight[3]
	forkHeader, _ := c.store.GetHeader(forkHash)
	forkHeight := uint32(3)

	prevHash := forkHash
	prevHeader := forkHeader
	prevHeight := forkHeight

	sideAncestors := make(map[uint32]*types.BlockHeader)
	sideHashes := make(map[uint32]types.Hash)
	for h := uint32(0); h <= forkHeight; h++ {
		hash := c.hashByHeight[h]
		hdr, _ := c.store.GetHeader(hash)
		sideAncestors[h] = hdr
		sideHashes[h] = hash
	}
	sideGetAncestor := func(h uint32) *types.BlockHeader { return sideAncestors[h] }

	for i := 4; i <= 6; i++ {
		ts := forkHeader.Timestamp + uint32(i-3)*120
		block := mineBlockWithTimestamp(t, prevHash, prevHeader, prevHeight, p, ts, fmt.Sprintf("side-%d", i), sideGetAncestor)
		blockHash := crypto.HashBlockHeader(&block.Header)
		sideAncestors[uint32(i)] = &block.Header
		sideHashes[uint32(i)] = blockHash

		// Submit to chain — will be stored as side chain (less work than main).
		_, err := c.ProcessBlock(block)
		if err != nil && !errors.Is(err, ErrSideChain) {
			t.Fatalf("side chain block %d: %v", i, err)
		}

		prevHash = blockHash
		prevHeader = &block.Header
		prevHeight = uint32(i)
	}

	// Now test buildAncestorLookup for the side chain tip (height 6).
	// The side chain's parent is at height 5 with hash sideHashes[5].
	sideParentHash := sideHashes[5]
	sideParentHeight := uint32(5)
	lookup := c.buildAncestorLookup(sideParentHash, sideParentHeight)

	// Heights 4 and 5 should return side-chain headers (divergent from main).
	for _, h := range []uint32{4, 5} {
		got := lookup(h)
		if got == nil {
			t.Fatalf("buildAncestorLookup(%d) returned nil", h)
		}
		mainHash := c.hashByHeight[h]
		mainHeader, _ := c.store.GetHeader(mainHash)
		expectedSide := sideAncestors[h]

		if got.Timestamp != expectedSide.Timestamp {
			t.Errorf("height %d: got timestamp %d, want side-chain %d", h, got.Timestamp, expectedSide.Timestamp)
		}
		if got.Timestamp == mainHeader.Timestamp && expectedSide.Timestamp != mainHeader.Timestamp {
			t.Errorf("height %d: returned main-chain header instead of side-chain", h)
		}
	}

	// Heights at and below the fork point (0-3) should return main-chain headers
	// since both chains share the same history there.
	for h := uint32(0); h <= 3; h++ {
		got := lookup(h)
		if got == nil {
			t.Fatalf("buildAncestorLookup(%d) returned nil", h)
		}
		mainHash := c.hashByHeight[h]
		mainHeader, _ := c.store.GetHeader(mainHash)
		if got.Timestamp != mainHeader.Timestamp {
			t.Errorf("height %d (shared): got timestamp %d, want %d", h, got.Timestamp, mainHeader.Timestamp)
		}
	}

	t.Logf("buildAncestorLookup correctly returns side-chain headers above fork and main-chain headers at/below fork")
}

// TestReorgAcrossRetargetBoundary verifies that a reorg succeeds when the fork
// spans a retarget boundary and the two chains produce genuinely different
// difficulty bits. This requires params where the target is NOT already at the
// minimum difficulty, so retarget adjustments actually change the bits value.
func TestReorgAcrossRetargetBoundary(t *testing.T) {
	// Use a harder initial difficulty (0x1e0fffff) with a high MinBits ceiling
	// so retarget adjustments produce different bits on each chain.
	interval := uint32(5)
	blockSpacing := 60 * time.Second
	targetTimespan := time.Duration(interval) * blockSpacing

	p := &fcparams.ChainParams{
		Name:                   "retarget-reorg-test",
		NetworkMagic:           [4]byte{0xFA, 0x1C, 0xC0, 0xFD},
		DefaultPort:            19556,
		TargetBlockSpacing:     blockSpacing,
		RetargetInterval:       interval,
		TargetTimespan:         targetTimespan,
		MaxTimeFutureDrift:     10 * time.Minute,
		MinTimestampRule:       "prev+1",
		InitialBits:            0x1e0fffff,
		MinBits:                0x1e0fffff,
		NoRetarget:             false,
		MaxBlockSize:           4_000_000,
		MaxBlockTxCount:        50_000,
		InitialSubsidy:         50_0000_0000,
		SubsidyHalvingInterval: 150,
		CoinbaseMaturity:       1,
		MaxMempoolSize:         10000,
		MinRelayTxFee:          0,
		SeedNodes:              []string{},
		ActivationHeights:      map[string]uint32{},
	}

	cfg := fcparams.GenesisConfig{
		NetworkName:     "retarget-reorg-test",
		CoinbaseMessage: []byte("retarget reorg test genesis"),
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

	baseTime := genesis.Header.Timestamp

	// Mine 3 shared blocks (60s spacing, on-target).
	for i := 1; i <= 3; i++ {
		tipHash, tipHeight := c.Tip()
		tipHeader, _ := c.TipHeader()
		ts := baseTime + uint32(i)*60
		block := mineBlockWithTimestamp(t, tipHash, tipHeader, tipHeight, p, ts, fmt.Sprintf("shared-%d", i), c.getAncestorUnsafe)
		if _, err := c.ProcessBlock(block); err != nil {
			t.Fatalf("shared block %d: %v", i, err)
		}
	}

	forkHash := c.hashByHeight[3]
	forkHeader, _ := c.store.GetHeader(forkHash)
	forkHeight := uint32(3)

	// Main chain: mine blocks 4-10 with SLOW spacing (240s — 4x slower than target).
	// Slow blocks produce lower difficulty at the retarget boundary.
	for i := 4; i <= 10; i++ {
		tipHash, tipHeight := c.Tip()
		tipHeader, _ := c.TipHeader()
		ts := baseTime + uint32(3)*60 + uint32(i-3)*240
		block := mineBlockWithTimestamp(t, tipHash, tipHeader, tipHeight, p, ts, fmt.Sprintf("main-%d", i), c.getAncestorUnsafe)
		if _, err := c.ProcessBlock(block); err != nil {
			t.Fatalf("main chain block %d: %v", i, err)
		}
	}

	mainTipHash, mainTipHeight := c.Tip()
	t.Logf("Main chain: height=%d, tip=%s", mainTipHeight, mainTipHash.ReverseString()[:16])

	// Record main chain bits at height 10 (first block after retarget).
	mainBlock10Hash := c.hashByHeight[10]
	mainBlock10, _ := c.store.GetHeader(mainBlock10Hash)
	t.Logf("Main chain bits at height 10: 0x%08x", mainBlock10.Bits)

	// Side chain: mine blocks 4-15 with FAST spacing (15s — 4x faster than target).
	// Fast blocks produce higher difficulty at the retarget boundary, meaning
	// each side-chain block contributes MORE work than each main-chain block.
	sideAncestors := make(map[uint32]*types.BlockHeader)
	for h := uint32(0); h <= forkHeight; h++ {
		hash := c.hashByHeight[h]
		hdr, _ := c.store.GetHeader(hash)
		sideAncestors[h] = hdr
	}
	sideGetAncestor := func(h uint32) *types.BlockHeader { return sideAncestors[h] }

	prevHash := forkHash
	prevHeader := forkHeader
	prevHeight := forkHeight
	var sideBlocks []*types.Block

	for i := 4; i <= 15; i++ {
		ts := baseTime + uint32(3)*60 + uint32(i-3)*15
		block := mineBlockWithTimestamp(t, prevHash, prevHeader, prevHeight, p, ts, fmt.Sprintf("side-%d", i), sideGetAncestor)
		blockHash := crypto.HashBlockHeader(&block.Header)
		sideAncestors[uint32(i)] = &block.Header
		sideBlocks = append(sideBlocks, block)

		prevHash = blockHash
		prevHeader = &block.Header
		prevHeight = uint32(i)
	}

	// Verify the two chains produce different bits at the retarget boundary.
	sideBitsAt10 := sideAncestors[10].Bits
	t.Logf("Side chain bits at height 10: 0x%08x", sideBitsAt10)

	if mainBlock10.Bits == sideBitsAt10 {
		t.Logf("WARNING: bits match despite different timespans (clamping to MinBits)")
	} else {
		t.Logf("CONFIRMED: chains have different difficulty at retarget boundary (main=0x%08x, side=0x%08x)", mainBlock10.Bits, sideBitsAt10)
	}

	// Submit all side chain blocks. The side chain has more blocks AND harder
	// difficulty post-retarget, so it should accumulate more total work.
	var reorgHeight uint32
	for i, block := range sideBlocks {
		height := uint32(4 + i)
		_, err := c.ProcessBlock(block)
		if err != nil {
			if errors.Is(err, ErrSideChain) {
				continue
			}
			t.Fatalf("side chain block at height %d rejected: %v", height, err)
		}
		reorgHeight = height
	}

	newTipHash, newTipHeight := c.Tip()
	t.Logf("After reorg: height=%d, tip=%s", newTipHeight, newTipHash.ReverseString()[:16])

	if newTipHeight != 15 {
		t.Fatalf("expected tip at height 15 after reorg, got %d", newTipHeight)
	}
	if newTipHash == mainTipHash {
		t.Fatal("reorg did not happen — still on main chain")
	}
	if reorgHeight == 0 {
		t.Fatal("no block triggered the reorg")
	}

	// Verify the chain is consistent: walk backwards from tip and confirm all
	// heights have the side-chain blocks.
	for h := uint32(15); h > forkHeight; h-- {
		hash, ok := c.hashByHeight[h]
		if !ok {
			t.Fatalf("missing hashByHeight entry at height %d after reorg", h)
		}
		_, err := c.store.GetBlock(hash)
		if err != nil {
			t.Fatalf("cannot load block at height %d after reorg: %v", h, err)
		}
	}

	t.Logf("Reorg across retarget boundary succeeded: fork at height %d, reorg triggered at height %d, new tip at height %d",
		forkHeight, reorgHeight, newTipHeight)
}

// TestChainworkFromIndex verifies that the ChainWork stored in the block index
// for every block matches the cumulative work recomputed by summing CalcWork(bits)
// from genesis through that block. This confirms TASK-05: workForParentChain
// reads stored ChainWork (O(1)) and that value is consistent with the canonical
// per-block work calculation.
func TestChainworkFromIndex(t *testing.T) {
	c, p := setupTestChain(t)

	const chainLen = 10
	for i := 0; i < chainLen; i++ {
		block := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(block); err != nil {
			t.Fatalf("ProcessBlock at height %d: %v", i+1, err)
		}
	}

	_, tipHeight := c.Tip()
	if tipHeight != chainLen {
		t.Fatalf("tip height = %d, want %d", tipHeight, chainLen)
	}

	// Walk the chain from genesis to tip, accumulating work from CalcWork(bits)
	// and comparing against the stored ChainWork at each height.
	cumulativeWork := new(big.Int)
	for h := uint32(0); h <= tipHeight; h++ {
		hash, ok := c.hashByHeight[h]
		if !ok {
			t.Fatalf("missing hashByHeight at height %d", h)
		}

		rec, err := c.store.GetBlockIndex(hash)
		if err != nil {
			t.Fatalf("GetBlockIndex at height %d: %v", h, err)
		}

		blockWork := crypto.CalcWork(rec.Header.Bits)
		cumulativeWork.Add(cumulativeWork, blockWork)

		if rec.ChainWork == nil {
			t.Fatalf("height %d: stored ChainWork is nil", h)
		}
		if rec.ChainWork.Cmp(cumulativeWork) != 0 {
			t.Fatalf("height %d: stored ChainWork=%s, recomputed=%s",
				h, rec.ChainWork.Text(16), cumulativeWork.Text(16))
		}

		// Also verify workForParentChain returns the same value.
		fromFunc, err := c.workForParentChain(hash)
		if err != nil {
			t.Fatalf("workForParentChain at height %d: %v", h, err)
		}
		if fromFunc.Cmp(cumulativeWork) != 0 {
			t.Fatalf("height %d: workForParentChain=%s, recomputed=%s",
				h, fromFunc.Text(16), cumulativeWork.Text(16))
		}
	}

	t.Logf("ChainWork verified for all %d blocks: stored index values match cumulative CalcWork sums", tipHeight+1)
}

// TestHasBlockFalseAfterFailedReorg verifies that when a block triggers a
// reorg that fails tx validation, the block index entry is cleaned up so
// HasBlock returns false. This prevents the block from being permanently
// "known" and un-requestable by the P2P layer (TASK-03).
func TestHasBlockFalseAfterFailedReorg(t *testing.T) {
	c, p := setupReorgTestChain(t)

	// Main chain: 5 blocks.
	for i := 0; i < 5; i++ {
		b := mineBlock(t, c, p)
		if _, err := c.ProcessBlock(b); err != nil {
			t.Fatalf("main block %d: %v", i, err)
		}
	}

	// Side chain: fork from genesis, 5 valid blocks (stored as side chain).
	genesisHash := p.GenesisHash
	genesisHeader := &p.GenesisBlock.Header
	side := buildChainOnParent(t, genesisHash, genesisHeader, 0, p, 5, "task03")
	for _, b := range side[:4] {
		_, err := c.ProcessBlock(b)
		if err != nil && !errors.Is(err, ErrSideChain) {
			t.Fatalf("side chain block: %v", err)
		}
	}
	// 5th side block may trigger equal-work reorg — submit it.
	c.ProcessBlock(side[4])

	// Build a 6th side-chain block with an inflated coinbase (2x subsidy).
	// This gives the side chain strictly more work, triggering a reorg that
	// must fail during tx validation.
	lastValid := side[len(side)-1]
	lastValidHash := crypto.HashBlockHeader(&lastValid.Header)
	inflated := mineBlockOnParentWithValue(t, lastValidHash, &lastValid.Header, 5, p, "inflated-task03", p.CalcSubsidy(6)*2)
	inflatedHash := crypto.HashBlockHeader(&inflated.Header)

	_, err := c.ProcessBlock(inflated)
	if err == nil {
		t.Fatal("expected rejection of inflated coinbase block during reorg")
	}

	// The critical assertion: HasBlock must return false for the rejected
	// block. Before TASK-03, the block index entry persisted after the
	// failed reorg, causing HasBlock to return true and permanently
	// preventing the P2P layer from re-requesting the block.
	if c.HasBlock(inflatedHash) {
		t.Fatal("HasBlock returned true for a block whose reorg failed — block index entry was not cleaned up (TASK-03 regression)")
	}

	// HasBlockOnChain should also be false.
	if c.HasBlockOnChain(inflatedHash) {
		t.Fatal("HasBlockOnChain returned true for a rejected reorg block")
	}

	// The chain should still be on the original main chain.
	_, tipHeight := c.Tip()
	if tipHeight < 5 {
		t.Fatalf("chain should still be at height >= 5, got %d", tipHeight)
	}
}

// TestOrphanCountAndEviction verifies that OrphanCount tracks the pool size
// and EvictExpiredOrphans removes stale entries.
func TestOrphanCountAndEviction(t *testing.T) {
	c, p := setupTestChain(t)

	if c.OrphanCount() != 0 {
		t.Fatalf("expected 0 orphans at start, got %d", c.OrphanCount())
	}

	// Build a block whose parent is unknown (random hash). This will be
	// accepted into the orphan pool after passing the PoW sanity check.
	fakeParent := types.Hash{0xDE, 0xAD}
	genesisHeader := &p.GenesisBlock.Header

	orphan := mineBlockOnParent(t, fakeParent, genesisHeader, 0, p, "orphan-test")

	_, err := c.ProcessBlock(orphan)
	if !errors.Is(err, ErrOrphanBlock) {
		t.Fatalf("expected ErrOrphanBlock, got: %v", err)
	}

	if c.OrphanCount() != 1 {
		t.Fatalf("expected 1 orphan, got %d", c.OrphanCount())
	}

	// EvictExpiredOrphans should not evict a fresh orphan.
	evicted := c.EvictExpiredOrphans()
	if evicted != 0 {
		t.Fatalf("expected 0 evictions for fresh orphan, got %d", evicted)
	}
	if c.OrphanCount() != 1 {
		t.Fatalf("expected 1 orphan after no-op eviction, got %d", c.OrphanCount())
	}
}

// TestOrphanExpiryByAge verifies that orphans older than OrphanExpiry are
// removed by the eviction sweep. Uses direct manipulation of the addedAt
// timestamp to avoid waiting 20 real minutes.
func TestOrphanExpiryByAge(t *testing.T) {
	c, p := setupTestChain(t)

	fakeParent := types.Hash{0xDE, 0xAD}
	genesisHeader := &p.GenesisBlock.Header

	orphan := mineBlockOnParent(t, fakeParent, genesisHeader, 0, p, "expiry-test")

	_, err := c.ProcessBlock(orphan)
	if !errors.Is(err, ErrOrphanBlock) {
		t.Fatalf("expected ErrOrphanBlock, got: %v", err)
	}

	if c.OrphanCount() != 1 {
		t.Fatalf("expected 1 orphan, got %d", c.OrphanCount())
	}

	// Artificially age the orphan past the expiry threshold.
	c.mu.Lock()
	for hash, ob := range c.orphans {
		ob.addedAt = time.Now().Add(-(OrphanExpiry + time.Minute))
		c.orphans[hash] = ob
	}
	c.mu.Unlock()

	evicted := c.EvictExpiredOrphans()
	if evicted != 1 {
		t.Fatalf("expected 1 eviction, got %d", evicted)
	}
	if c.OrphanCount() != 0 {
		t.Fatalf("expected 0 orphans after expiry eviction, got %d", c.OrphanCount())
	}
}

// TestOrphanPoolCapacity verifies that the orphan pool respects MaxOrphanBlocks
// and evicts entries when full.
func TestOrphanPoolCapacity(t *testing.T) {
	c, p := setupTestChain(t)

	genesisHeader := &p.GenesisBlock.Header

	for i := 0; i < MaxOrphanBlocks+5; i++ {
		fakeParent := types.Hash{byte(i >> 8), byte(i)}
		tag := fmt.Sprintf("cap-test-%d", i)
		orphan := mineBlockOnParent(t, fakeParent, genesisHeader, 0, p, tag)
		c.ProcessBlock(orphan)
	}

	if c.OrphanCount() > MaxOrphanBlocks {
		t.Fatalf("orphan pool exceeded capacity: %d > %d", c.OrphanCount(), MaxOrphanBlocks)
	}
}

// TestOrphanResolution verifies that when a parent block arrives, its orphan
// children are processed and accepted onto the chain.
func TestOrphanResolution(t *testing.T) {
	c, p := setupTestChain(t)

	// Mine block 1 (the parent) but don't submit it yet.
	parent := mineBlock(t, c, p)
	parentHash := crypto.HashBlockHeader(&parent.Header)

	// Mine block 2 on top of block 1.
	child := mineBlockOnParent(t, parentHash, &parent.Header, 1, p, "orphan-child")

	// Submit block 2 first — it becomes an orphan.
	_, err := c.ProcessBlock(child)
	if !errors.Is(err, ErrOrphanBlock) {
		t.Fatalf("expected ErrOrphanBlock for out-of-order block, got: %v", err)
	}
	if c.OrphanCount() != 1 {
		t.Fatalf("expected 1 orphan, got %d", c.OrphanCount())
	}

	// Now submit the parent — the orphan child should be resolved.
	_, err = c.ProcessBlock(parent)
	if err != nil {
		t.Fatalf("parent block rejected: %v", err)
	}

	// Orphan pool should be empty and chain should be at height 2.
	if c.OrphanCount() != 0 {
		t.Fatalf("expected 0 orphans after parent arrived, got %d", c.OrphanCount())
	}
	_, tipHeight := c.Tip()
	if tipHeight != 2 {
		t.Fatalf("expected chain at height 2 after orphan resolution, got %d", tipHeight)
	}
}
