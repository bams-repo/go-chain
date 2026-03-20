#include "sha256mem.h"
#include <stdio.h>
#include <string.h>
#include <time.h>

int main(void) {
    const uint8_t input[] = "benchmark input for sha256mem";
    const size_t input_len = sizeof(input) - 1;
    uint8_t out[32];

    const int iterations = 1000;

    struct timespec start, end;
    clock_gettime(CLOCK_MONOTONIC, &start);

    for (int i = 0; i < iterations; i++)
        sha256mem_hash(input, input_len, out);

    clock_gettime(CLOCK_MONOTONIC, &end);

    double elapsed = (end.tv_sec - start.tv_sec) + (end.tv_nsec - start.tv_nsec) / 1e9;
    double per_hash_ms = (elapsed / iterations) * 1000.0;

    printf("Iterations: %d\n", iterations);
    printf("Total time: %.3f s\n", elapsed);
    printf("Per hash:   %.3f ms\n", per_hash_ms);
    printf("Hashes/sec: %.1f\n", iterations / elapsed);

    return 0;
}
