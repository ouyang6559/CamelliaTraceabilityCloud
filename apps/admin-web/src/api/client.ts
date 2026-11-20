import axios, { AxiosError } from 'axios'

export const api = axios.create({
  baseURL: '/api/v1',
  timeout: 30_000,
})

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('ctc.access_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

let refreshing: Promise<string | null> | null = null

async function tryRefresh(): Promise<string | null> {
  const refresh = localStorage.getItem('ctc.refresh_token')
  if (!refresh) return null
  try {
    const res = await axios.post('/api/v1/auth/refresh', { refresh_token: refresh })
    const { access_token, refresh_token } = res.data.tokens
    localStorage.setItem('ctc.access_token', access_token)
    localStorage.setItem('ctc.refresh_token', refresh_token)
    return access_token as string
  } catch {
    return null
  }
}

api.interceptors.response.use(
  (res) => res,
  async (err: AxiosError) => {
    const original = err.config as (typeof err.config & { _retried?: boolean }) | undefined
    if (err.response?.status === 401 && original && !original._retried) {
      original._retried = true
      refreshing = refreshing ?? tryRefresh()
      const token = await refreshing
      refreshing = null
      if (token) {
        original.headers.Authorization = `Bearer ${token}`
        return api.request(original)
      }
      clearSession()
      window.location.hash = '#/login'
      window.location.reload()
    }
    return Promise.reject(err)
  },
)

export function clearSession() {
  localStorage.removeItem('ctc.access_token')
  localStorage.removeItem('ctc.refresh_token')
  localStorage.removeItem('ctc.user')
}

export function apiError(err: unknown): string {
  if (err instanceof AxiosError) {
    const data = err.response?.data as { message?: string; code?: string } | undefined
    if (data?.message) return data.message
    return err.message
  }
  return String(err)
}

// ---------- typed helpers ----------

export interface Page<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export async function listPage<T>(path: string, params: Record<string, unknown> = {}): Promise<Page<T>> {
  const res = await api.get<Page<T>>(path, { params })
  return res.data
}

export async function getJSON<T>(path: string): Promise<T> {
  const res = await api.get<T>(path)
  return res.data
}

export async function postJSON<T>(path: string, body?: unknown): Promise<T> {
  const res = await api.post<T>(path, body)
  return res.data
}

export async function patchJSON<T>(path: string, body?: unknown): Promise<T> {
  const res = await api.patch<T>(path, body)
  return res.data
}

export async function deleteJSON(path: string): Promise<void> {
  await api.delete(path)
}

export async function uploadFile(file: File): Promise<{ id: string; sha256: string }> {
  const form = new FormData()
  form.append('file', file)
  const res = await api.post('/files', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return res.data
}
