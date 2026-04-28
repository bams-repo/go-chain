import { ExecuteRPC } from "../../wailsjs/go/main/App";

/** Call in-process JSON-RPC (same path as the debug console). */
export async function walletRpc<T = unknown>(method: string, params: unknown[] = []): Promise<T> {
  const resp = await ExecuteRPC(method, JSON.stringify(params));
  if (resp.error) {
    throw new Error(String(resp.error));
  }
  const raw = resp.result;
  if (raw == null || raw === "") {
    return undefined as T;
  }
  if (typeof raw === "string") {
    const s = raw.trim();
    if (s === "" || s === "null") {
      return undefined as T;
    }
    try {
      return JSON.parse(s) as T;
    } catch {
      return raw as T;
    }
  }
  return raw as T;
}
