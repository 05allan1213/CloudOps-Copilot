const tokenKey = "server-monitor-token";
const userKey = "server-monitor-user";
const expiresAtKey = "server-monitor-token-expires-at";

export function getStoredToken(): string {
  return localStorage.getItem(tokenKey) ?? "";
}

export function setStoredToken(token: string): void {
  localStorage.setItem(tokenKey, token);
}

export function clearStoredAuth(): void {
  localStorage.removeItem(tokenKey);
  localStorage.removeItem(userKey);
  localStorage.removeItem(expiresAtKey);
}

export function getStoredUser<T>(): T | null {
  const raw = localStorage.getItem(userKey);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return null;
  }
}

export function setStoredUser(value: unknown): void {
  localStorage.setItem(userKey, JSON.stringify(value));
}

export function getStoredExpiresAt(): string {
  return localStorage.getItem(expiresAtKey) ?? "";
}

export function setStoredExpiresAt(value: string): void {
  localStorage.setItem(expiresAtKey, value);
}
