export interface AuthUser {
  id: number;
  username: string;
  role: "admin" | "viewer";
}

export interface LoginResponse {
  token: string;
  expires_at: string;
  user: AuthUser;
}

export interface ApiResponse<T> {
  status: string;
  data?: T;
  error?: string;
}
