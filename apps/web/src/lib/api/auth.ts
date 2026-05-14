import { apiFetch } from "./core"

export const auth = {
  login: (email: string, password: string) =>
    apiFetch<{ token?: string; totp_required?: boolean; mfa_token?: string }>(
      "/api/v1/auth/login",
      { method: "POST", credentials: "include", body: JSON.stringify({ email, password }) }
    ),

  completeTOTPLogin: (mfaToken: string, code: string, trustDevice: boolean) =>
    apiFetch<{ token: string }>("/api/v1/auth/totp", {
      method: "POST",
      credentials: "include",
      body: JSON.stringify({ mfa_token: mfaToken, code, trust_device: trustDevice }),
    }),

  register: (username: string, email: string, password: string) =>
    apiFetch<{ id: string; username: string; email: string }>(
      "/api/v1/auth/register",
      { method: "POST", body: JSON.stringify({ username, email, password }) }
    ),

  getMe: (token: string) =>
    apiFetch<{ id: string; username: string; email: string; totp_enabled: boolean }>(
      "/api/v1/me", {}, token
    ),

  setupTOTP: (token: string) =>
    apiFetch<{ otp_url: string; secret: string }>(
      "/api/v1/me/totp/setup", { method: "POST" }, token
    ),

  enableTOTP: (code: string, token: string) =>
    apiFetch<void>("/api/v1/me/totp/enable", {
      method: "POST",
      body: JSON.stringify({ code }),
    }, token),

  disableTOTP: (code: string, token: string) =>
    apiFetch<void>("/api/v1/me/totp", {
      method: "DELETE",
      body: JSON.stringify({ code }),
    }, token),
}
