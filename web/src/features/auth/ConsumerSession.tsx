import { useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getGetConsumerProfileQueryKey } from "../../api/generated/places";
import { SessionContext } from "./session";
export function ConsumerSession({ children }: { children: ReactNode }) {
  const [access, setAccess] = useState("");
  const client = useQueryClient();
  function replace(token: string) {
    client.removeQueries({ queryKey: getGetConsumerProfileQueryKey() });
    setAccess(token);
  }
  return (
    <SessionContext.Provider value={{ access, replace }}>
      {children}
    </SessionContext.Provider>
  );
}
