import { defineStore } from "pinia";
import { ref } from "vue";
import axios from "axios";

const baseURL =
  import.meta.env.VITE_API_BASE_URL || "http://127.0.0.1:8081/admin/v1";
const AUTH_STORAGE_PREFIX = "linlinqi-admin";
const authTokenKey = `${AUTH_STORAGE_PREFIX}-token`;
const authProfileKey = `${AUTH_STORAGE_PREFIX}-profile`;

export interface AdminProfile {
  id?: string;
  username?: string;
  name: string;
  role: string;
  permissions: string[];
}

function normalizeProfile(value: unknown): AdminProfile {
  const input = (value || {}) as Partial<AdminProfile>;
  return {
    id: String(input.id || "") || undefined,
    username: String(input.username || "") || undefined,
    name: String(input.name || ""),
    role: String(input.role || ""),
    permissions: Array.isArray(input.permissions)
      ? [...new Set(input.permissions.map((item) => String(item).trim()))]
          .filter(Boolean)
          .sort()
      : [],
  };
}

export const useAuthStore = defineStore("auth", () => {
  const token = ref(localStorage.getItem(authTokenKey) || "");
  const storedProfile = localStorage.getItem(authProfileKey);
  let initialProfile = normalizeProfile({});
  // Persisted permissions are a first-paint cache only. Each browser page
  // session must refresh them from the authoritative admin session endpoint.
  let profileRefreshed = false;
  try {
    if (storedProfile) {
      const parsed = JSON.parse(storedProfile);
      initialProfile = normalizeProfile(parsed);
    }
  } catch {
    localStorage.removeItem(authProfileKey);
  }
  const profile = ref(initialProfile);
  let profileRequest: Promise<void> | null = null;

  function saveProfile(value: unknown, refreshedFromSession = false) {
    profile.value = normalizeProfile(value);
    if (refreshedFromSession) profileRefreshed = true;
    localStorage.setItem(authProfileKey, JSON.stringify(profile.value));
  }

  async function login(account: string, password: string, otp = "") {
    const { data } = await axios.post(`${baseURL}/auth/login`, {
      account,
      password,
      otp,
    });
    token.value = data.data.token;
    localStorage.setItem(authTokenKey, token.value);
    // A successful login response is authoritative for this page session.
    saveProfile(data.data.admin, true);
  }

  async function ensureProfile() {
    if (!token.value || profileRefreshed) return;
    if (!profileRequest) {
      profileRequest = axios
        .get(`${baseURL}/auth/session`, {
          headers: { Authorization: `Bearer ${token.value}` },
        })
        .then(({ data }) => saveProfile(data.data, true))
        .finally(() => {
          profileRequest = null;
        });
    }
    await profileRequest;
  }

  function hasPermission(required?: string) {
    if (!required) return true;
    // Persisted permissions may keep the first paint stable and provide a
    // least-privilege fallback when the session refresh has a transient 5xx or
    // network failure. They never suppress the refresh itself.
    const permissions = new Set(profile.value.permissions);
    if (permissions.has(required)) return true;
    if (required.endsWith(".view"))
      return permissions.has(`${required.slice(0, -5)}.manage`);
    return false;
  }

  function logout() {
    token.value = "";
    profile.value = normalizeProfile({});
    profileRefreshed = false;
    profileRequest = null;
    localStorage.removeItem(authTokenKey);
    localStorage.removeItem(authProfileKey);
  }
  return { token, profile, login, logout, ensureProfile, hasPermission };
});

// Admin reads are normally fast, but currency changes may resolve an FX
// snapshot and reprice the catalog in one transaction. Keep a generous
// client deadline so a committed change is not reported as a false failure.
export const adminApi = axios.create({ baseURL, timeout: 30_000 });
function targetsAdminAPI(config: { url?: string; baseURL?: string }) {
  try {
    const pageOrigin = window.location.origin;
    const apiBase = new URL(baseURL, pageOrigin);
    const requestBase = new URL(config.baseURL || baseURL, pageOrigin);
    const requestURL = new URL(config.url || "", requestBase);
    return requestURL.origin === apiBase.origin;
  } catch {
    return false;
  }
}
adminApi.interceptors.request.use((config) => {
  if (!targetsAdminAPI(config)) return config;
  const token = localStorage.getItem(authTokenKey);
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});
adminApi.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401 && targetsAdminAPI(error.config || {})) {
      localStorage.removeItem(authTokenKey);
      localStorage.removeItem(authProfileKey);
      if (window.location.pathname !== "/login")
        window.location.replace("/login");
    }
    return Promise.reject(error);
  },
);
