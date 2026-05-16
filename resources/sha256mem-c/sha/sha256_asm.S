/*
 * sha256_asm.S — x86-64 SHA-256 using Intel SHA-NI + SSE4.1 + SSSE3
 * AT&T syntax (native GAS)
 *
 * Exports:
 *   void sha256_asm_block(uint32_t state[8], const uint8_t block[64])
 *   void sha256_asm(const uint8_t *data, size_t len, uint8_t out[32])
 *
 * System V AMD64 ABI:
 *   args: %rdi, %rsi, %rdx, %rcx, %r8, %r9
 */

    .section .rodata
    .balign 16
K256:
    .long 0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5
    .long 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5
    .long 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3
    .long 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174
    .long 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc
    .long 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da
    .long 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7
    .long 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967
    .long 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13
    .long 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85
    .long 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3
    .long 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070
    .long 0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5
    .long 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3
    .long 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208
    .long 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2

    .balign 16
BSWAP_MASK:
    .byte 3,2,1,0, 7,6,5,4, 11,10,9,8, 15,14,13,12

    .balign 16
SHA256_INIT:
    .long 0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a
    .long 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19

/* ----------------------------------------------------------------
 * sha256_asm_block(state %rdi, block %rsi)
 *
 * Registers:
 *   %xmm0 = STATE0 (ABEF)    %xmm1 = STATE1 (CDGH)
 *   %xmm2 = MSG (temp for rounds)
 *   %xmm3..%xmm6 = W0..W3 (message schedule)
 *   %xmm7 = byte-swap mask
 *   %xmm8 = temp
 *   %xmm9 = ABEF_SAVE   %xmm10 = CDGH_SAVE
 *   %xmm11 = temp for palignr
 * ---------------------------------------------------------------- */
    .text
    .globl sha256_asm_block
    .type  sha256_asm_block, @function
    .balign 16
sha256_asm_block:
    movdqa      BSWAP_MASK(%rip), %xmm7

    /* Load state. */
    movdqu      (%rdi),     %xmm0          /* A B C D */
    movdqu      16(%rdi),   %xmm1          /* E F G H */

    /* Rearrange for SHA-NI: STATE0=[A,B,E,F], STATE1=[C,D,G,H]. */
    pshufd      $0xB1, %xmm0, %xmm0        /* B A D C */
    pshufd      $0x1B, %xmm1, %xmm1        /* H G F E */
    movdqa      %xmm0, %xmm8
    palignr     $8, %xmm1, %xmm0           /* A B E F */
    pblendw     $0xF0, %xmm8, %xmm1        /* C D G H */

    /* Save initial state. */
    movdqa      %xmm0, %xmm9
    movdqa      %xmm1, %xmm10

    leaq        K256(%rip), %rax

    /* Load and byte-swap message words. */
    movdqu      (%rsi),     %xmm3
    pshufb      %xmm7, %xmm3
    movdqu      16(%rsi),   %xmm4
    pshufb      %xmm7, %xmm4
    movdqu      32(%rsi),   %xmm5
    pshufb      %xmm7, %xmm5
    movdqu      48(%rsi),   %xmm6
    pshufb      %xmm7, %xmm6

    /* Rounds 0-3 */
    movdqa      %xmm3, %xmm2
    paddd       (%rax), %xmm2
    sha256rnds2 %xmm0, %xmm1
    pshufd      $0x0E, %xmm2, %xmm0
    sha256rnds2 %xmm1, %xmm0
    movdqa      %xmm9, %xmm0               /* restore STATE0 for next use */

    /* Wait — sha256rnds2 implicitly uses xmm0 as the message.
     * AT&T syntax: sha256rnds2 %xmm0, src, dst
     * Actually: sha256rnds2 src, dst (implicit xmm0)
     * Let me redo this properly. */

    /* Start over with correct register usage.
     * sha256rnds2 src, dst  — uses xmm0 implicitly as the message chunk.
     * So we must put the K+W value in xmm0, and use other regs for state. */
    jmp sha256_asm_block_v2

    .size sha256_asm_block, . - sha256_asm_block

/*
 * Correct implementation: sha256rnds2 uses %xmm0 implicitly.
 * So state must live in other registers.
 *
 * Register plan:
 *   %xmm0 = MSG (implicit operand for sha256rnds2) — loaded with K+W
 *   %xmm1 = STATE0 (ABEF)
 *   %xmm2 = STATE1 (CDGH)
 *   %xmm3..%xmm6 = W0..W3
 *   %xmm7 = byte-swap mask
 *   %xmm8 = temp
 *   %xmm9 = ABEF_SAVE
 *   %xmm10 = CDGH_SAVE
 *   %xmm11 = temp
 */
    .globl sha256_asm_block_v2
    .type  sha256_asm_block_v2, @function
    .balign 16
sha256_asm_block_v2:
    movdqa      BSWAP_MASK(%rip), %xmm7

    movdqu      (%rdi),     %xmm1          /* A B C D */
    movdqu      16(%rdi),   %xmm2          /* E F G H */

    pshufd      $0xB1, %xmm1, %xmm1
    pshufd      $0x1B, %xmm2, %xmm2
    movdqa      %xmm1, %xmm8
    palignr     $8, %xmm2, %xmm1           /* STATE0 = ABEF */
    pblendw     $0xF0, %xmm8, %xmm2        /* STATE1 = CDGH */

    movdqa      %xmm1, %xmm9               /* ABEF_SAVE */
    movdqa      %xmm2, %xmm10              /* CDGH_SAVE */

    leaq        K256(%rip), %rax

    movdqu      (%rsi),     %xmm3
    pshufb      %xmm7, %xmm3
    movdqu      16(%rsi),   %xmm4
    pshufb      %xmm7, %xmm4
    movdqu      32(%rsi),   %xmm5
    pshufb      %xmm7, %xmm5
    movdqu      48(%rsi),   %xmm6
    pshufb      %xmm7, %xmm6

    /* Rounds 0-3 */
    movdqa      %xmm3, %xmm0
    paddd       (%rax), %xmm0
    sha256rnds2 %xmm1, %xmm2               /* STATE1 = rnds2(STATE0, STATE1, xmm0) */
    pshufd      $0x0E, %xmm0, %xmm0
    sha256rnds2 %xmm2, %xmm1               /* STATE0 = rnds2(STATE1, STATE0, xmm0) */

    /* Rounds 4-7 */
    movdqa      %xmm4, %xmm0
    paddd       16(%rax), %xmm0
    sha256rnds2 %xmm1, %xmm2
    pshufd      $0x0E, %xmm0, %xmm0
    sha256rnds2 %xmm2, %xmm1
    sha256msg1  %xmm4, %xmm3

    /* Rounds 8-11 */
    movdqa      %xmm5, %xmm0
    paddd       32(%rax), %xmm0
    sha256rnds2 %xmm1, %xmm2
    pshufd      $0x0E, %xmm0, %xmm0
    sha256rnds2 %xmm2, %xmm1
    sha256msg1  %xmm5, %xmm4

    /* Rounds 12-15 */
    movdqa      %xmm6, %xmm0
    paddd       48(%rax), %xmm0
    sha256rnds2 %xmm1, %xmm2
    movdqa      %xmm6, %xmm11
    palignr     $4, %xmm5, %xmm11
    paddd       %xmm11, %xmm3
    sha256msg2  %xmm6, %xmm3
    pshufd      $0x0E, %xmm0, %xmm0
    sha256rnds2 %xmm2, %xmm1
    sha256msg1  %xmm6, %xmm5

/*
 * Macro for rounds 16-59: 4 rounds + message schedule.
 * Args: W0, W1, W2, W3, K_offset
 * After: W1 is updated with new schedule word.
 */
.macro FOUR_ROUNDS_SCHED w0, w1, w2, w3, koff
    movdqa      \w0, %xmm0
    paddd       \koff(%rax), %xmm0
    sha256rnds2 %xmm1, %xmm2
    movdqa      \w0, %xmm11
    palignr     $4, \w3, %xmm11
    paddd       %xmm11, \w1
    sha256msg2  \w0, \w1
    pshufd      $0x0E, %xmm0, %xmm0
    sha256rnds2 %xmm2, %xmm1
    sha256msg1  \w0, \w3
.endm

    FOUR_ROUNDS_SCHED %xmm3, %xmm4, %xmm5, %xmm6, 64
    FOUR_ROUNDS_SCHED %xmm4, %xmm5, %xmm6, %xmm3, 80
    FOUR_ROUNDS_SCHED %xmm5, %xmm6, %xmm3, %xmm4, 96
    FOUR_ROUNDS_SCHED %xmm6, %xmm3, %xmm4, %xmm5, 112
    FOUR_ROUNDS_SCHED %xmm3, %xmm4, %xmm5, %xmm6, 128
    FOUR_ROUNDS_SCHED %xmm4, %xmm5, %xmm6, %xmm3, 144
    FOUR_ROUNDS_SCHED %xmm5, %xmm6, %xmm3, %xmm4, 160
    FOUR_ROUNDS_SCHED %xmm6, %xmm3, %xmm4, %xmm5, 176
    FOUR_ROUNDS_SCHED %xmm3, %xmm4, %xmm5, %xmm6, 192
    FOUR_ROUNDS_SCHED %xmm4, %xmm5, %xmm6, %xmm3, 208

    /* Rounds 56-59 (schedule w1 but no msg1 needed) */
    movdqa      %xmm5, %xmm0
    paddd       224(%rax), %xmm0
    sha256rnds2 %xmm1, %xmm2
    movdqa      %xmm5, %xmm11
    palignr     $4, %xmm4, %xmm11
    paddd       %xmm11, %xmm6
    sha256msg2  %xmm5, %xmm6
    pshufd      $0x0E, %xmm0, %xmm0
    sha256rnds2 %xmm2, %xmm1

    /* Rounds 60-63 (no scheduling) */
    movdqa      %xmm6, %xmm0
    paddd       240(%rax), %xmm0
    sha256rnds2 %xmm1, %xmm2
    pshufd      $0x0E, %xmm0, %xmm0
    sha256rnds2 %xmm2, %xmm1

    /* Add saved state. */
    paddd       %xmm9,  %xmm1
    paddd       %xmm10, %xmm2

    /* Convert back: STATE0=[A,B,E,F] STATE1=[C,D,G,H] → [A,B,C,D] [E,F,G,H]. */
    pshufd      $0x1B, %xmm1, %xmm8        /* F E B A */
    pshufd      $0xB1, %xmm2, %xmm2        /* D C H G */
    movdqa      %xmm8, %xmm1
    pblendw     $0xF0, %xmm2, %xmm1        /* D C B A */
    palignr     $8, %xmm8, %xmm2           /* H G F E */

    movdqu      %xmm1, (%rdi)
    movdqu      %xmm2, 16(%rdi)
    ret
    .size sha256_asm_block_v2, . - sha256_asm_block_v2


/* ----------------------------------------------------------------
 * sha256_asm(data %rdi, len %rsi, out %rdx)
 * Full SHA-256 with PKCS padding.
 * ---------------------------------------------------------------- */
    .globl sha256_asm
    .type  sha256_asm, @function
    .balign 16
sha256_asm:
    pushq       %rbp
    movq        %rsp, %rbp
    pushq       %rbx
    pushq       %r12
    pushq       %r13
    pushq       %r14

    /* Allocate: 32 (state) + 128 (pad) = 160, round to 176 for alignment. */
    subq        $176, %rsp

    movq        %rdi, %r12                  /* data */
    movq        %rsi, %r13                  /* len */
    movq        %rdx, %r14                  /* out */

    /* state at %rsp+0 (32 bytes), pad buf at %rsp+32 (128 bytes). */
    leaq        SHA256_INIT(%rip), %rax
    movdqu      (%rax),   %xmm0
    movdqu      16(%rax), %xmm1
    movdqu      %xmm0, (%rsp)
    movdqu      %xmm1, 16(%rsp)

    /* Process full 64-byte blocks. */
    movq        %r13, %rcx
    shrq        $6, %rcx
    testq       %rcx, %rcx
    jz          .Lasm_pad

    movq        %r12, %rbx                  /* block ptr */
.Lasm_full_loop:
    movq        %rsp, %rdi                  /* state */
    movq        %rbx, %rsi                  /* block */
    call        sha256_asm_block_v2
    addq        $64, %rbx
    decq        %rcx
    jnz         .Lasm_full_loop

.Lasm_pad:
    /* Zero pad buffer (128 bytes at rsp+32). */
    pxor        %xmm0, %xmm0
    movdqu      %xmm0, 32(%rsp)
    movdqu      %xmm0, 48(%rsp)
    movdqu      %xmm0, 64(%rsp)
    movdqu      %xmm0, 80(%rsp)
    movdqu      %xmm0, 96(%rsp)
    movdqu      %xmm0, 112(%rsp)
    movdqu      %xmm0, 128(%rsp)
    movdqu      %xmm0, 144(%rsp)

    /* Copy remaining bytes. */
    movq        %r13, %rax
    andq        $63, %rax                   /* remain */

    /* src = data + (len & ~63) */
    movq        %r13, %rsi
    andq        $-64, %rsi
    addq        %r12, %rsi

    /* dst = rsp+32 */
    leaq        32(%rsp), %rdi
    movq        %rax, %rcx
    rep movsb

    /* 0x80 byte. */
    leaq        32(%rsp), %rdi
    movb        $0x80, (%rdi,%rax,1)

    /* Bit length (big-endian 64-bit). */
    movq        %r13, %rcx
    shlq        $3, %rcx
    bswapq      %rcx

    cmpq        $56, %rax
    jae         .Lasm_two_blocks

    /* Single final block. */
    movq        %rcx, 88(%rsp)              /* rsp+32+56 = rsp+88 */
    movq        %rsp, %rdi
    leaq        32(%rsp), %rsi
    call        sha256_asm_block_v2
    jmp         .Lasm_finalize

.Lasm_two_blocks:
    movq        %rcx, 152(%rsp)             /* rsp+32+120 = rsp+152 */
    movq        %rsp, %rdi
    leaq        32(%rsp), %rsi
    call        sha256_asm_block_v2
    movq        %rsp, %rdi
    leaq        96(%rsp), %rsi              /* rsp+32+64 = rsp+96 */
    call        sha256_asm_block_v2

.Lasm_finalize:
    /* Write big-endian digest. */
    movl        (%rsp),    %eax
    bswapl      %eax
    movl        %eax, (%r14)
    movl        4(%rsp),   %eax
    bswapl      %eax
    movl        %eax, 4(%r14)
    movl        8(%rsp),   %eax
    bswapl      %eax
    movl        %eax, 8(%r14)
    movl        12(%rsp),  %eax
    bswapl      %eax
    movl        %eax, 12(%r14)
    movl        16(%rsp),  %eax
    bswapl      %eax
    movl        %eax, 16(%r14)
    movl        20(%rsp),  %eax
    bswapl      %eax
    movl        %eax, 20(%r14)
    movl        24(%rsp),  %eax
    bswapl      %eax
    movl        %eax, 24(%r14)
    movl        28(%rsp),  %eax
    bswapl      %eax
    movl        %eax, 28(%r14)

    addq        $176, %rsp
    popq        %r14
    popq        %r13
    popq        %r12
    popq        %rbx
    popq        %rbp
    ret
    .size sha256_asm, . - sha256_asm

    .section .note.GNU-stack,"",@progbits
