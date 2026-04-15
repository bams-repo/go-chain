export interface WalletTransaction {
  txid: string;
  vout: number;
  address: string;
  category: "receive" | "generate" | "immature";
  amount: number;
  confirmations: number;
  blockheight: number;
  isCoinbase: boolean;
  maturityProgress: number;
  maturityTarget: number;
}
