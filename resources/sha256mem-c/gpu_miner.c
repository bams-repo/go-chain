/*
 * sha256mem GPU Miner — submits blocks to a Fairchain daemon
 * ============================================================
 * Uses the optimized OpenCL kernel to mine blocks and submits them
 * via the REST /submitblock endpoint.
 *
 * Build:
 *   gcc -O2 -o gpu_miner gpu_miner.c -lOpenCL -lcrypto -lcurl -lm
 *
 * Run:
 *   ./gpu_miner                                    # defaults: localhost:19335
 *   ./gpu_miner --rpc http://127.0.0.1:19335       # explicit RPC
 *   ./gpu_miner --workers 60                        # override worker count
 *   ./gpu_miner --honest                            # use wall-clock timestamps
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
#include <openssl/sha.h>
#include <curl/curl.h>

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

/* ── SHA256 midstate ────────────────────────────────────────────── */

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

    uint32_t a=midstate[0], b=midstate[1], c=midstate[2], d=midstate[3];
    uint32_t e=midstate[4], f=midstate[5], g=midstate[6], h=midstate[7];

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

/* ── HTTP / JSON helpers (using libcurl) ────────────────────────── */

struct membuf {
    char  *data;
    size_t len;
    size_t cap;
};

static size_t write_cb(void *ptr, size_t size, size_t nmemb, void *userdata)
{
    struct membuf *buf = userdata;
    size_t total = size * nmemb;
    if (buf->len + total + 1 > buf->cap) {
        buf->cap = (buf->len + total + 1) * 2;
        buf->data = realloc(buf->data, buf->cap);
    }
    memcpy(buf->data + buf->len, ptr, total);
    buf->len += total;
    buf->data[buf->len] = '\0';
    return total;
}

static char *http_get(const char *url)
{
    CURL *c = curl_easy_init();
    if (!c) return NULL;
    struct membuf buf = { .data = malloc(4096), .len = 0, .cap = 4096 };
    curl_easy_setopt(c, CURLOPT_URL, url);
    curl_easy_setopt(c, CURLOPT_WRITEFUNCTION, write_cb);
    curl_easy_setopt(c, CURLOPT_WRITEDATA, &buf);
    curl_easy_setopt(c, CURLOPT_TIMEOUT, 10L);
    CURLcode res = curl_easy_perform(c);
    curl_easy_cleanup(c);
    if (res != CURLE_OK) { free(buf.data); return NULL; }
    return buf.data;
}

static char *http_post_binary(const char *url, const uint8_t *data, size_t len)
{
    CURL *c = curl_easy_init();
    if (!c) return NULL;
    struct membuf buf = { .data = malloc(4096), .len = 0, .cap = 4096 };
    curl_easy_setopt(c, CURLOPT_URL, url);
    curl_easy_setopt(c, CURLOPT_POST, 1L);
    curl_easy_setopt(c, CURLOPT_POSTFIELDS, data);
    curl_easy_setopt(c, CURLOPT_POSTFIELDSIZE, (long)len);
    struct curl_slist *hdrs = curl_slist_append(NULL, "Content-Type: application/octet-stream");
    curl_easy_setopt(c, CURLOPT_HTTPHEADER, hdrs);
    curl_easy_setopt(c, CURLOPT_WRITEFUNCTION, write_cb);
    curl_easy_setopt(c, CURLOPT_WRITEDATA, &buf);
    curl_easy_setopt(c, CURLOPT_TIMEOUT, 30L);
    CURLcode res = curl_easy_perform(c);
    long http_code = 0;
    curl_easy_getinfo(c, CURLINFO_RESPONSE_CODE, &http_code);
    curl_slist_free_all(hdrs);
    curl_easy_cleanup(c);
    if (res != CURLE_OK) { free(buf.data); return NULL; }
    return buf.data;
}

/* Minimal JSON string value extractor — finds "key":"value" or "key":number */
static int json_get_string(const char *json, const char *key, char *out, size_t outlen)
{
    char needle[256];
    snprintf(needle, sizeof(needle), "\"%s\"", key);
    const char *p = strstr(json, needle);
    if (!p) return -1;
    p += strlen(needle);
    while (*p == ' ' || *p == ':' || *p == '\t') p++;
    if (*p == '"') {
        p++;
        size_t i = 0;
        while (*p && *p != '"' && i < outlen - 1) out[i++] = *p++;
        out[i] = '\0';
        return 0;
    }
    /* number */
    size_t i = 0;
    while (*p && *p != ',' && *p != '}' && *p != ' ' && i < outlen - 1) out[i++] = *p++;
    out[i] = '\0';
    return 0;
}

/* ── Block serialization helpers ────────────────────────────────── */

static void write_le32(uint8_t *buf, uint32_t v)
{
    buf[0] = v & 0xFF; buf[1] = (v>>8)&0xFF; buf[2] = (v>>16)&0xFF; buf[3] = (v>>24)&0xFF;
}

static void write_le64(uint8_t *buf, uint64_t v)
{
    for (int i = 0; i < 8; i++) buf[i] = (v >> (8*i)) & 0xFF;
}

static int write_varint(uint8_t *buf, uint64_t v)
{
    if (v < 0xFD) { buf[0] = (uint8_t)v; return 1; }
    if (v <= 0xFFFF) { buf[0] = 0xFD; buf[1] = v&0xFF; buf[2] = (v>>8)&0xFF; return 3; }
    buf[0] = 0xFE; write_le32(buf+1, (uint32_t)v); return 5;
}

static void hex_to_bytes_reverse(const char *hex, uint8_t *out, int nbytes)
{
    for (int i = 0; i < nbytes; i++) {
        int hi = hex[i*2], lo = hex[i*2+1];
        hi = (hi >= 'a') ? hi - 'a' + 10 : (hi >= 'A') ? hi - 'A' + 10 : hi - '0';
        lo = (lo >= 'a') ? lo - 'a' + 10 : (lo >= 'A') ? lo - 'A' + 10 : lo - '0';
        out[nbytes - 1 - i] = (hi << 4) | lo;
    }
}

static void double_sha256(const uint8_t *data, size_t len, uint8_t out[32])
{
    uint8_t tmp[32];
    SHA256(data, len, tmp);
    SHA256(tmp, 32, out);
}

/* Build coinbase transaction, returns serialized length */
static int build_coinbase(uint8_t *buf, uint32_t height, uint64_t subsidy)
{
    uint8_t *p = buf;

    /* version */
    write_le32(p, 1); p += 4;
    /* input count */
    *p++ = 1;
    /* prev outpoint (coinbase: all zeros + 0xFFFFFFFF) */
    memset(p, 0, 32); p += 32;
    write_le32(p, 0xFFFFFFFF); p += 4;

    /* scriptSig: BIP34 height encoding + tag */
    uint8_t scriptsig[64];
    int push_len;
    if (height <= 0xFF) push_len = 1;
    else if (height <= 0xFFFF) push_len = 2;
    else if (height <= 0xFFFFFF) push_len = 3;
    else push_len = 4;

    scriptsig[0] = (uint8_t)push_len;
    for (int i = 0; i < push_len; i++)
        scriptsig[1 + i] = (height >> (8*i)) & 0xFF;

    const char *tag = "gpu-miner";
    int tag_len = strlen(tag);
    memcpy(scriptsig + 1 + push_len, tag, tag_len);
    int scriptsig_len = 1 + push_len + tag_len;

    p += write_varint(p, scriptsig_len);
    memcpy(p, scriptsig, scriptsig_len); p += scriptsig_len;

    /* sequence */
    write_le32(p, 0xFFFFFFFF); p += 4;

    /* output count */
    *p++ = 1;
    /* value */
    write_le64(p, subsidy); p += 8;
    /* pkScript: OP_FALSE (anyone can spend, same as daemon default) */
    *p++ = 1;  /* script length */
    *p++ = 0x00;

    /* locktime */
    write_le32(p, 0); p += 4;

    return (int)(p - buf);
}

/* Compute merkle root from a single transaction hash */
static void compute_merkle_root(const uint8_t *tx_data, int tx_len, uint8_t root[32])
{
    double_sha256(tx_data, tx_len, root);
}

/* Serialize full block: header (80) + varint(1) + coinbase tx */
static int serialize_block(uint8_t *out, const uint8_t header[80],
                           const uint8_t *cb_data, int cb_len)
{
    uint8_t *p = out;
    memcpy(p, header, 80); p += 80;
    p += write_varint(p, 1);  /* 1 transaction */
    memcpy(p, cb_data, cb_len); p += cb_len;
    return (int)(p - out);
}

/*
 * Convert compact bits to target as 8 x uint32 matching the GPU kernel's
 * hash/target layout.
 *
 * The kernel stores hashes as: hash[w] = bswap32(sha256_state[w])
 * The kernel comparison does: bswap32(hash[w]) vs bswap32(target[w])
 * from w=7 down to w=0, matching Go's types.Hash.LessOrEqual.
 *
 * bswap32(hash[w]) recovers the Go types.Hash LE uint32 word.
 * So bswap32(target[w]) must equal the Go types.Hash LE uint32 word,
 * i.e., target[w] = bswap32(go_hash_le_word[w]).
 *
 * Go's CompactToHash stores the target in LE byte order (byte 0 = LSB).
 * We replicate that here, then bswap each word for the kernel.
 */
static void bits_to_target(uint32_t bits, uint32_t target[8])
{
    /* Build target in Go's types.Hash byte layout (LE, byte 0 = LSB) */
    uint8_t le[32];
    memset(le, 0, 32);

    uint32_t mantissa = bits & 0x007FFFFF;
    uint32_t exponent = bits >> 24;

    /*
     * compact value = mantissa * 2^(8*(exponent-3))
     * In LE byte array, mantissa LSB goes at byte_offset = exponent - 3,
     * mantissa middle byte at exponent - 2, mantissa MSB at exponent - 1.
     */
    if (exponent >= 3) {
        int base = exponent - 3;
        if (base < 32)     le[base]     = mantissa & 0xFF;
        if (base + 1 < 32) le[base + 1] = (mantissa >> 8) & 0xFF;
        if (base + 2 < 32) le[base + 2] = (mantissa >> 16) & 0xFF;
    } else {
        mantissa >>= 8 * (3 - exponent);
        le[0] = mantissa & 0xFF;
    }

    /*
     * Store as LE uint32 words — same layout as the kernel's final_hash.
     * Direct uint32 comparison from word 7 down to word 0 then matches
     * Go's types.Hash.LessOrEqual byte-by-byte comparison.
     */
    for (int w = 0; w < 8; w++) {
        target[w] = ((uint32_t)le[w*4])           |
                    ((uint32_t)le[w*4+1] << 8)    |
                    ((uint32_t)le[w*4+2] << 16)   |
                    ((uint32_t)le[w*4+3] << 24);
    }
}

/* ── Main ───────────────────────────────────────────────────────── */

int main(int argc, char **argv)
{
    const char *rpc_base = "http://127.0.0.1:19335";
    int num_workers = 0;
    int hashes_per_batch = 4;
    int honest_timestamps = 0;

    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "--rpc") == 0 && i+1 < argc) rpc_base = argv[++i];
        else if (strcmp(argv[i], "--workers") == 0 && i+1 < argc) num_workers = atoi(argv[++i]);
        else if (strcmp(argv[i], "--hpi") == 0 && i+1 < argc) hashes_per_batch = atoi(argv[++i]);
        else if (strcmp(argv[i], "--honest") == 0) honest_timestamps = 1;
        else { fprintf(stderr, "Usage: %s [--rpc URL] [--workers N] [--hpi N] [--honest]\n", argv[0]); return 1; }
    }

    signal(SIGINT, sighandler);
    signal(SIGTERM, sighandler);
    curl_global_init(CURL_GLOBAL_DEFAULT);

    const size_t MEM_PER_WORKER = 4194304UL * 32;

    /* ── OpenCL setup ─────────────────────────────────────────── */
    cl_platform_id platform;
    cl_device_id device;
    cl_int err;

    CL_CHECK(clGetPlatformIDs(1, &platform, NULL), "get platform");
    CL_CHECK(clGetDeviceIDs(platform, CL_DEVICE_TYPE_GPU, 1, &device, NULL), "get device");

    char dev_name[256];
    size_t dev_gmem;
    clGetDeviceInfo(device, CL_DEVICE_NAME, sizeof(dev_name), dev_name, NULL);
    clGetDeviceInfo(device, CL_DEVICE_GLOBAL_MEM_SIZE, sizeof(dev_gmem), &dev_gmem, NULL);

    if (num_workers <= 0) {
        num_workers = (int)((dev_gmem * 0.80) / MEM_PER_WORKER);
        if (num_workers < 1) num_workers = 1;
    }

    size_t total_vram = (size_t)num_workers * MEM_PER_WORKER;

    printf("sha256mem GPU Miner — Fairchain\n");
    printf("  GPU:       %s\n", dev_name);
    printf("  VRAM:      %lu / %lu MiB\n",
           (unsigned long)(total_vram/(1024*1024)),
           (unsigned long)(dev_gmem/(1024*1024)));
    printf("  Workers:   %d\n", num_workers);
    printf("  HPI:       %d\n", hashes_per_batch);
    printf("  RPC:       %s\n", rpc_base);
    printf("  Timestamp: %s\n\n", honest_timestamps ? "honest (wall clock)" : "manipulated (parent+1)");

    cl_context ctx = clCreateContext(NULL, 1, &device, NULL, NULL, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create context: %d\n", err); return 1; }
    cl_command_queue queue = clCreateCommandQueue(ctx, device, 0, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create queue: %d\n", err); return 1; }

    size_t src_len;
    char *src = load_kernel_source("sha256mem_gpu_fast.cl", &src_len);
    cl_program prog = clCreateProgramWithSource(ctx, 1, (const char **)&src, &src_len, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create program: %d\n", err); return 1; }

    printf("Compiling kernel...\n");
    err = clBuildProgram(prog, 1, &device, "-cl-mad-enable -cl-fast-relaxed-math -cl-std=CL1.2", NULL, NULL);
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

    /* ── Allocate GPU buffers ─────────────────────────────────── */
    cl_mem buf_midstate = clCreateBuffer(ctx, CL_MEM_READ_ONLY, 8*sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc midstate");
    cl_mem buf_tail = clCreateBuffer(ctx, CL_MEM_READ_ONLY, 4*sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc tail");
    cl_mem buf_mem = clCreateBuffer(ctx, CL_MEM_READ_WRITE, total_vram, NULL, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "VRAM alloc failed: %s\n", cl_err_str(err)); return 1; }
    cl_mem buf_counts = clCreateBuffer(ctx, CL_MEM_READ_WRITE, num_workers*sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc counts");
    cl_mem buf_found = clCreateBuffer(ctx, CL_MEM_READ_WRITE, sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc found");
    cl_mem buf_nonce = clCreateBuffer(ctx, CL_MEM_READ_WRITE, sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc nonce");
    cl_mem buf_hash = clCreateBuffer(ctx, CL_MEM_READ_WRITE, 8*sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc hash");
    cl_mem buf_target = clCreateBuffer(ctx, CL_MEM_READ_ONLY, 8*sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc target");

    cl_kernel kernel = clCreateKernel(prog, "sha256mem_mine_fast", &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create kernel: %d (%s)\n", err, cl_err_str(err)); return 1; }

    CL_CHECK(clSetKernelArg(kernel, 0, sizeof(cl_mem), &buf_midstate), "arg 0");
    CL_CHECK(clSetKernelArg(kernel, 1, sizeof(cl_mem), &buf_tail), "arg 1");
    CL_CHECK(clSetKernelArg(kernel, 2, sizeof(cl_mem), &buf_mem), "arg 2");
    CL_CHECK(clSetKernelArg(kernel, 3, sizeof(cl_mem), &buf_counts), "arg 3");
    CL_CHECK(clSetKernelArg(kernel, 4, sizeof(cl_mem), &buf_found), "arg 4");
    CL_CHECK(clSetKernelArg(kernel, 5, sizeof(cl_mem), &buf_nonce), "arg 5");
    CL_CHECK(clSetKernelArg(kernel, 6, sizeof(cl_mem), &buf_hash), "arg 6");
    CL_CHECK(clSetKernelArg(kernel, 7, sizeof(cl_mem), &buf_target), "arg 7");

    uint32_t hpi = (uint32_t)hashes_per_batch;
    CL_CHECK(clSetKernelArg(kernel, 9, sizeof(uint32_t), &hpi), "arg 9");

    size_t global_size = (size_t)num_workers;
    size_t local_size = 1;

    uint32_t *hash_counts_host = calloc(num_workers, sizeof(uint32_t));
    uint64_t total_blocks_mined = 0;
    time_t start_time = time(NULL);

    printf("GPU miner ready. Mining...\n\n");

    /* ── Mining loop ──────────────────────────────────────────── */
    while (g_running) {
        char url[512];

        /* Fetch block template (gives correct next-block bits) */
        snprintf(url, sizeof(url), "%s/getblocktemplate", rpc_base);
        char *tmpl_json = http_get(url);
        if (!tmpl_json) {
            fprintf(stderr, "getblocktemplate failed, retrying in 2s...\n");
            sleep(2); continue;
        }

        char height_str[32], bits_str[32], prevhash_str[128];
        char coinbasevalue_str[32], curtime_str[32];
        json_get_string(tmpl_json, "height", height_str, sizeof(height_str));
        json_get_string(tmpl_json, "bits", bits_str, sizeof(bits_str));
        json_get_string(tmpl_json, "previousblockhash", prevhash_str, sizeof(prevhash_str));
        json_get_string(tmpl_json, "coinbasevalue", coinbasevalue_str, sizeof(coinbasevalue_str));
        json_get_string(tmpl_json, "curtime", curtime_str, sizeof(curtime_str));
        free(tmpl_json);

        uint32_t new_height = (uint32_t)atoi(height_str);
        uint32_t bits;
        sscanf(bits_str, "%x", &bits);
        uint64_t coinbasevalue = (uint64_t)atoll(coinbasevalue_str);
        uint32_t template_time = (uint32_t)atol(curtime_str);

        /* Determine block timestamp */
        uint32_t block_timestamp;
        if (honest_timestamps) {
            block_timestamp = template_time;
        } else {
            /* Fetch tip block for parent timestamp (manipulation mode) */
            snprintf(url, sizeof(url), "%s/getblockbyheight?height=%u", rpc_base, new_height - 1);
            char *tip_json = http_get(url);
            if (!tip_json) { fprintf(stderr, "Fetch tip failed\n"); sleep(1); continue; }
            char tip_time_str[32];
            json_get_string(tip_json, "time", tip_time_str, sizeof(tip_time_str));
            free(tip_json);
            block_timestamp = (uint32_t)atol(tip_time_str) + 1;
        }

        /* Build coinbase */
        uint8_t cb_data[256];
        int cb_len = build_coinbase(cb_data, new_height, coinbasevalue);

        /* Compute merkle root (single tx) */
        uint8_t merkle_root[32];
        compute_merkle_root(cb_data, cb_len, merkle_root);

        /* Build 80-byte header */
        uint8_t header[80];
        memset(header, 0, 80);
        write_le32(header + 0, 1);  /* version */
        hex_to_bytes_reverse(prevhash_str, header + 4, 32);  /* prevblock */
        memcpy(header + 36, merkle_root, 32);  /* merkle root */
        write_le32(header + 68, block_timestamp);
        write_le32(header + 72, bits);
        /* nonce at header+76, set to 0 — GPU will iterate */

        /* Compute midstate */
        uint32_t midstate[8], tail[4];
        compute_midstate(header, midstate, tail);

        /* Compute target from bits */
        uint32_t target[8];
        bits_to_target(bits, target);

        printf("Height %u  bits=0x%08x  ts=%u (%s)\n",
               new_height, bits, block_timestamp,
               honest_timestamps ? "honest" : "parent+1");
        printf("  target (LE words): ");
        for (int i = 0; i < 8; i++) printf("[%d]=0x%08x ", i, target[i]);
        printf("\n  target (bytes):    ");
        for (int i = 0; i < 32; i++) printf("%02x", ((uint8_t*)target)[i]);
        printf("\n");

        /* Upload to GPU */
        CL_CHECK(clEnqueueWriteBuffer(queue, buf_midstate, CL_TRUE, 0, 8*sizeof(uint32_t), midstate, 0, NULL, NULL), "upload midstate");
        CL_CHECK(clEnqueueWriteBuffer(queue, buf_tail, CL_TRUE, 0, 4*sizeof(uint32_t), tail, 0, NULL, NULL), "upload tail");
        CL_CHECK(clEnqueueWriteBuffer(queue, buf_target, CL_TRUE, 0, 8*sizeof(uint32_t), target, 0, NULL, NULL), "upload target");

        uint32_t nonce_offset = 0;
        int found = 0;
        uint32_t winning_nonce = 0;
        uint64_t block_hashes = 0;
        struct timespec block_start, now;
        clock_gettime(CLOCK_MONOTONIC, &block_start);

        while (g_running && !found) {
            /* Reset found flag and counts */
            uint32_t zero = 0;
            CL_CHECK(clEnqueueWriteBuffer(queue, buf_found, CL_TRUE, 0, sizeof(uint32_t), &zero, 0, NULL, NULL), "reset found");
            memset(hash_counts_host, 0, num_workers * sizeof(uint32_t));
            CL_CHECK(clEnqueueWriteBuffer(queue, buf_counts, CL_TRUE, 0, num_workers*sizeof(uint32_t), hash_counts_host, 0, NULL, NULL), "reset counts");

            CL_CHECK(clSetKernelArg(kernel, 8, sizeof(uint32_t), &nonce_offset), "arg 8");

            CL_CHECK(clEnqueueNDRangeKernel(queue, kernel, 1, NULL, &global_size, &local_size, 0, NULL, NULL), "enqueue");
            CL_CHECK(clFinish(queue), "finish");

            /* Read results */
            uint32_t found_flag = 0;
            CL_CHECK(clEnqueueReadBuffer(queue, buf_found, CL_TRUE, 0, sizeof(uint32_t), &found_flag, 0, NULL, NULL), "read found");

            CL_CHECK(clEnqueueReadBuffer(queue, buf_counts, CL_TRUE, 0, num_workers*sizeof(uint32_t), hash_counts_host, 0, NULL, NULL), "read counts");
            uint64_t batch_hashes = 0;
            for (int i = 0; i < num_workers; i++) batch_hashes += hash_counts_host[i];
            block_hashes += batch_hashes;

            if (found_flag) {
                CL_CHECK(clEnqueueReadBuffer(queue, buf_nonce, CL_TRUE, 0, sizeof(uint32_t), &winning_nonce, 0, NULL, NULL), "read nonce");
                found = 1;
            } else {
                nonce_offset += num_workers * hashes_per_batch;

                clock_gettime(CLOCK_MONOTONIC, &now);
                double dt = (now.tv_sec - block_start.tv_sec) + (now.tv_nsec - block_start.tv_nsec)/1e9;
                double rate = block_hashes / dt;
                printf("  ... %lu hashes, %.1f H/s, %.0fs, nonce_offset=%u\n",
                       (unsigned long)block_hashes, rate, dt, nonce_offset);

                /* Check for stale tip */
                snprintf(url, sizeof(url), "%s/getblockchaininfo", rpc_base);
                char *check = http_get(url);
                if (check) {
                    char new_hash[128];
                    if (json_get_string(check, "bestblockhash", new_hash, sizeof(new_hash)) == 0) {
                        if (strlen(new_hash) > 0 && strcmp(new_hash, prevhash_str) != 0) {
                            printf("  TIP CHANGED — restarting template\n");
                            free(check);
                            break;
                        }
                    }
                    free(check);
                }
            }
        }

        if (!found) continue;

        clock_gettime(CLOCK_MONOTONIC, &now);
        double dt = (now.tv_sec - block_start.tv_sec) + (now.tv_nsec - block_start.tv_nsec)/1e9;

        printf("  FOUND! nonce=%u  hashes=%lu  time=%.1fs  rate=%.1f H/s\n",
               winning_nonce, (unsigned long)block_hashes, dt, block_hashes/dt);

        /* Set nonce in header */
        write_le32(header + 76, winning_nonce);

        /* Serialize full block */
        uint8_t block_data[1024];
        int block_len = serialize_block(block_data, header, cb_data, cb_len);

        /* Compute block hash for display */
        uint8_t block_hash[32];
        double_sha256(header, 80, block_hash);

        printf("  Submitting block...\n");
        snprintf(url, sizeof(url), "%s/submitblock", rpc_base);
        char *resp = http_post_binary(url, block_data, block_len);
        if (resp) {
            if (strstr(resp, "accepted") || strstr(resp, "null") || strlen(resp) < 5) {
                total_blocks_mined++;
                printf("  ACCEPTED! hash=");
                for (int i = 31; i >= 0; i--) printf("%02x", block_hash[i]);
                printf("\n  height=%u  total_mined=%lu  uptime=%lds\n\n",
                       new_height, (unsigned long)total_blocks_mined,
                       (long)(time(NULL) - start_time));
            } else {
                printf("  REJECTED: %s\n\n", resp);
            }
            free(resp);
        } else {
            printf("  Submit failed (network error)\n\n");
        }
    }

    printf("\nGPU miner stopped. Mined %lu blocks in %lds.\n",
           (unsigned long)total_blocks_mined, (long)(time(NULL) - start_time));

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
    clReleaseContext(ctx);
    free(hash_counts_host);
    curl_global_cleanup();

    return 0;
}
