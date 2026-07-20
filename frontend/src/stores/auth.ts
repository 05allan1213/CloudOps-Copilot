import { computed, ref } from "vue";
import { defineStore } from "pinia";

import { fetchSession, oauthSignOutURL } from "../api/auth";
import type { SessionActor } from "../types";

export const useAuthStore = defineStore("auth", () => {
  const actor = ref<SessionActor | null>(null);
  const csrfToken = ref("");
  const csrfExpiresAt = ref("");
  const initialized = ref(false);
  const loading = ref(false);
  const error = ref("");

  const isAuthenticated = computed(() => actor.value !== null);
  const isOperator = computed(() => actor.value?.role === "operator");

  function tokenFresh(): boolean {
    const expires = Date.parse(csrfExpiresAt.value);
    return csrfToken.value !== "" && Number.isFinite(expires) && expires - Date.now() > 30_000;
  }

  async function loadSession(force = false): Promise<void> {
    if (!force && initialized.value && tokenFresh()) return;
    loading.value = true;
    error.value = "";
    try {
      const session = await fetchSession();
      actor.value = session.actor;
      csrfToken.value = session.token;
      csrfExpiresAt.value = session.expires_at;
      initialized.value = true;
    } catch (err) {
      clearSession();
      error.value = err instanceof Error ? err.message : "Unable to establish the GitHub session";
      throw err;
    } finally {
      loading.value = false;
    }
  }

  async function commandToken(): Promise<string> {
    await loadSession(!tokenFresh());
    return csrfToken.value;
  }

  function clearSession() {
    actor.value = null;
    csrfToken.value = "";
    csrfExpiresAt.value = "";
    initialized.value = false;
  }

  function signOut() {
    clearSession();
    window.location.assign(oauthSignOutURL());
  }

  return {
    actor,
    csrfExpiresAt,
    initialized,
    loading,
    error,
    isAuthenticated,
    isOperator,
    loadSession,
    commandToken,
    clearSession,
    signOut,
  };
});
