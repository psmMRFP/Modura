import { type PropsWithChildren, useEffect, useMemo, useState } from "react";
import {
  platformLogin,
  platformLogout,
  platformRefresh,
} from "../../api/generated/wheretolive";
import {
  PlatformAuthContext,
  type PlatformSession,
  type PlatformSessionStatus,
} from "./platform-auth-context";

function cookie(name: string) {
  const prefix = `${encodeURIComponent(name)}=`;
  return document.cookie
    .split(";")
    .map((item) => item.trim())
    .find((item) => item.startsWith(prefix))
    ?.slice(prefix.length);
}

export function PlatformAuthProvider({ children }: PropsWithChildren) {
  const [status, setStatus] = useState<PlatformSessionStatus>(() =>
    cookie("wheretolive_platform_csrf") ? "loading" : "anonymous",
  );
  const [accessToken, setAccessToken] = useState("");
  const [csrfToken, setCsrfToken] = useState("");
  useEffect(() => {
    const csrf = cookie("wheretolive_platform_csrf");
    if (!csrf) return;
    void platformRefresh({
      credentials: "include",
      headers: { "X-CSRF-Token": decodeURIComponent(csrf) },
    })
      .then((response) => {
        if (response.status === 200) {
          setAccessToken(response.data.accessToken);
          setCsrfToken(response.data.csrfToken);
          setStatus("authenticated");
        } else setStatus("anonymous");
      })
      .catch(() => setStatus("anonymous"));
  }, []);
  const value = useMemo<PlatformSession>(
    () => ({
      status,
      csrfToken,
      fetchOptions: {
        credentials: "include",
        headers: accessToken
          ? { Authorization: `Bearer ${accessToken}` }
          : undefined,
      },
      login: async (request) => {
        const response = await platformLogin(request, {
          credentials: "include",
        });
        if (response.status !== 200) throw new Error("平台登录失败");
        setAccessToken(response.data.accessToken);
        setCsrfToken(response.data.csrfToken);
        setStatus("authenticated");
      },
      logout: async () => {
        try {
          if (csrfToken && accessToken) {
            await platformLogout({
              credentials: "include",
              headers: {
                Authorization: `Bearer ${accessToken}`,
                "X-CSRF-Token": csrfToken,
              },
            });
          }
        } finally {
          setAccessToken("");
          setCsrfToken("");
          setStatus("anonymous");
        }
      },
    }),
    [accessToken, csrfToken, status],
  );
  return (
    <PlatformAuthContext.Provider value={value}>
      {children}
    </PlatformAuthContext.Provider>
  );
}
