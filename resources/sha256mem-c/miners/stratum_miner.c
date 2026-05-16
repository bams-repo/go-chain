/*
 * sha256mem Stratum GPU Miner — Fairchain pool mining
 * ====================================================
 * Connects to Stratum v1 and mines sha256mem v2 with the TMTO OpenCL kernel.
 *
 * Default pool is the in-wallet stratum server (Fairchain-Qt: Mining →
 * start stratum, default port 3333). Override with -o / -u / -p.
 *
 * Build:
 *   make stratum_miner
 *
 * Run (defaults = local wallet stratum):
 *   ./stratum_miner
 *
 * Run (public pool example):
 *   ./stratum_miner -o stratum+tcp://fair.suprnova.cc:3833 \
 *                   -u WALLET.worker -p x
 *
 * Copyright (c) 2024-2026 The Fairchain Contributors
 * Distributed under the MIT software license.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <time.h>
#include <math.h>
#include <signal.h>
#include <unistd.h>
#include <errno.h>
#include <sys/stat.h>
#include <sys/socket.h>
#include <sys/select.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <netdb.h>
#include <openssl/sha.h>
#include <jansson.h>
#include "sha256mem_v2.h"

#ifdef __APPLE__
#include <OpenCL/opencl.h>
#else
#include <CL/cl.h>
#endif

/* ── Globals ────────────────────────────────────────────────────── */

static volatile int g_running = 1;
static void sighandler(int sig) { (void)sig; g_running = 0; }

/* ── OpenCL helpers ─────────────────────────────────────────────── */

static const char *cl_err_str(cl_int err)
{
    switch (err) {
    case CL_SUCCESS:                        return "CL_SUCCESS";
    case CL_DEVICE_NOT_FOUND:               return "CL_DEVICE_NOT_FOUND";
    case CL_BUILD_PROGRAM_FAILURE:          return "CL_BUILD_PROGRAM_FAILURE";
    case CL_MEM_OBJECT_ALLOCATION_FAILURE:  return "CL_MEM_OBJECT_ALLOCATION_FAILURE";
    case CL_OUT_OF_RESOURCES:               return "CL_OUT_OF_RESOURCES";
    default: return "UNKNOWN";
    }
}

#define CL_CHECK(call, msg) do { \
    cl_int _err = (call); \
    if (_err != CL_SUCCESS) { \
        fprintf(stderr, "OpenCL error: %s (%d: %s)\n", msg, _err, cl_err_str(_err)); \
        exit(1); \
    } \
} while(0)

static char *load_kernel_source(const char *path, size_t *len)
{
    FILE *f = fopen(path, "r");
    if (!f) { fprintf(stderr, "Cannot open kernel: %s\n", path); exit(1); }
    fseek(f, 0, SEEK_END);
    *len = (size_t)ftell(f);
    fseek(f, 0, SEEK_SET);
    char *src = malloc(*len + 1);
    if (fread(src, 1, *len, f) != *len) { fprintf(stderr, "Read error\n"); exit(1); }
    src[*len] = '\0';
    fclose(f);
    return src;
}

/* ── SHA256 helpers ─────────────────────────────────────────────── */

static void double_sha256(const uint8_t *data, size_t len, uint8_t out[32])
{
    uint8_t tmp[32];
    SHA256(data, len, tmp);
    SHA256(tmp, 32, out);
}

/* ── SHA256 midstate (first 64 bytes of 80-byte header) ──────── */

static void compute_midstate(const uint8_t header[80], uint32_t midstate[8], uint32_t tail[4])
{
    midstate[0] = 0x6a09e667; midstate[1] = 0xbb67ae85;
    midstate[2] = 0x3c6ef372; midstate[3] = 0xa54ff53a;
    midstate[4] = 0x510e527f; midstate[5] = 0x9b05688c;
    midstate[6] = 0x1f83d9ab; midstate[7] = 0x5be0cd19;

    uint32_t W[64];
    for (int i = 0; i < 16; i++)
        W[i] = ((uint32_t)header[i*4] << 24) | ((uint32_t)header[i*4+1] << 16) |
               ((uint32_t)header[i*4+2] << 8) | (uint32_t)header[i*4+3];

    #define ROTR(x,n) (((x)>>(n))|((x)<<(32-(n))))
    #define s0(x) (ROTR(x,7)^ROTR(x,18)^((x)>>3))
    #define s1(x) (ROTR(x,17)^ROTR(x,19)^((x)>>10))
    #define S0(x) (ROTR(x,2)^ROTR(x,13)^ROTR(x,22))
    #define S1(x) (ROTR(x,6)^ROTR(x,11)^ROTR(x,25))
    #define Ch(x,y,z) (((x)&(y))^(~(x)&(z)))
    #define Maj(x,y,z) (((x)&(y))^((x)&(z))^((y)&(z)))

    for (int i = 16; i < 64; i++)
        W[i] = s1(W[i-2]) + W[i-7] + s0(W[i-15]) + W[i-16];

    static const uint32_t K[64] = {
        0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,
        0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
        0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,
        0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
        0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,
        0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
        0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,
        0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
        0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,
        0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
        0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,
        0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
        0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,
        0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
        0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,
        0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2
    };

    uint32_t a=midstate[0],b=midstate[1],c=midstate[2],d=midstate[3];
    uint32_t e=midstate[4],f=midstate[5],g=midstate[6],h=midstate[7];

    for (int i = 0; i < 64; i++) {
        uint32_t T1 = h + S1(e) + Ch(e,f,g) + K[i] + W[i];
        uint32_t T2 = S0(a) + Maj(a,b,c);
        h=g; g=f; f=e; e=d+T1; d=c; c=b; b=a; a=T1+T2;
    }

    midstate[0]+=a; midstate[1]+=b; midstate[2]+=c; midstate[3]+=d;
    midstate[4]+=e; midstate[5]+=f; midstate[6]+=g; midstate[7]+=h;

    #undef ROTR
    #undef s0
    #undef s1
    #undef S0
    #undef S1
    #undef Ch
    #undef Maj

    for (int i = 0; i < 4; i++)
        tail[i] = ((uint32_t)header[64+i*4] << 24) | ((uint32_t)header[64+i*4+1] << 16) |
                  ((uint32_t)header[64+i*4+2] << 8) | (uint32_t)header[64+i*4+3];
}

/* ── Hex utilities ──────────────────────────────────────────────── */

static int hex_to_bytes(const char *hex, uint8_t *out, size_t max)
{
    size_t hlen = strlen(hex);
    if (hlen & 1) return -1;
    size_t n = hlen / 2;
    if (n > max) return -1;
    for (size_t i = 0; i < n; i++) {
        int hi = hex[i*2], lo = hex[i*2+1];
        hi = (hi>='a') ? hi-'a'+10 : (hi>='A') ? hi-'A'+10 : hi-'0';
        lo = (lo>='a') ? lo-'a'+10 : (lo>='A') ? lo-'A'+10 : lo-'0';
        if (hi < 0 || hi > 15 || lo < 0 || lo > 15) return -1;
        out[i] = (hi << 4) | lo;
    }
    return (int)n;
}

static void bytes_to_hex(const uint8_t *data, int len, char *out)
{
    for (int i = 0; i < len; i++)
        sprintf(out + i*2, "%02x", data[i]);
    out[len*2] = '\0';
}

static void write_le32(uint8_t *buf, uint32_t v)
{
    buf[0]=v&0xFF; buf[1]=(v>>8)&0xFF; buf[2]=(v>>16)&0xFF; buf[3]=(v>>24)&0xFF;
}

static uint32_t read_le32(const uint8_t *buf)
{
    return (uint32_t)buf[0] | ((uint32_t)buf[1]<<8) | ((uint32_t)buf[2]<<16) | ((uint32_t)buf[3]<<24);
}

static void print_hash_raw_u32(const uint8_t hash[32])
{
    for (int w = 7; w >= 0; w--)
        printf("%08x", read_le32(hash + w*4));
}

/* ── TCP connection ─────────────────────────────────────────────── */

static int tcp_connect(const char *host, int port)
{
    struct addrinfo hints, *res, *rp;
    memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;

    char portstr[16];
    snprintf(portstr, sizeof(portstr), "%d", port);

    int rc = getaddrinfo(host, portstr, &hints, &res);
    if (rc != 0) {
        fprintf(stderr, "getaddrinfo(%s:%d): %s\n", host, port, gai_strerror(rc));
        return -1;
    }

    int fd = -1;
    for (rp = res; rp; rp = rp->ai_next) {
        fd = socket(rp->ai_family, rp->ai_socktype, rp->ai_protocol);
        if (fd < 0) continue;
        if (connect(fd, rp->ai_addr, rp->ai_addrlen) == 0) break;
        close(fd);
        fd = -1;
    }
    freeaddrinfo(res);

    if (fd >= 0) {
        int flag = 1;
        setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &flag, sizeof(flag));
    }
    return fd;
}

/* ── Stratum state ──────────────────────────────────────────────── */

typedef struct {
    char job_id[64];
    uint8_t prevhash[32];     /* raw LE bytes after un-swapping stratum's 4-byte-swapped hex */
    uint8_t coinbase1[512];
    int     coinbase1_len;
    uint8_t coinbase2[512];
    int     coinbase2_len;
    uint8_t merkle_branch[32][32];
    int     merkle_branch_count;
    uint32_t version;
    uint32_t bits;
    uint32_t ntime;
    int      clean;
    double   difficulty;
} stratum_job;

typedef struct {
    int fd;
    char recvbuf[8192];
    int  recvlen;

    uint8_t extranonce1[8];
    int     extranonce1_len;
    int     extranonce2_size;
    int     subscribed;
    int     authorized;

    stratum_job job;
    int         has_job;
    double      difficulty;

    int next_id;
} stratum_ctx;

static int stratum_send(stratum_ctx *ctx, const char *msg)
{
    size_t len = strlen(msg);
    ssize_t sent = send(ctx->fd, msg, len, 0);
    if (sent < 0) { perror("send"); return -1; }
    return 0;
}

static int stratum_send_json(stratum_ctx *ctx, json_t *val)
{
    char *s = json_dumps(val, JSON_COMPACT);
    if (!s) return -1;
    char buf[4096];
    snprintf(buf, sizeof(buf), "%s\n", s);
    free(s);
    return stratum_send(ctx, buf);
}

/* Read one newline-delimited JSON line. Returns JSON object or NULL. */
static json_t *stratum_recv_line(stratum_ctx *ctx, int timeout_ms)
{
    /* Check if we already have a complete line */
    for (;;) {
        char *nl = memchr(ctx->recvbuf, '\n', ctx->recvlen);
        if (nl) {
            *nl = '\0';
            json_error_t jerr;
            json_t *obj = json_loads(ctx->recvbuf, 0, &jerr);
            int consumed = (int)(nl - ctx->recvbuf) + 1;
            ctx->recvlen -= consumed;
            if (ctx->recvlen > 0)
                memmove(ctx->recvbuf, nl + 1, ctx->recvlen);
            return obj;
        }

        /* Need more data */
        fd_set fds;
        FD_ZERO(&fds);
        FD_SET(ctx->fd, &fds);
        struct timeval tv = { .tv_sec = timeout_ms / 1000, .tv_usec = (timeout_ms % 1000) * 1000 };
        int rc = select(ctx->fd + 1, &fds, NULL, NULL, &tv);
        if (rc <= 0) return NULL;

        ssize_t n = recv(ctx->fd, ctx->recvbuf + ctx->recvlen,
                         sizeof(ctx->recvbuf) - ctx->recvlen - 1, 0);
        if (n <= 0) return NULL;
        ctx->recvlen += (int)n;
    }
}

static int stratum_subscribe(stratum_ctx *ctx)
{
    json_t *req = json_pack("{s:i, s:s, s:[s]}",
                            "id", ctx->next_id++,
                            "method", "mining.subscribe",
                            "params", "sha256mem-miner/1.0");
    stratum_send_json(ctx, req);
    json_decref(req);

    json_t *resp = stratum_recv_line(ctx, 10000);
    if (!resp) { fprintf(stderr, "No subscribe response\n"); return -1; }

    json_t *result = json_object_get(resp, "result");
    if (!result || !json_is_array(result) || json_array_size(result) < 3) {
        fprintf(stderr, "Bad subscribe result\n");
        json_decref(resp);
        return -1;
    }

    const char *en1_hex = json_string_value(json_array_get(result, 1));
    int en2_size = (int)json_integer_value(json_array_get(result, 2));

    if (!en1_hex) { fprintf(stderr, "No extranonce1\n"); json_decref(resp); return -1; }

    ctx->extranonce1_len = hex_to_bytes(en1_hex, ctx->extranonce1, sizeof(ctx->extranonce1));
    ctx->extranonce2_size = en2_size;
    ctx->subscribed = 1;

    printf("Subscribed: extranonce1=%s extranonce2_size=%d\n", en1_hex, en2_size);
    json_decref(resp);
    return 0;
}

static int stratum_authorize(stratum_ctx *ctx, const char *user, const char *pass)
{
    json_t *req = json_pack("{s:i, s:s, s:[s,s]}",
                            "id", ctx->next_id++,
                            "method", "mining.authorize",
                            "params", user, pass);
    stratum_send_json(ctx, req);
    json_decref(req);

    json_t *resp = stratum_recv_line(ctx, 10000);
    if (!resp) { fprintf(stderr, "No authorize response\n"); return -1; }

    json_t *result = json_object_get(resp, "result");
    if (!json_is_true(result)) {
        fprintf(stderr, "Authorization failed\n");
        json_decref(resp);
        return -1;
    }

    ctx->authorized = 1;
    printf("Authorized.\n");
    json_decref(resp);
    return 0;
}

static void stratum_extranonce_subscribe(stratum_ctx *ctx)
{
    json_t *req = json_pack("{s:i, s:s, s:[]}",
                            "id", ctx->next_id++,
                            "method", "mining.extranonce.subscribe",
                            "params");
    stratum_send_json(ctx, req);
    json_decref(req);
}

/*
 * Decode stratum prevhash — try BOTH standard conventions:
 *   1. Bitcoin/ckpool: each 4-byte group is byte-swapped vs internal LE
 *   2. Raw hex: direct hex-to-bytes (some pools)
 *
 * We use the 4-byte-group swap (ckpool convention) since that's what
 * Fairchain's own stratum server uses. Set PREVHASH_NOSWAP=1 to disable.
 */
static void decode_stratum_prevhash(const char *hex, uint8_t out[32])
{
    uint8_t raw[32];
    hex_to_bytes(hex, raw, 32);
    for (int i = 0; i < 32; i += 4) {
        out[i+0] = raw[i+3];
        out[i+1] = raw[i+2];
        out[i+2] = raw[i+1];
        out[i+3] = raw[i+0];
    }
}

static uint32_t decode_hex_be32(const char *hex)
{
    uint8_t b[4];
    hex_to_bytes(hex, b, 4);
    return ((uint32_t)b[0]<<24) | ((uint32_t)b[1]<<16) | ((uint32_t)b[2]<<8) | b[3];
}

static int handle_mining_notify(stratum_ctx *ctx, json_t *params)
{
    if (!json_is_array(params) || json_array_size(params) < 9)
        return -1;

    stratum_job *j = &ctx->job;
    memset(j, 0, sizeof(*j));

    const char *job_id = json_string_value(json_array_get(params, 0));
    const char *prevhash_hex = json_string_value(json_array_get(params, 1));
    const char *cb1_hex = json_string_value(json_array_get(params, 2));
    const char *cb2_hex = json_string_value(json_array_get(params, 3));
    json_t *branches = json_array_get(params, 4);
    const char *version_hex = json_string_value(json_array_get(params, 5));
    const char *nbits_hex = json_string_value(json_array_get(params, 6));
    const char *ntime_hex = json_string_value(json_array_get(params, 7));
    int clean = json_is_true(json_array_get(params, 8));

    if (!job_id || !prevhash_hex || !cb1_hex || !cb2_hex || !version_hex || !nbits_hex || !ntime_hex)
        return -1;

    strncpy(j->job_id, job_id, sizeof(j->job_id) - 1);
    decode_stratum_prevhash(prevhash_hex, j->prevhash);
    j->coinbase1_len = hex_to_bytes(cb1_hex, j->coinbase1, sizeof(j->coinbase1));
    j->coinbase2_len = hex_to_bytes(cb2_hex, j->coinbase2, sizeof(j->coinbase2));
    j->version = decode_hex_be32(version_hex);
    j->bits = decode_hex_be32(nbits_hex);
    j->ntime = decode_hex_be32(ntime_hex);
    j->clean = clean;
    j->difficulty = ctx->difficulty;

    j->merkle_branch_count = 0;
    if (json_is_array(branches)) {
        int n = (int)json_array_size(branches);
        if (n > 32) n = 32;
        for (int i = 0; i < n; i++) {
            const char *bh = json_string_value(json_array_get(branches, i));
            if (bh) hex_to_bytes(bh, j->merkle_branch[i], 32);
            j->merkle_branch_count++;
        }
    }

    ctx->has_job = 1;

    printf("Job: %s  ntime=%u  bits=0x%08x  clean=%d  branches=%d\n",
           j->job_id, j->ntime, j->bits, j->clean, j->merkle_branch_count);
    printf("  RAW STRATUM PARAMS:\n");
    printf("    prevhash=%s\n", prevhash_hex);
    printf("    cb1=%s\n", cb1_hex);
    printf("    cb2=%s\n", cb2_hex);
    printf("    version=%s  nbits=%s  ntime=%s\n", version_hex, nbits_hex, ntime_hex);
    printf("    en1="); for (int i = 0; i < ctx->extranonce1_len; i++) printf("%02x", ctx->extranonce1[i]); printf(" en2_size=%d\n", ctx->extranonce2_size);
    printf("  prevhash(decoded): ");
    for (int i = 0; i < 32; i++) printf("%02x", j->prevhash[i]);
    printf("\n");

    return 0;
}

/*
 * Convert standard Stratum difficulty to a Bitcoin-style big-endian target.
 * Suprnova's FAIR pool uses a standard stratum server, so share difficulty is
 * scored against the final sha256mem digest as a normal 256-bit BE hash.
 */
static void difficulty_to_target(double diff, uint32_t target[8])
{
    if (diff <= 0) {
        diff = 1.0;
    }

    memset(target, 0, 8 * sizeof(uint32_t));

    long double scale = 65535.0L / (long double)diff;
    long double max_scale = 281474976710655.0L; /* 2^48 - 1 */
    if (scale >= max_scale) {
        for (int i = 0; i < 8; i++) target[i] = 0xFFFFFFFFu;
        return;
    }

    uint64_t scaled = (uint64_t)scale;
    target[0] = (uint32_t)(scaled >> 16);
    target[1] = (uint32_t)((scaled & 0xFFFFu) << 16);
}

static int handle_set_difficulty(stratum_ctx *ctx, json_t *params)
{
    if (!json_is_array(params) || json_array_size(params) < 1) return -1;
    double d = json_number_value(json_array_get(params, 0));
    if (d > 0) {
        ctx->difficulty = d;
        ctx->job.difficulty = d;
        printf("Difficulty set: %g\n", d);
    }
    return 0;
}

#define STRATUM_POLL_JOB  1
#define STRATUM_POLL_DIFF 2

/* Process any pending stratum messages (non-blocking). Returns STRATUM_POLL_* flags. */
static int stratum_poll(stratum_ctx *ctx)
{
    int flags = 0;
    for (;;) {
        json_t *msg = stratum_recv_line(ctx, 0);
        if (!msg) break;

        const char *method = json_string_value(json_object_get(msg, "method"));
        json_t *params = json_object_get(msg, "params");

        if (method) {
            if (strcmp(method, "mining.notify") == 0) {
                handle_mining_notify(ctx, params);
                flags |= STRATUM_POLL_JOB;
            } else if (strcmp(method, "mining.set_difficulty") == 0) {
                handle_set_difficulty(ctx, params);
                flags |= STRATUM_POLL_DIFF;
            }
        }
        json_decref(msg);
    }
    return flags;
}

/* Wait for first job (blocking, up to timeout_s seconds) */
static int stratum_wait_job(stratum_ctx *ctx, int timeout_s)
{
    for (int i = 0; i < timeout_s * 10 && g_running; i++) {
        json_t *msg = stratum_recv_line(ctx, 100);
        if (!msg) continue;

        const char *method = json_string_value(json_object_get(msg, "method"));
        json_t *params = json_object_get(msg, "params");

        if (method) {
            if (strcmp(method, "mining.notify") == 0) {
                handle_mining_notify(ctx, params);
                json_decref(msg);
                return 0;
            } else if (strcmp(method, "mining.set_difficulty") == 0) {
                handle_set_difficulty(ctx, params);
            }
        }
        json_decref(msg);
    }
    return -1;
}

/*
 * Build 80-byte header from stratum job + extranonce.
 * Returns the extranonce2 used (for submit).
 */
static void build_header_from_job(const stratum_ctx *ctx, uint32_t en2_val, uint8_t header[80])
{
    const stratum_job *j = &ctx->job;

    /* Build coinbase: cb1 + extranonce1 + extranonce2 + cb2 */
    uint8_t coinbase[1024];
    int pos = 0;
    memcpy(coinbase + pos, j->coinbase1, j->coinbase1_len); pos += j->coinbase1_len;
    memcpy(coinbase + pos, ctx->extranonce1, ctx->extranonce1_len); pos += ctx->extranonce1_len;

    /* extranonce2: little-endian bytes */
    for (int i = 0; i < ctx->extranonce2_size; i++)
        coinbase[pos++] = (en2_val >> (8*i)) & 0xFF;

    memcpy(coinbase + pos, j->coinbase2, j->coinbase2_len); pos += j->coinbase2_len;

    /* double-SHA256 coinbase → coinbase hash */
    uint8_t cb_hash[32];
    double_sha256(coinbase, pos, cb_hash);

    /* Walk merkle branch: current = double_sha256(current || branch[i]) */
    uint8_t merkle_root[32];
    memcpy(merkle_root, cb_hash, 32);
    for (int i = 0; i < j->merkle_branch_count; i++) {
        uint8_t combined[64];
        memcpy(combined, merkle_root, 32);
        memcpy(combined + 32, j->merkle_branch[i], 32);
        double_sha256(combined, 64, merkle_root);
    }

    /* Suprnova's FAIR stratum validates against the internal little-endian
     * merkle hash bytes. SRBMiner accepted-share captures match this layout. */
    uint8_t merkle_header[32];
    for (int i = 0; i < 32; i++)
        merkle_header[i] = merkle_root[31 - i];

    /* Build header: version(4 LE) + prevhash(32) + merkle_root(32) + ntime(4 LE) + bits(4 LE) + nonce(4 LE=0) */
    write_le32(header + 0, j->version);
    memcpy(header + 4, j->prevhash, 32);
    memcpy(header + 36, merkle_header, 32);
    write_le32(header + 68, j->ntime);
    write_le32(header + 72, j->bits);
    write_le32(header + 76, 0);  /* nonce filled by GPU */
}

/*
 * Convert compact bits to target for GPU kernel comparison.
 * Target stored as 8 × uint32_t LE words (same layout as kernel's final_hash).
 */
static void bits_to_target(uint32_t bits, uint32_t target[8])
{
    uint8_t le[32];
    memset(le, 0, 32);
    uint32_t mantissa = bits & 0x007FFFFF;
    uint32_t exponent = bits >> 24;
    if (exponent >= 3) {
        int base = exponent - 3;
        if (base < 32)     le[base]     = mantissa & 0xFF;
        if (base+1 < 32)   le[base+1]   = (mantissa >> 8) & 0xFF;
        if (base+2 < 32)   le[base+2]   = (mantissa >> 16) & 0xFF;
    } else {
        mantissa >>= 8 * (3 - exponent);
        le[0] = mantissa & 0xFF;
    }
    for (int w = 0; w < 8; w++)
        target[w] = read_le32(le + w*4);
}

/* Submit share to pool */
static int stratum_submit(stratum_ctx *ctx, const char *worker,
                          const char *job_id, uint32_t en2_val,
                          uint32_t ntime, uint32_t nonce)
{
    char en2_hex[32], ntime_hex[16], nonce_hex[16];
    int submit_id = ctx->next_id++;

    /* extranonce2: little-endian hex */
    uint8_t en2_bytes[8];
    for (int i = 0; i < ctx->extranonce2_size; i++)
        en2_bytes[i] = (en2_val >> (8*i)) & 0xFF;
    bytes_to_hex(en2_bytes, ctx->extranonce2_size, en2_hex);

    /* ntime: big-endian hex (Fairchain stratum convention) */
    sprintf(ntime_hex, "%08x", ntime);

    /* Standard stratum pools submit nonce as big-endian text; the server
       decodes the integer and serializes it LE into the block header. */
    sprintf(nonce_hex, "%08x", nonce);

    json_t *req = json_pack("{s:i, s:s, s:[s,s,s,s,s]}",
                            "id", submit_id,
                            "method", "mining.submit",
                            "params", worker, job_id, en2_hex, ntime_hex, nonce_hex);

    printf("  Submitting: job=%s en2=%s ntime=%s nonce=%s\n",
           job_id, en2_hex, ntime_hex, nonce_hex);

    int rc = stratum_send_json(ctx, req);
    json_decref(req);
    return rc == 0 ? submit_id : -1;
}

/* ── Main ───────────────────────────────────────────────────────── */

int main(int argc, char **argv)
{
    const char *pool_url = "stratum+tcp://127.0.0.1:3333";
    const char *worker_name = "tmto";
    const char *password = "x";
    int num_workers = 0;

    for (int i = 1; i < argc; i++) {
        if ((strcmp(argv[i], "-o") == 0 || strcmp(argv[i], "--url") == 0) && i+1 < argc)
            pool_url = argv[++i];
        else if ((strcmp(argv[i], "-u") == 0 || strcmp(argv[i], "--user") == 0) && i+1 < argc)
            worker_name = argv[++i];
        else if ((strcmp(argv[i], "-p") == 0 || strcmp(argv[i], "--pass") == 0) && i+1 < argc)
            password = argv[++i];
        else if (strcmp(argv[i], "--workers") == 0 && i+1 < argc)
            num_workers = atoi(argv[++i]);
        else {
            fprintf(stderr,
                    "Usage: %s [-o stratum+tcp://HOST:PORT] [-u WORKER] [-p PASS] [--workers N]\n"
                    "  Defaults: -o stratum+tcp://127.0.0.1:3333 -u tmto -p x (Fairchain-Qt stratum)\n",
                    argv[0]);
            return 1;
        }
    }

    signal(SIGINT, sighandler);
    signal(SIGTERM, sighandler);

    /* Parse pool URL: stratum+tcp://host:port */
    char pool_host[256] = {0};
    /* Fairchain wallet stratum default; used when URL has no :port */
    int pool_port = 3333;
    {
        const char *h = pool_url;
        if (strncmp(h, "stratum+tcp://", 14) == 0) h += 14;
        else if (strncmp(h, "tcp://", 6) == 0) h += 6;

        const char *colon = strrchr(h, ':');
        if (colon) {
            int hlen = (int)(colon - h);
            if (hlen > 0 && hlen < (int)sizeof(pool_host)) {
                memcpy(pool_host, h, hlen);
                pool_host[hlen] = '\0';
            }
            pool_port = atoi(colon + 1);
            if (pool_port <= 0)
                pool_port = 3333;
        } else {
            strncpy(pool_host, h, sizeof(pool_host) - 1);
        }
    }

    const size_t MEM_PER_WORKER = (2097152UL / 128UL) * 32UL;  /* 512 KiB TMTO */

    /* ── OpenCL setup ──────────────────────────────────────── */
    cl_platform_id platform;
    cl_device_id device;
    cl_int err;

    CL_CHECK(clGetPlatformIDs(1, &platform, NULL), "get platform");
    CL_CHECK(clGetDeviceIDs(platform, CL_DEVICE_TYPE_GPU, 1, &device, NULL), "get device");

    char dev_name[256];
    size_t dev_gmem;
    cl_uint vendor_id = 0;
    clGetDeviceInfo(device, CL_DEVICE_NAME, sizeof(dev_name), dev_name, NULL);
    clGetDeviceInfo(device, CL_DEVICE_GLOBAL_MEM_SIZE, sizeof(dev_gmem), &dev_gmem, NULL);
    clGetDeviceInfo(device, CL_DEVICE_VENDOR_ID, sizeof(vendor_id), &vendor_id, NULL);

    /* Load autotune cache */
    if (num_workers <= 0) {
        char cmd[256];
        snprintf(cmd, sizeof(cmd), "ls Autotune/sha256mem_%04x_* 2>/dev/null | head -1", vendor_id);
        FILE *p = popen(cmd, "r");
        if (p) {
            char path[256] = {0};
            if (fgets(path, sizeof(path), p)) {
                path[strcspn(path, "\n")] = '\0';
                FILE *fc = fopen(path, "r");
                if (fc) {
                    int cached = 0;
                    if (fscanf(fc, "%d", &cached) == 1 && cached > 0) {
                        num_workers = cached;
                        printf("Loaded autotune: %d workers from %s\n", num_workers, path);
                    }
                    fclose(fc);
                }
            }
            pclose(p);
        }
    }

    if (num_workers <= 0) {
        int w = (int)((dev_gmem * 0.64) / MEM_PER_WORKER);
        w = (w / 32) * 32;
        if (w < 256) w = 256;
        num_workers = w;
    }

    size_t total_vram = (size_t)num_workers * MEM_PER_WORKER;

    printf("═══════════════════════════════════════════════════\n");
    printf("  sha256mem Stratum GPU Miner — Fairchain\n");
    printf("═══════════════════════════════════════════════════\n");
    printf("  GPU:       %s\n", dev_name);
    printf("  VRAM:      %lu / %lu MiB\n",
           (unsigned long)(total_vram/(1024*1024)),
           (unsigned long)(dev_gmem/(1024*1024)));
    printf("  Workers:   %d (TMTO 512 KiB each)\n", num_workers);
    printf("  Pool:      %s:%d\n", pool_host, pool_port);
    printf("  User:      %s\n", worker_name);
    printf("═══════════════════════════════════════════════════\n\n");

    cl_context ctx_cl = clCreateContext(NULL, 1, &device, NULL, NULL, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create context: %d\n", err); return 1; }
    cl_command_queue queue = clCreateCommandQueue(ctx_cl, device, 0, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create queue: %d\n", err); return 1; }

    size_t src_len;
    char *src = load_kernel_source("opencl/sha256mem_v2_tmto_gpu.cl", &src_len);
    cl_program prog = clCreateProgramWithSource(ctx_cl, 1, (const char **)&src, &src_len, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create program: %d\n", err); return 1; }

    printf("Compiling kernel...\n");
    {
        char build_opts[512];
        snprintf(build_opts, sizeof(build_opts),
                 "-cl-mad-enable -cl-fast-relaxed-math -cl-std=CL1.2%s",
                 strstr(dev_name, "NVIDIA") != NULL ? " -cl-nv-opt-level=3" : "");
        err = clBuildProgram(prog, 1, &device, build_opts, NULL, NULL);
    }
    if (err != CL_SUCCESS) {
        size_t log_len;
        clGetProgramBuildInfo(prog, device, CL_PROGRAM_BUILD_LOG, 0, NULL, &log_len);
        char *log = malloc(log_len + 1);
        clGetProgramBuildInfo(prog, device, CL_PROGRAM_BUILD_LOG, log_len, log, NULL);
        log[log_len] = '\0';
        fprintf(stderr, "Build failed:\n%s\n", log);
        free(log); return 1;
    }
    printf("Kernel compiled.\n\n");
    free(src);

    /* Allocate GPU buffers */
    cl_mem buf_midstate = clCreateBuffer(ctx_cl, CL_MEM_READ_ONLY, 8*sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc midstate");
    cl_mem buf_tail = clCreateBuffer(ctx_cl, CL_MEM_READ_ONLY, 4*sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc tail");
    cl_mem buf_mem = clCreateBuffer(ctx_cl, CL_MEM_READ_WRITE, total_vram, NULL, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "VRAM alloc failed: %s\n", cl_err_str(err)); return 1; }
    cl_mem buf_counts = clCreateBuffer(ctx_cl, CL_MEM_READ_WRITE, num_workers*sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc counts");
    cl_mem buf_found = clCreateBuffer(ctx_cl, CL_MEM_READ_WRITE, sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc found");
    cl_mem buf_nonce = clCreateBuffer(ctx_cl, CL_MEM_READ_WRITE, sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc nonce");
    cl_mem buf_hash = clCreateBuffer(ctx_cl, CL_MEM_READ_WRITE, 8*sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc hash");
    cl_mem buf_target = clCreateBuffer(ctx_cl, CL_MEM_READ_ONLY, 8*sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc target");

    cl_kernel kernel = clCreateKernel(prog, "sha256mem_mine", &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create kernel: %d (%s)\n", err, cl_err_str(err)); return 1; }

    CL_CHECK(clSetKernelArg(kernel, 0, sizeof(cl_mem), &buf_midstate), "arg 0");
    CL_CHECK(clSetKernelArg(kernel, 1, sizeof(cl_mem), &buf_tail), "arg 1");
    CL_CHECK(clSetKernelArg(kernel, 2, sizeof(cl_mem), &buf_mem), "arg 2");
    CL_CHECK(clSetKernelArg(kernel, 3, sizeof(cl_mem), &buf_counts), "arg 3");
    CL_CHECK(clSetKernelArg(kernel, 4, sizeof(cl_mem), &buf_found), "arg 4");
    CL_CHECK(clSetKernelArg(kernel, 5, sizeof(cl_mem), &buf_nonce), "arg 5");
    CL_CHECK(clSetKernelArg(kernel, 6, sizeof(cl_mem), &buf_hash), "arg 6");
    CL_CHECK(clSetKernelArg(kernel, 7, sizeof(cl_mem), &buf_target), "arg 7");

    uint32_t hpi = 1;
    CL_CHECK(clSetKernelArg(kernel, 9, sizeof(uint32_t), &hpi), "arg 9");

    size_t global_size = (size_t)num_workers;
    uint32_t *hash_counts_host = calloc(num_workers, sizeof(uint32_t));

    /* ── Connect to pool ───────────────────────────────── */
reconnect:
    while (g_running) {
        printf("Connecting to %s:%d...\n", pool_host, pool_port);

        stratum_ctx sctx;
        memset(&sctx, 0, sizeof(sctx));
        sctx.next_id = 1;
        sctx.difficulty = 1.0;

        sctx.fd = tcp_connect(pool_host, pool_port);
        if (sctx.fd < 0) {
            fprintf(stderr, "Connection failed. Retrying in 5s...\n");
            sleep(5);
            continue;
        }
        printf("Connected.\n");

        if (stratum_subscribe(&sctx) < 0) {
            close(sctx.fd);
            sleep(5);
            continue;
        }

        if (stratum_authorize(&sctx, worker_name, password) < 0) {
            close(sctx.fd);
            sleep(5);
            continue;
        }
        stratum_extranonce_subscribe(&sctx);

        /* Wait for first job */
        printf("Waiting for work...\n");
        if (stratum_wait_job(&sctx, 30) < 0) {
            fprintf(stderr, "No job received. Reconnecting...\n");
            close(sctx.fd);
            sleep(2);
            continue;
        }

        uint64_t total_shares = 0;
        uint64_t accepted = 0;
        uint64_t rejected = 0;
        uint64_t total_hashes = 0;
        time_t start_time = time(NULL);
        uint32_t extranonce2_counter = (uint32_t)time(NULL);

        /* ── Mining loop ──────────────────────────────── */
        while (g_running) {
            if (!sctx.has_job) {
                if (stratum_wait_job(&sctx, 10) < 0) {
                    fprintf(stderr, "Lost connection. Reconnecting...\n");
                    close(sctx.fd);
                    goto reconnect;
                }
                continue;
            }

            stratum_job cur_job;
            memcpy(&cur_job, &sctx.job, sizeof(cur_job));
            uint32_t en2_val = extranonce2_counter;

            /* Build 80-byte header */
            uint8_t header[80];
            build_header_from_job(&sctx, en2_val, header);

            /* Compute midstate */
            uint32_t midstate[8], tail[4];
            compute_midstate(header, midstate, tail);

            /* Convert pool share difficulty to target for GPU */
            uint32_t target[8];
            double share_diff = cur_job.difficulty > 0 ? cur_job.difficulty : sctx.difficulty;
            if (share_diff <= 0) share_diff = 0.001;
            difficulty_to_target(share_diff, target);

            printf("  Mining job=%s  diff=%g  target[7]=0x%08x\n",
                   cur_job.job_id, share_diff, target[7]);

            /* Debug: compare sha256mem vs sha256d for nonce=0 */
            {
                uint8_t cpu_hash[32], sha256d_hash[32];
                sha256mem_v2_hash(header, 80, cpu_hash);
                double_sha256(header, 80, sha256d_hash);
                printf("  sha256mem(n=0): ");
                for (int i = 0; i < 8; i++) printf("%02x", cpu_hash[i]);
                printf("...  sha256d(n=0): ");
                for (int i = 0; i < 8; i++) printf("%02x", sha256d_hash[i]);
                printf("...\n");
            }

            CL_CHECK(clEnqueueWriteBuffer(queue, buf_midstate, CL_TRUE, 0, 8*sizeof(uint32_t), midstate, 0, NULL, NULL), "upload midstate");
            CL_CHECK(clEnqueueWriteBuffer(queue, buf_tail, CL_TRUE, 0, 4*sizeof(uint32_t), tail, 0, NULL, NULL), "upload tail");
            CL_CHECK(clEnqueueWriteBuffer(queue, buf_target, CL_TRUE, 0, 8*sizeof(uint32_t), target, 0, NULL, NULL), "upload target");

            uint32_t nonce_offset = 0;
            int found_share = 0;
            uint64_t job_hashes = 0;
            struct timespec job_start;
            clock_gettime(CLOCK_MONOTONIC, &job_start);

            while (g_running && !found_share) {
                int restart_job = 0;

                /* Check for new work from pool */
                int poll_flags = stratum_poll(&sctx);
                if ((poll_flags & STRATUM_POLL_JOB) && sctx.job.clean) {
                    break;
                }
                if (poll_flags & STRATUM_POLL_DIFF) {
                    cur_job.difficulty = sctx.difficulty;
                    share_diff = sctx.difficulty;
                    if (share_diff <= 0) share_diff = 0.001;
                    difficulty_to_target(share_diff, target);
                    CL_CHECK(clEnqueueWriteBuffer(queue, buf_target, CL_TRUE, 0, 8*sizeof(uint32_t), target, 0, NULL, NULL), "upload updated target");
                    printf("  Updated target for diff=%g  target[7]=0x%08x\n", share_diff, target[7]);
                }

                /* Reset GPU state */
                uint32_t zero = 0;
                CL_CHECK(clEnqueueWriteBuffer(queue, buf_found, CL_TRUE, 0, sizeof(uint32_t), &zero, 0, NULL, NULL), "reset found");
                memset(hash_counts_host, 0, num_workers*sizeof(uint32_t));
                CL_CHECK(clEnqueueWriteBuffer(queue, buf_counts, CL_TRUE, 0, num_workers*sizeof(uint32_t), hash_counts_host, 0, NULL, NULL), "reset counts");
                CL_CHECK(clSetKernelArg(kernel, 8, sizeof(uint32_t), &nonce_offset), "arg 8 nonce");

                cl_int enq = clEnqueueNDRangeKernel(queue, kernel, 1, NULL, &global_size, NULL, 0, NULL, NULL);
                if (enq != CL_SUCCESS) {
                    fprintf(stderr, "Kernel launch failed: %d\n", enq);
                    break;
                }
                CL_CHECK(clFinish(queue), "finish");

                /* Read results */
                uint32_t found_flag = 0;
                CL_CHECK(clEnqueueReadBuffer(queue, buf_found, CL_TRUE, 0, sizeof(uint32_t), &found_flag, 0, NULL, NULL), "read found");

                CL_CHECK(clEnqueueReadBuffer(queue, buf_counts, CL_TRUE, 0, num_workers*sizeof(uint32_t), hash_counts_host, 0, NULL, NULL), "read counts");
                uint64_t batch_hashes = 0;
                for (int i = 0; i < num_workers; i++) batch_hashes += hash_counts_host[i];
                job_hashes += batch_hashes;
                total_hashes += batch_hashes;

                if (found_flag) {
                    uint32_t winning_nonce = 0;
                    CL_CHECK(clEnqueueReadBuffer(queue, buf_nonce, CL_TRUE, 0, sizeof(uint32_t), &winning_nonce, 0, NULL, NULL), "read nonce");

                    /* Read the hash for debug */
                    uint32_t found_hash_words[8];
                    CL_CHECK(clEnqueueReadBuffer(queue, buf_hash, CL_TRUE, 0, 8*sizeof(uint32_t), found_hash_words, 0, NULL, NULL), "read hash");
                    printf("  Found: nonce=%u (0x%08x)  hash=", winning_nonce, winning_nonce);
                    for (int w = 7; w >= 0; w--) printf("%08x", found_hash_words[w]);
                    printf("\n");

                    uint8_t submit_header[80], cpu_submit_hash[32];
                    memcpy(submit_header, header, sizeof(submit_header));
                    write_le32(submit_header + 76, winning_nonce);
                    sha256mem_v2_hash(submit_header, 80, cpu_submit_hash);
                    printf("  CPU check: nonce_bytes=%02x%02x%02x%02x hash=",
                           submit_header[76], submit_header[77],
                           submit_header[78], submit_header[79]);
                    print_hash_raw_u32(cpu_submit_hash);
                    printf("\n");

                    total_shares++;

                    int submit_id = stratum_submit(&sctx, worker_name, cur_job.job_id,
                                                   en2_val, cur_job.ntime, winning_nonce);
                    if (submit_id >= 0) {
                        /* Read pool responses (may include notifications interleaved) */
                        for (int ri = 0; ri < 3; ri++) {
                            json_t *resp = stratum_recv_line(&sctx, ri == 0 ? 5000 : 100);
                            if (!resp) break;

                            const char *method = json_string_value(json_object_get(resp, "method"));
                            if (method) {
                                if (strcmp(method, "mining.notify") == 0) {
                                    handle_mining_notify(&sctx, json_object_get(resp, "params"));
                                    if (sctx.job.clean) {
                                        restart_job = 1;
                                    }
                                } else if (strcmp(method, "mining.set_difficulty") == 0) {
                                    handle_set_difficulty(&sctx, json_object_get(resp, "params"));
                                    cur_job.difficulty = sctx.difficulty;
                                    share_diff = sctx.difficulty;
                                    if (share_diff <= 0) share_diff = 0.001;
                                    difficulty_to_target(share_diff, target);
                                    CL_CHECK(clEnqueueWriteBuffer(queue, buf_target, CL_TRUE, 0, 8*sizeof(uint32_t), target, 0, NULL, NULL), "upload updated target");
                                    printf("  Updated target for diff=%g  target[7]=0x%08x\n", share_diff, target[7]);
                                }
                                json_decref(resp);
                                if (restart_job) break;
                                continue;
                            }

                            json_t *result = json_object_get(resp, "result");
                            json_t *id = json_object_get(resp, "id");
                            (void)submit_id;
                            if (!json_is_integer(id) || json_integer_value(id) < 4) {
                                json_decref(resp);
                                continue;
                            }
                            if (json_is_true(result)) {
                                accepted++;
                                printf("  ACCEPTED! (shares: %lu/%lu)\n",
                                       (unsigned long)accepted, (unsigned long)total_shares);
                            } else {
                                rejected++;
                                char *raw = json_dumps(resp, JSON_COMPACT);
                                printf("  REJECTED: %s\n", raw ? raw : "(null)");
                                if (raw) free(raw);
                            }
                            json_decref(resp);
                            break;
                        }
                    }

                    if (restart_job) {
                        printf("  Clean job received; switching from job=%s to job=%s\n",
                               cur_job.job_id, sctx.job.job_id);
                        break;
                    }

                    found_share = 0;
                }

                nonce_offset += num_workers * hpi;

                /* Periodic status */
                struct timespec now;
                clock_gettime(CLOCK_MONOTONIC, &now);
                double dt = (now.tv_sec - job_start.tv_sec) + (now.tv_nsec - job_start.tv_nsec)/1e9;
                if (!found_share && (int)dt % 5 == 0 && dt > 0.5) {
                    double rate = (double)total_hashes / (double)(time(NULL) - start_time);
                    printf("  [%s] %.1f H/s  hashes=%lu  shares=%lu/%lu\n",
                           cur_job.job_id, rate, (unsigned long)total_hashes,
                           (unsigned long)accepted, (unsigned long)total_shares);
                }

                /* Wrap nonce space — move to next extranonce2 */
                if (nonce_offset > 0xFFF00000u) {
                    break;
                }
            }
        }

        close(sctx.fd);
    }

    printf("\nStratum miner stopped.\n");

    /* Cleanup */
    clReleaseMemObject(buf_midstate);
    clReleaseMemObject(buf_tail);
    clReleaseMemObject(buf_mem);
    clReleaseMemObject(buf_counts);
    clReleaseMemObject(buf_found);
    clReleaseMemObject(buf_nonce);
    clReleaseMemObject(buf_hash);
    clReleaseMemObject(buf_target);
    clReleaseKernel(kernel);
    clReleaseProgram(prog);
    clReleaseCommandQueue(queue);
    clReleaseContext(ctx_cl);
    free(hash_counts_host);

    return 0;
}
