import { create } from 'zustand'

export interface AuthUser {
  id: string
  username: string
  display_name: string
  roles: string[]
}

interface AuthState {
  user: AuthUser | null
  locale: string
  setLocale: (locale: string) => void
  login: (user: AuthUser, accessToken: string, refreshToken: string) => void
  logout: () => void
}

export const useAuth = create<AuthState>((set) => ({
  user: JSON.parse(localStorage.getItem('ctc.user') || 'null'),
  locale: localStorage.getItem('ctc.locale') || 'zh-CN',
  setLocale: (locale) => {
    localStorage.setItem('ctc.locale', locale)
    set({ locale })
  },
  login: (user, accessToken, refreshToken) => {
    localStorage.setItem('ctc.access_token', accessToken)
    localStorage.setItem('ctc.refresh_token', refreshToken)
    localStorage.setItem('ctc.user', JSON.stringify(user))
    set({ user })
  },
  logout: () => {
    localStorage.removeItem('ctc.access_token')
    localStorage.removeItem('ctc.refresh_token')
    localStorage.removeItem('ctc.user')
    set({ user: null })
  },
}))
