import { type PropsWithChildren, useEffect, useMemo, useState } from "react";

import {
  changePassword as changePasswordRequest,
  login as loginRequest,
  logout as logoutRequest,
  refresh,
} from "../../api/generated/modura";
import {
  AuthContext,
  type AuthSession,
  type SessionStatus,
} from "./auth-context";

function cookie(name: string) {
  const prefix = `${encodeURIComponent(name)}=`;
  return document.cookie
    .split(";")
    .map((item) => item.trim())
    .find((item) => item.startsWith(prefix))
    ?.slice(prefix.length);
}

export function AuthProvider({ children }: PropsWithChildren) {
  const [status, setStatus] = useState<SessionStatus>(() =>
    cookie("modura_csrf") ? "loading" : "anonymous",
  );
  const [accessToken, setAccessToken] = useState("");
  const [csrfToken, setCsrfToken] = useState("");

  useEffect(() => {
    const csrf = cookie("modura_csrf");
    if (!csrf) {
      return;
    }
    void refresh({
      credentials: "include",
      headers: { "X-CSRF-Token": decodeURIComponent(csrf) },
    })
      .then((response) => {
        if (response.status === 200) {
          setAccessToken(response.data.accessToken);
          setCsrfToken(response.data.csrfToken);
          setStatus("authenticated");
        } else {
          setStatus("anonymous");
        }
      })
      .catch(() => setStatus("anonymous"));
  }, []);

  const value = useMemo<AuthSession>(
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
        const response = await loginRequest(request, {
          credentials: "include",
        });
        if (response.status !== 200) {
          throw new Error("登录失败，请检查租户、账号和密码");
        }
        setAccessToken(response.data.accessToken);
        setCsrfToken(response.data.csrfToken);
        setStatus("authenticated");
      },
      changePassword: async (request) => {
        const response = await changePasswordRequest(request, {
          credentials: "include",
          headers: {
            "X-CSRF-Token": csrfToken,
            Authorization: `Bearer ${accessToken}`,
          },
        });
        if (response.status !== 200) throw new Error("密码修改失败");
        setAccessToken(response.data.accessToken);
        setCsrfToken(response.data.csrfToken);
      },
      logout: async () => {
        if (csrfToken) {
          await logoutRequest({
            credentials: "include",
            headers: {
              "X-CSRF-Token": csrfToken,
              ...(accessToken
                ? { Authorization: `Bearer ${accessToken}` }
                : {}),
            },
          });
        }
        setAccessToken("");
        setCsrfToken("");
        setStatus("anonymous");
      },
    }),
    [accessToken, csrfToken, status],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
