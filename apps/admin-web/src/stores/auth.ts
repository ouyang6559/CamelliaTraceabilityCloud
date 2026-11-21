import { create } from 'zustand'

export interface AuthUser {
  id: string
  username: string
  display_name: string
  roles: string[]
  platform_admin?: boolean
}

export interface AuthTenant {
  id: string
  name: string
  code: string
  status?: string
}

interface AuthState {
  user: AuthUser | null
  tenant: AuthTenant | null
  locale: string
  setLocale: (locale: string) => void
  login: (user: AuthUser, accessToken: string, refreshToken: string, tenant?: AuthTenant | null) => void
  logout: () => void
}

const savedTenant = (): AuthTenant | null => {
  try {
    return JSON.parse(localStorage.getItem('ctc.tenant') || 'null')
  } catch {
    return null
  }
}

export const useAuth = create<AuthState>((set) => ({
  user: JSON.parse(localStorage.getItem('ctc.user') || 'null'),
  tenant: savedTenant(),
  locale: localStorage.getItem('ctc.locale') || 'zh-CN',
  setLocale: (locale) => {
    localStorage.setItem('ctc.locale', locale)
    set({ locale })
  },
  login: (user, accessToken, refreshToken, tenant) => {
    localStorage.setItem('ctc.access_token', accessToken)
    localStorage.setItem('ctc.refresh_token', refreshToken)
    localStorage.setItem('ctc.user', JSON.stringify(user))
    if (tenant) localStorage.setItem('ctc.tenant', JSON.stringify(tenant))
    set({ user, tenant: tenant ?? null })
  },
  logout: () => {
    localStorage.removeItem('ctc.access_token')
    localStorage.removeItem('ctc.refresh_token')
    localStorage.removeItem('ctc.user')
    localStorage.removeItem('ctc.tenant')
    set({ user: null, tenant: null })
  },
}))
