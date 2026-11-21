import { useEffect } from 'react'
import { Button, Card, Descriptions, Form, Input, message, Tag } from 'antd'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { api, apiError } from '../api/client'
import { useAuth } from '../stores/auth'

/** 租户信息：查看当前租户 + 更新名称（tenant:update 权限）。 */
export default function TenantInfo() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const tenant = useAuth((s) => s.tenant)
  const user = useAuth((s) => s.user)
  const login = useAuth((s) => s.login)
  const [form] = Form.useForm()

  useEffect(() => {
    // Refresh tenant info from /auth/me on mount.
    api
      .get('/auth/me')
      .then((res) => {
        login(res.data.user, localStorage.getItem('ctc.access_token') || '', localStorage.getItem('ctc.refresh_token') || '', res.data.tenant)
        form.setFieldsValue({ name: res.data.tenant?.name, code: res.data.tenant?.code })
      })
      .catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function save(values: { name: string }) {
    try {
      await api.patch('/tenants/current', { name: values.name })
      message.success(t('common.success'))
      void qc.invalidateQueries({ queryKey: ['dashboard-summary'] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  return (
    <Card className="page-card" title={t('nav.tenant')}>
      <Descriptions column={1} bordered size="small">
        <Descriptions.Item label="tenant_code">
          <span className="mono">{tenant?.code ?? '-'}</span>
        </Descriptions.Item>
        <Descriptions.Item label="tenant_id">
          <span className="mono">{tenant?.id ?? '-'}</span>
        </Descriptions.Item>
        <Descriptions.Item label={t('common.status')}>
          <Tag color={tenant?.status === 'active' ? 'green' : 'red'}>{tenant?.status ?? '-'}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label={t('nav.users')}>{user?.display_name}（{user?.username}）</Descriptions.Item>
      </Descriptions>

      <Form form={form} layout="vertical" style={{ maxWidth: 420, marginTop: 20 }} onFinish={save}>
        <Form.Item name="name" label={t('common.name')} rules={[{ required: true }]}>
          <Input />
        </Form.Item>
        <Button type="primary" htmlType="submit">{t('common.save')}</Button>
      </Form>
    </Card>
  )
}
