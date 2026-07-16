import { getStoredToken } from "./authStorage";

export const webSocketBearerSubprotocol = "cloudops-bearer";

export function webSocketConnectionOptions(url: string, token: string | null): { url: string; protocols?: string[] } {
  return token
    ? { url, protocols: [webSocketBearerSubprotocol, token] }
    : { url };
}

export function openAuthenticatedWebSocket(url: string): WebSocket {
  const options = webSocketConnectionOptions(url, getStoredToken());
  return options.protocols ? new WebSocket(options.url, options.protocols) : new WebSocket(options.url);
}
