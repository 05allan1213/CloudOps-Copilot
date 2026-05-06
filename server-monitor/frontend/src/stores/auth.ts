import { computed, ref } from "vue";
import { defineStore } from "pinia";

import { fetchCurrentUser, login as loginRequest } from "../api/auth";
import {
  clearStoredAuth,
  getStoredExpiresAt,
  getStoredToken,
  getStoredUser,
  setStoredExpiresAt,
  setStoredToken,
  setStoredUser,
} from "../api/authStorage";
import type { AuthUser } from "../types";

export const useAuthStore = defineStore("auth", () => {
  const token = ref(getStoredToken());
  const user = ref<AuthUser | null>(getStoredUser<AuthUser>());
  const expiresAt = ref(getStoredExpiresAt());
  const loading = ref(false);
  const error = ref("");

  const isAuthenticated = computed(() => token.value !== "");
  const isAdmin = computed(() => user.value?.role === "admin");

  function setSession(nextToken: string, nextUser: AuthUser, nextExpiresAt: string) {
    token.value = nextToken;
    user.value = nextUser;
    expiresAt.value = nextExpiresAt;
    setStoredToken(nextToken);
    setStoredUser(nextUser);
    setStoredExpiresAt(nextExpiresAt);
  }

  async function login(username: string, password: string) {
    loading.value = true;
    error.value = "";
    try {
      const result = await loginRequest({ username, password });
      setSession(result.token, result.user, result.expires_at);
    } catch (err) {
      error.value = err instanceof Error ? err.message : "登录失败";
      throw err;
    } finally {
      loading.value = false;
    }
  }

  async function loadCurrentUser() {
    if (!token.value) return;
    loading.value = true;
    error.value = "";
    try {
      user.value = await fetchCurrentUser();
      setStoredUser(user.value);
    } catch (err) {
      logout();
      throw err;
    } finally {
      loading.value = false;
    }
  }

  function logout() {
    token.value = "";
    user.value = null;
    expiresAt.value = "";
    clearStoredAuth();
  }

  return {
    token,
    user,
    expiresAt,
    loading,
    error,
    isAuthenticated,
    isAdmin,
    login,
    loadCurrentUser,
    logout,
  };
});
