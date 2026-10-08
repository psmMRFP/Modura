import { createContext, useContext } from "react";

import type {
  ChangePasswordRequest,
  LoginRequest,
} from "../../api/generated/wheretolive";

export type SessionStatus = "loading" | "authenticated" | "anonymous";

export interface AuthSession {
  status: SessionStatus;
  csrfToken: string;
  fetchOptions: RequestInit;
  login: (request: LoginRequest) => Promise<void>;
  changePassword: (request: ChangePasswordRequest) => Promise<void>;
  logout: () => Promise<void>;
}

export const AuthContext = createContext<AuthSession | null>(null);

export function useAuth() {
  const session = useContext(AuthContext);
  if (!session) {
    throw new Error("AuthProvider is missing");
  }
  return session;
}
