export interface WalletTransaction {
  txid: string;
  vout: number;
  address: string;
  category: "receive" | "generate" | "immature" | "send";
  amount: number;
  confirmations: number;
  blockheight: number;
  isCoinbase: boolean;
  maturityProgress: number;
  maturityTarget: number;
  maturityStatus: "mempool" | "unverified" | "verified";
}
