export interface IRCStatus {
  connected?: boolean;
  server?: string;
  channel?: string;
  nick?: string;
  error?: string;
  topic?: string;
  userCount?: number;
}

export interface IRCMessage {
  time?: string;
  sender?: string;
  text?: string;
  self?: boolean;
  system?: boolean;
}
