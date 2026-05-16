package main

import "testing"

func TestNetworkFromArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "network separate value", args: []string{"-network", "testnet"}, want: "testnet"},
		{name: "network equals value", args: []string{"-network=testnet"}, want: "testnet"},
		{name: "double dash network", args: []string{"--network=regtest"}, want: "regtest"},
		{name: "bitcoin style testnet", args: []string{"-testnet"}, want: "testnet"},
		{name: "bitcoin style mainnet", args: []string{"--mainnet"}, want: "mainnet"},
		{name: "none", args: []string{"-debug"}, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := networkFromArgs(tc.args); got != tc.want {
				t.Fatalf("networkFromArgs(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
