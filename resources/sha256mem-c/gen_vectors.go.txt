// gen_vectors generates 1000 sha256mem test vectors as hex pairs (input, expected_output).
// Output format: one line per vector, "hex_input hex_output\n"
package main

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/bams-repo/fairchain/internal/algorithms/sha256mem"
)

func main() {
	h := sha256mem.New()

	f, err := os.Create("test_vectors.txt")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	for i := 0; i < 1000; i++ {
		// Build deterministic inputs of varying lengths.
		// i=0 → empty, i=1 → [0], i=2 → [0,1], ..., i=255 → [0..254], i=256 → [1,0], etc.
		input := make([]byte, i)
		for j := 0; j < i; j++ {
			input[j] = byte((i + j) & 0xFF)
		}

		result := h.PoWHash(input)

		fmt.Fprintf(f, "%s %s\n", hex.EncodeToString(input), hex.EncodeToString(result[:]))
	}

	fmt.Println("wrote 1000 test vectors to test_vectors.txt")
}
