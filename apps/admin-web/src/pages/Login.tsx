import { useState } from 'react'
import { Button, Card, Form, Input, message, Typography } from 'antd'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, apiError } from '../api/client'
import { useAuth } from '../stores/auth'

interface LoginResponse {
  user: { id: string; username: string; display_name: string; roles: string[] }
  tokens: { access_token: string; refresh_token: string }
}

export default function Login() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const login = useAuth((s) => s.login)
  const [loading, setLoading] = useState(false)

  async function handleLogin(values: { username: string; password: string; tenant_code?: string }) {
    setLoading(true)
    try {
      const res = await api.post<LoginResponse>('/auth/login', values)
      login(res.data.user, res.data.tokens.access_token, res.data.tokens.refresh_token)
      navigate('/')
    } catch (err) {
      message.error(t('login.error') + ' — ' + apiError(err))
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
        <Typography.Title level={3} style={{ textAlign: 'center', marginBottom: 4 }}>
          {t('app.name')}
        </Typography.Title>
        <Typography.Paragraph type="secondary" style={{ textAlign: 'center' }}>
          {t('login.welcome')}
        </Typography.Paragraph>
        <Form layout="vertical" onFinish={handleLogin} initialValues={{ username: 'admin', password: 'admin123' }}>
          <Form.Item name="username" label={t('login.username')} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="password" label={t('login.password')} rules={[{ required: true }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="tenant_code" label={t('login.tenantCode')}>
            <Input placeholder="demo" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={loading}>
            {t('login.submit')}
          </Button>
        </Form>
        <Typography.Text type="secondary" style={{ display: 'block', marginTop: 12, textAlign: 'center', fontSize: 12 }}>
          {t('login.hint')}
        </Typography.Text>
      </Card>
    </div>
  )
}
