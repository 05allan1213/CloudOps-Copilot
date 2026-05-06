import { deleteApiData, getApiData, postApiData } from "./client";
import type { AuthUser, LoginResponse } from "../types";

export interface LoginRequest {
  username: string;
  password: string;
}

export async function login(request: LoginRequest): Promise<LoginResponse> {
  return await postApiData<LoginResponse, LoginRequest>("/api/v1/auth/login", request);
}

export async function fetchCurrentUser(): Promise<AuthUser> {
  return await getApiData<AuthUser>("/api/v1/auth/me");
}

export interface RegisterRequest {
  username: string;
  password: string;
  role: string;
}

export async function register(request: RegisterRequest): Promise<AuthUser> {
  return await postApiData<AuthUser, RegisterRequest>("/api/v1/auth/register", request);
}

export async function fetchUsers(): Promise<AuthUser[]> {
  return await getApiData<AuthUser[]>("/api/v1/users");
}

export async function deleteUser(id: number): Promise<void> {
  await deleteApiData(`/api/v1/users/${id}`);
}
