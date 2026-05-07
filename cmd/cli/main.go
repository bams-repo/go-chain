// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/version"
)

func main() {
	rpcConnect := flag.String("rpcconnect", "127.0.0.1", "RPC server host")
	rpcPort := flag.String("rpcport", "19445", "RPC server port")
	printVer := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *printVer {
		fmt.Printf("%s CLI version v%s\n", coinparams.Name, version.String())
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	baseURL := fmt.Sprintf("http://%s:%s", *rpcConnect, *rpcPort)
	command := strings.ToLower(args[0])
	params := args[1:]

	reqSpec, err := resolveRequest(command, params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	resp, err := doRequest(baseURL, reqSpec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: Could not connect to the server %s\n", baseURL)
		fmt.Fprintf(os.Stderr, "       Is %s running?\n", coinparams.DaemonName)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		if json.Unmarshal(body, &errResp) == nil {
			if msg, ok := errResp["error"]; ok {
				fmt.Fprintf(os.Stderr, "error: %s\n", msg)
				os.Exit(1)
			}
		}
		fmt.Fprintf(os.Stderr, "error code: %d\n%s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	formatOutput(body)
}

type requestSpec struct {
	Method   string
	Endpoint string
	Form     url.Values
}

func resolveRequest(command string, params []string) (requestSpec, error) {
	post := func(endpoint string, form url.Values) (requestSpec, error) {
		return requestSpec{Method: http.MethodPost, Endpoint: endpoint, Form: form}, nil
	}
	get := func(endpoint string) (requestSpec, error) {
		return requestSpec{Method: http.MethodGet, Endpoint: endpoint}, nil
	}

	switch command {

	// --- Blockchain ---
	case "getblockchaininfo":
		return get("/getblockchaininfo")
	case "getblockcount":
		return get("/getblockcount")
	case "getbestblockhash":
		return get("/getbestblockhash")
	case "getblockhash":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("getblockhash requires <height>")
		}
		return get("/getblockhash?height=" + url.QueryEscape(params[0]))
	case "getblock":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("getblock requires <hash>")
		}
		return get("/getblock?hash=" + url.QueryEscape(params[0]))
	case "getblockbyheight":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("getblockbyheight requires <height>")
		}
		return get("/getblockbyheight?height=" + url.QueryEscape(params[0]))
	case "getdifficulty":
		return get("/getdifficulty")

	// --- Network ---
	case "getnetworkinfo":
		return get("/getnetworkinfo")
	case "getpeerinfo":
		return get("/getpeerinfo")
	case "getconnectioncount":
		return get("/getconnectioncount")
	case "addnode":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("addnode requires <ip:port>")
		}
		return post("/addnode", url.Values{"node": []string{params[0]}})
	case "disconnectnode":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("disconnectnode requires <address>")
		}
		return post("/disconnectnode", url.Values{"address": []string{params[0]}})

	// --- Mempool ---
	case "getmempoolinfo":
		return get("/getmempoolinfo")
	case "getrawmempool":
		verbose := ""
		if len(params) > 0 && params[0] == "true" {
			verbose = "?verbose=true"
		}
		return get("/getrawmempool" + verbose)
	case "getmempoolentry":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("getmempoolentry requires <txid>")
		}
		return get("/getmempoolentry?txid=" + url.QueryEscape(params[0]))

	// --- UTXO ---
	case "gettxout":
		if len(params) < 2 {
			return requestSpec{}, fmt.Errorf("gettxout requires <txid> <n>")
		}
		return get("/gettxout?txid=" + url.QueryEscape(params[0]) + "&n=" + url.QueryEscape(params[1]))
	case "gettxoutsetinfo":
		return get("/gettxoutsetinfo")

	// --- Mining ---
	case "getblocktemplate":
		return get("/getblocktemplate")
	case "getmininginfo":
		return get("/getmininginfo")
	case "getnetworkhashps":
		q := "/getnetworkhashps"
		if len(params) > 0 {
			q += "?nblocks=" + url.QueryEscape(params[0])
		}
		if len(params) > 1 {
			if strings.Contains(q, "?") {
				q += "&"
			} else {
				q += "?"
			}
			q += "height=" + url.QueryEscape(params[1])
		}
		return get(q)
	case "getrawtransaction":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("getrawtransaction requires <txid> [verbose]")
		}
		q := "/getrawtransaction?txid=" + url.QueryEscape(params[0])
		if len(params) > 1 && (params[1] == "true" || params[1] == "1") {
			q += "&verbose=true"
		}
		return get(q)
	case "submitblock":
		return requestSpec{}, fmt.Errorf("submitblock requires POST with raw binary/hex payload — use curl or JSON-RPC")

	// --- Control ---
	case "getinfo":
		return get("/getinfo")
	case "stop":
		return post("/stop", url.Values{})
	case "help":
		printUsage()
		os.Exit(0)
		return requestSpec{}, nil

	// --- Wallet ---
	case "getnewaddress":
		return post("/getnewaddress", url.Values{})
	case "getbalance":
		minconf := "1"
		if len(params) > 0 {
			minconf = params[0]
		}
		return post("/getbalance", url.Values{"minconf": []string{minconf}})
	case "listunspent":
		form := url.Values{}
		if len(params) >= 1 {
			form.Set("minconf", params[0])
		}
		if len(params) >= 2 {
			form.Set("maxconf", params[1])
		}
		return post("/listunspent", form)
	case "sendtoaddress":
		if len(params) < 2 {
			return requestSpec{}, fmt.Errorf("sendtoaddress requires <address> <amount>")
		}
		return post("/sendtoaddress", url.Values{"address": []string{params[0]}, "amount": []string{params[1]}})
	case "getwalletinfo":
		return post("/getwalletinfo", url.Values{})
	case "dumpprivkey":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("dumpprivkey requires <address>")
		}
		return post("/dumpprivkey", url.Values{"address": []string{params[0]}})
	case "importprivkey":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("importprivkey requires <privkey>")
		}
		return post("/importprivkey", url.Values{"privkey": []string{params[0]}})
	case "validateaddress":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("validateaddress requires <address>")
		}
		return post("/validateaddress", url.Values{"address": []string{params[0]}})
	case "getrawchangeaddress":
		return post("/getrawchangeaddress", url.Values{})
	case "settxfee":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("settxfee requires <amount>")
		}
		return post("/settxfee", url.Values{"amount": []string{params[0]}})
	case "sendrawtransaction":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("sendrawtransaction requires <hexstring>")
		}
		return post("/sendrawtransaction", url.Values{"hexstring": []string{params[0]}})
	case "dumpwallet":
		return post("/dumpwallet", url.Values{})
	case "signrawtransactionwithwallet":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("signrawtransactionwithwallet requires <hexstring>")
		}
		return post("/signrawtransactionwithwallet", url.Values{"hexstring": []string{params[0]}})
	case "getreceivedbyaddress":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("getreceivedbyaddress requires <address> [minconf]")
		}
		form := url.Values{"address": []string{params[0]}}
		if len(params) >= 2 {
			form.Set("minconf", params[1])
		}
		return post("/getreceivedbyaddress", form)
	case "listaddressgroupings":
		return post("/listaddressgroupings", url.Values{})
	case "backupwallet":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("backupwallet requires <destination>")
		}
		return post("/backupwallet", url.Values{"destination": []string{params[0]}})
	case "getaddressesbylabel":
		return post("/getaddressesbylabel", url.Values{})
	case "listtransactions":
		form := url.Values{}
		if len(params) > 0 {
			form.Set("count", params[0])
		}
		return post("/listtransactions", form)
	case "gettransaction":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("gettransaction requires <txid>")
		}
		return post("/gettransaction", url.Values{"txid": []string{params[0]}})
	case "encryptwallet":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("encryptwallet requires <passphrase>")
		}
		return post("/encryptwallet", url.Values{"passphrase": []string{params[0]}})
	case "walletpassphrase":
		if len(params) < 1 {
			return requestSpec{}, fmt.Errorf("walletpassphrase requires <passphrase> [timeout]")
		}
		form := url.Values{"passphrase": []string{params[0]}}
		if len(params) >= 2 {
			form.Set("timeout", params[1])
		}
		return post("/walletpassphrase", form)
	case "walletlock":
		return post("/walletlock", url.Values{})

	// --- Chain-specific ---
	case "getchainstatus":
		return get("/getchainstatus")
	case "metrics":
		return get("/metrics")

	default:
		return requestSpec{}, fmt.Errorf("unknown command: %s\nRun '%s help' for usage", command, coinparams.CLIName)
	}
}

func doRequest(baseURL string, reqSpec requestSpec) (*http.Response, error) {
	if reqSpec.Method == http.MethodPost {
		body := ""
		if reqSpec.Form != nil {
			body = reqSpec.Form.Encode()
		}
		req, err := http.NewRequest(http.MethodPost, baseURL+reqSpec.Endpoint, bytes.NewBufferString(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return http.DefaultClient.Do(req)
	}
	return http.Get(baseURL + reqSpec.Endpoint)
}

func formatOutput(body []byte) {
	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		fmt.Println(string(body))
		return
	}

	switch v := data.(type) {
	case string:
		fmt.Println(v)
	case float64:
		if v == float64(int64(v)) {
			fmt.Printf("%d\n", int64(v))
		} else {
			fmt.Printf("%v\n", v)
		}
	default:
		pretty, _ := json.MarshalIndent(data, "", "  ")
		fmt.Println(string(pretty))
	}
}

func printUsage() {
	fmt.Println(coinparams.Name + " CLI v" + version.String())
	fmt.Println()
	fmt.Println("Usage: " + coinparams.CLIName + " [options] <command> [params]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -rpcconnect=<ip>    Connect to RPC at <ip> (default: 127.0.0.1)")
	fmt.Println("  -rpcport=<port>     Connect to RPC on <port> (default: 19445)")
	fmt.Println("  -version            Print version and exit")
	fmt.Println()
	fmt.Println("Blockchain commands:")
	fmt.Println("  getblockchaininfo              Get blockchain state")
	fmt.Println("  getblockcount                  Get current block height")
	fmt.Println("  getbestblockhash               Get hash of best block")
	fmt.Println("  getblockhash <height>          Get block hash at height")
	fmt.Println("  getblock <hash>                Get block data by hash")
	fmt.Println("  getblockbyheight <height>      Get block data by height")
	fmt.Println("  getdifficulty                  Get current difficulty")
	fmt.Println()
	fmt.Println("Network commands:")
	fmt.Println("  getnetworkinfo                 Get network state")
	fmt.Println("  getpeerinfo                    Get connected peer details")
	fmt.Println("  getconnectioncount             Get number of connections")
	fmt.Println("  addnode <ip:port>              Connect to a node")
	fmt.Println("  disconnectnode <addr>          Disconnect a peer")
	fmt.Println()
	fmt.Println("Mempool commands:")
	fmt.Println("  getmempoolinfo                 Get mempool state")
	fmt.Println("  getrawmempool [true]           List mempool txids (verbose=true for details)")
	fmt.Println("  getmempoolentry <txid>         Get mempool entry for a transaction")
	fmt.Println()
	fmt.Println("UTXO commands:")
	fmt.Println("  gettxout <txid> <n>            Get unspent output")
	fmt.Println("  gettxoutsetinfo                Get UTXO set statistics")
	fmt.Println()
	fmt.Println("Mining commands:")
	fmt.Println("  getblocktemplate               Get block template (BIP 22)")
	fmt.Println("  getmininginfo                  Get mining-related information")
	fmt.Println("  getnetworkhashps [nblocks] [h] Estimated network hash rate")
	fmt.Println("  submitblock                    Submit a block (POST via curl)")
	fmt.Println()
	fmt.Println("Raw transaction commands:")
	fmt.Println("  getrawtransaction <txid> [verbose]  Get raw transaction hex")
	fmt.Println()
	fmt.Println("Wallet commands:")
	fmt.Println("  getnewaddress                  Generate a new receiving address")
	fmt.Println("  getbalance [minconf]           Get wallet balance (default minconf=1)")
	fmt.Println("  listunspent [minconf] [maxconf]  List unspent outputs")
	fmt.Println("  sendtoaddress <addr> <amount>  Send coins to an address")
	fmt.Println("  getwalletinfo                  Get wallet information")
	fmt.Println("  dumpprivkey <address>          Dump private key (WIF format)")
	fmt.Println("  importprivkey <key>            Import a private key (WIF or hex)")
	fmt.Println("  validateaddress <address>      Validate an address")
	fmt.Println("  getrawchangeaddress            Get a new change address")
	fmt.Println("  settxfee <amount>              Set transaction fee per byte")
	fmt.Println("  sendrawtransaction <hex>       Submit a raw transaction")
	fmt.Println("  signrawtransactionwithwallet <hex>  Sign a raw transaction with wallet keys")
	fmt.Println("  getreceivedbyaddress <addr> [minconf]  Total received by address")
	fmt.Println("  listaddressgroupings           List address groupings with balances")
	fmt.Println("  backupwallet <destination>     Backup wallet to file")
	fmt.Println("  getaddressesbylabel            List addresses by label")
	fmt.Println("  listtransactions [count]       List recent wallet transactions")
	fmt.Println("  gettransaction <txid>          Get transaction details")
	fmt.Println("  dumpwallet                     Dump wallet info (mnemonic, addresses)")
	fmt.Println("  encryptwallet <passphrase>     Encrypt the wallet with a passphrase")
	fmt.Println("  walletpassphrase <pass> [secs] Unlock wallet for <secs> seconds")
	fmt.Println("  walletlock                     Lock the wallet")
	fmt.Println()
	fmt.Println("Control commands:")
	fmt.Println("  getinfo                        Get node overview")
	fmt.Println("  stop                           Stop the daemon")
	fmt.Println("  help                           Show this help")
	fmt.Println()
	fmt.Println(coinparams.Name + "-specific commands:")
	fmt.Println("  getchainstatus                 Get chain status (bits, retarget, peers)")
	fmt.Println("  metrics                        Get internal metrics")
}
