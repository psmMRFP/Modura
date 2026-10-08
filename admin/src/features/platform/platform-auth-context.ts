import { createContext, useContext } from "react";
import type { PlatformLoginRequest } from "../../api/generated/wheretolive";

export type PlatformSessionStatus = "loading" | "authenticated" | "anonymous";
export interface PlatformSession {
  status: PlatformSessionStatus;
  csrfToken: string;
  fetchOptions: RequestInit;
  login(request: PlatformLoginRequest): Promise<void>;
  logout(): Promise<void>;
}
export const PlatformAuthContext = createContext<PlatformSession | null>(null);
export function usePlatformAuth() {
  const value = useContext(PlatformAuthContext);
  if (!value) throw new Error("PlatformAuthProvider is missing");
  return value;
}
