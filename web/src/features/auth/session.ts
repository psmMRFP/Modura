import { createContext, useContext } from "react";

type Session = { access: string; replace: (access: string) => void };
export const SessionContext = createContext<Session | null>(null);
export function useConsumerSession() {
  const session = useContext(SessionContext);
  if (!session) throw new Error("ConsumerSession is required");
  return session;
}
export function consumerCSRF() {
  if (typeof document === "undefined") return "";
  const value = document.cookie
    .split("; ")
    .find((part) => part.startsWith("wheretolive_csrf="));
  try {
    return value
      ? decodeURIComponent(value.slice("wheretolive_csrf=".length))
      : "";
  } catch {
    return "";
  }
}
