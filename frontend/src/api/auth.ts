import type { SessionResponse } from "../types";
import { getJSON } from "./client";

export function fetchSession(signal?: AbortSignal): Promise<SessionResponse> {
  return getJSON<SessionResponse>("/api/v3/session/csrf", { signal });
}

export function oauthSignOutURL(): string {
  const returnTo = `${window.location.origin}/`;
  return `/oauth2/sign_out?rd=${encodeURIComponent(returnTo)}`;
}
