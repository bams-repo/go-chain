#include "sha256mem.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int hex_to_bytes(const char *hex, uint8_t *out, size_t max_out) {
    size_t hex_len = strlen(hex);
    if (hex_len % 2 != 0 || hex_len / 2 > max_out)
        return -1;

    for (size_t i = 0; i < hex_len / 2; i++) {
        unsigned int byte_val;
        if (sscanf(hex + 2 * i, "%2x", &byte_val) != 1)
            return -1;
        out[i] = (uint8_t)byte_val;
    }
    return (int)(hex_len / 2);
}

static void bytes_to_hex(const uint8_t *data, size_t len, char *out) {
    for (size_t i = 0; i < len; i++)
        sprintf(out + 2 * i, "%02x", data[i]);
    out[len * 2] = '\0';
}

int main(int argc, char **argv) {
    const char *path = "test_vectors.txt";
    if (argc > 1)
        path = argv[1];

    FILE *f = fopen(path, "r");
    if (!f) {
        fprintf(stderr, "cannot open %s\n", path);
        return 1;
    }

    char line[131072];
    int total = 0, passed = 0, failed = 0;

    while (fgets(line, sizeof(line), f)) {
        /* Strip trailing newline. */
        size_t ln = strlen(line);
        while (ln > 0 && (line[ln - 1] == '\n' || line[ln - 1] == '\r'))
            line[--ln] = '\0';

        if (ln == 0)
            continue;

        /* Split on space: "hex_input hex_expected" */
        char *space = strchr(line, ' ');
        if (!space) {
            fprintf(stderr, "malformed line %d\n", total + 1);
            failed++;
            total++;
            continue;
        }
        *space = '\0';
        const char *hex_input = line;
        const char *hex_expected = space + 1;

        /* Decode input. */
        uint8_t input[65536];
        int input_len = 0;
        if (strlen(hex_input) > 0) {
            input_len = hex_to_bytes(hex_input, input, sizeof(input));
            if (input_len < 0) {
                fprintf(stderr, "bad hex input on line %d\n", total + 1);
                failed++;
                total++;
                continue;
            }
        }

        /* Decode expected hash. */
        uint8_t expected[32];
        if (hex_to_bytes(hex_expected, expected, 32) != 32) {
            fprintf(stderr, "bad expected hash on line %d\n", total + 1);
            failed++;
            total++;
            continue;
        }

        /* Compute sha256mem hash. */
        uint8_t got[32];
        sha256mem_hash(input, (size_t)input_len, got);

        total++;
        if (memcmp(got, expected, 32) == 0) {
            passed++;
        } else {
            char got_hex[65], exp_hex[65];
            bytes_to_hex(got, 32, got_hex);
            bytes_to_hex(expected, 32, exp_hex);
            fprintf(stderr, "MISMATCH vector %d:\n  expected: %s\n  got:      %s\n",
                    total, exp_hex, got_hex);
            failed++;
        }
    }

    fclose(f);

    printf("\n=== sha256mem C vs Go parity test ===\n");
    printf("Total:  %d\n", total);
    printf("Passed: %d\n", passed);
    printf("Failed: %d\n", failed);
    printf("Result: %s\n", failed == 0 ? "ALL MATCH" : "MISMATCH DETECTED");

    return failed == 0 ? 0 : 1;
}
