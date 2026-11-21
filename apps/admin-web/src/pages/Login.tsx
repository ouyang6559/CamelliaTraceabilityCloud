import { useState } from 'react'
import { Button, Card, Form, Input, message, Modal, Select, Typography } from 'antd'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, apiError } from '../api/client'
import { useAuth } from '../stores/auth'

interface LoginResponse {
  user: { id: string; username: string; display_name: string; roles: string[]; platform_admin?: boolean }
  tokens: { access_token: string; refresh_token: string }
  tenant?: { id: string; name: string; code: string }
}

interface TenantOption {
  code: string
  name: string
}

export default function Login() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const login = useAuth((s) => s.login)
  const [loading, setLoading] = useState(false)
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [tenantOptions, setTenantOptions] = useState<TenantOption[] | null>(null)
  const [pending, setPending] = useState<{ username: string; password: string } | null>(null)
  const [regForm] = Form.useForm()

  async function doLogin(values: { username: string; password: string; tenant_code?: string }) {
    setLoading(true)
    try {
      const res = await api.post<LoginResponse>('/auth/login', values)
      login(res.data.user, res.data.tokens.access_token, res.data.tokens.refresh_token, res.data.tenant ?? undefined)
      navigate('/')
    } finally {
      setLoading(false)
    }
  }

  async function handleLogin(values: { username: string; password: string; tenant_code?: string }) {
    try {
      await doLogin(values)
    } catch (err) {
      const anyErr = err as { response?: { status?: number; data?: { tenants?: TenantOption[] } } }
      if (anyErr.response?.status === 409 && anyErr.response.data?.tenants?.length) {
        // MULTI_TENANT_AMBIGUOUS: same username in several tenants — pick one.
        setPending({ username: values.username, password: values.password })
        setTenantOptions(anyErr.response.data.tenants)
        return
      }
      message.error(t('login.error') + ' — ' + apiError(err))
    }
  }

  async function handleAmbiguousPick(code: string) {
    if (!pending) return
    await doLogin({ ...pending, tenant_code: code })
    setTenantOptions(null)
    setPending(null)
  }

  async function handleRegister(values: {
    tenant_name: string; username: string; password: string; email?: string; display_name?: string
  }) {
    setLoading(true)
    try {
      const res = await api.post<LoginResponse>('/auth/register', values)
      login(res.data.user, res.data.tokens.access_token, res.data.tokens.refresh_token, res.data.tenant)
      message.success(t('register.success'))
      navigate('/')
    } catch (err) {
      message.error(apiError(err))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{
      minHeight: '100vh',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      background: 'linear-gradient(135deg, #e8f5e9 0%, #f1f8e9 50%, #ffffff 100%)',
    }}>
      <Card style={{ width: 420, boxShadow: '0 8px 32px rgba(46,125,50,.12)' }}>
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4, marginBottom: 8 }}>
          <img src="/logo.svg" alt="CTC logo" style={{ width: 64, height: 64, borderRadius: 14 }} />
          <Typography.Title level={3} style={{ margin: '8px 0 0' }}>
            {t('app.name')}
          </Typography.Title>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            CamelliaTraceabilityCloud · {t('app.subtitle')}
          </Typography.Text>
        </div>
        <Typography.Paragraph type="secondary" style={{ textAlign: 'center' }}>
          {t('login.welcome')}
        </Typography.Paragraph>
        {mode === 'login' ? (
          <>
            <Form layout="vertical" onFinish={handleLogin} initialValues={{ username: 'admin', password: 'admin123' }}>
              <Form.Item name="username" label={t('login.username')} rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="password" label={t('login.password')} rules={[{ required: true }]}>
                <Input.Password />
              </Form.Item>
              <Form.Item name="tenant_code" label={t('login.tenantCode')} extra={t('login.tenantCodeHint')}>
                <Input placeholder="demo" />
              </Form.Item>
              <Button type="primary" htmlType="submit" block loading={loading}>
                {t('login.submit')}
              </Button>
            </Form>
            <Typography.Text type="secondary" style={{ display: 'block', marginTop: 12, textAlign: 'center', fontSize: 12 }}>
              {t('login.hint')}
            </Typography.Text>
            <Typography.Link
              style={{ display: 'block', textAlign: 'center', marginTop: 8, fontSize: 13 }}
              onClick={() => setMode('register')}
            >
              {t('register.switch')}
            </Typography.Link>
          </>
        ) : (
          <>
            <Form layout="vertical" form={regForm} onFinish={handleRegister}>
              <Form.Item name="tenant_name" label={t('register.tenantName')} rules={[{ required: true }]}>
                <Input placeholder={t('register.tenantNamePh')} />
              </Form.Item>
              <Form.Item name="username" label={t('login.username')} rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="password" label={t('login.password')} rules={[{ required: true, min: 6 }]}>
                <Input.Password />
              </Form.Item>
              <Form.Item name="email" label="Email">
                <Input />
              </Form.Item>
              <Button type="primary" htmlType="submit" block loading={loading}>
                {t('register.submit')}
              </Button>
            </Form>
            <Typography.Link
              style={{ display: 'block', textAlign: 'center', marginTop: 12, fontSize: 13 }}
              onClick={() => setMode('login')}
            >
              {t('register.back')}
            </Typography.Link>
          </>
        )}
      </Card>

      <Modal
        open={tenantOptions !== null}
        title={t('login.pickTenant')}
        closable={false}
        footer={null}
      >
        <Typography.Paragraph>{t('login.pickTenantHint')}</Typography.Paragraph>
        <Select
          style={{ width: '100%' }}
          placeholder={t('login.pickTenant')}
          onChange={(code) => handleAmbiguousPick(code)}
          options={(tenantOptions ?? []).map((o) => ({ value: o.code, label: `${o.name} (${o.code})` }))}
        />
      </Modal>
    </div>
  )
}
