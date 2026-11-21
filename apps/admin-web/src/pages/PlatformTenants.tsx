import { useState } from 'react'
import {
  Button, Card, Form, Input, message, Modal, Popconfirm, Select, Space, Table, Tag, Typography,
} from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, getJSON, patchJSON, postJSON } from '../api/client'
import { useAuth } from '../stores/auth'

interface TenantRow {
  id: string
  name: string
  code: string
  status: string
  created_at: string
  users: number
  farms: number
  devices: number
}

/**
 * 平台管理（仅平台管理员可见，docs/adr/adr-006）：租户列表 / 创建 / 停用启用 / 重置管理员密码。
 */
export default function PlatformTenants() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [form] = Form.useForm()
  const [modalOpen, setModalOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [creating, setCreating] = useState(false)

  const query = useQuery({
    queryKey: ['platform-tenants', search, statusFilter],
    queryFn: () => {
      const params: Record<string, unknown> = {}
      if (search) params.search = search
      if (statusFilter) params.status = statusFilter
      return getJSON<{ items: TenantRow[] }>('/platform/tenants', { params } as never)
    },
  })

  async function create() {
    const values = await form.validateFields()
    setCreating(true)
    try {
      await postJSON('/platform/tenants', values)
      message.success(t('common.success'))
      setModalOpen(false)
      form.resetFields()
      void qc.invalidateQueries({ queryKey: ['platform-tenants'] })
    } catch (err) {
      message.error(apiError(err))
    } finally {
      setCreating(false)
    }
  }

  async function patchStatus(id: string, status: string) {
    try {
      await patchJSON(`/platform/tenants/${id}`, { status })
      message.success(t('common.success'))
      void qc.invalidateQueries({ queryKey: ['platform-tenants'] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  async function resetPassword(id: string) {
    try {
      const res = await postJSON<{ username: string }>(`/platform/tenants/${id}/reset-admin-password`, {
        new_password: 'reset-' + Math.random().toString(36).slice(2, 10),
      })
      message.success(`${t('platform.resetDone')} (${res.username})`)
    } catch (err) {
      message.error(apiError(err))
    }
  }

  const columns: ColumnsType<TenantRow> = [
    { key: 'name', title: t('common.name'), dataIndex: 'name', render: (v: string, row) => (
      <Space direction="vertical" size={0}>
        <b>{v}</b>
        <span className="mono" style={{ fontSize: 12, color: '#999' }}>{row.code}</span>
      </Space>
    ) },
    { key: 'status', title: t('common.status'), dataIndex: 'status', width: 110,
      render: (v: string) => <Tag color={v === 'active' ? 'green' : 'red'}>{v}</Tag> },
    { key: 'users', title: t('platform.users'), dataIndex: 'users', width: 90 },
    { key: 'farms', title: t('nav.farms'), dataIndex: 'farms', width: 90 },
    { key: 'devices', title: t('nav.devices'), dataIndex: 'devices', width: 90 },
    { key: 'created_at', title: t('common.createdAt'), dataIndex: 'created_at', width: 170,
      render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
    { key: 'actions', title: t('common.actions'), width: 240, render: (_, row) => (
      <Space size="small">
        {row.status === 'active' ? (
          <Popconfirm title={t('platform.suspendConfirm')} onConfirm={() => patchStatus(row.id, 'suspended')}>
            <Button size="small" danger>{t('platform.suspend')}</Button>
          </Popconfirm>
        ) : (
          <Button size="small" onClick={() => patchStatus(row.id, 'active')}>{t('platform.activate')}</Button>
        )}
        <Popconfirm title={t('platform.resetConfirm')} onConfirm={() => resetPassword(row.id)}>
          <Button size="small">{t('platform.resetPassword')}</Button>
        </Popconfirm>
      </Space>
    ) },
  ]

  return (
    <Card className="page-card">
      <div className="toolbar">
        <Input.Search placeholder={t('common.search')} style={{ width: 220 }} onSearch={setSearch} allowClear />
        <Select
          allowClear placeholder={t('common.status')} style={{ width: 140 }}
          options={[{ value: 'active', label: 'active' }, { value: 'suspended', label: 'suspended' }]}
          onChange={(v) => setStatusFilter(v ?? '')}
        />
        <Button icon={<ReloadOutlined />} onClick={() => void query.refetch()}>{t('common.refresh')}</Button>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalOpen(true)}>
          {t('platform.createTenant')}
        </Button>
        <Typography.Text type="secondary">{t('platform.note')}</Typography.Text>
      </div>
      <Table
        rowKey="id"
        columns={columns}
        dataSource={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={false}
      />

      <Modal
        open={modalOpen}
        title={t('platform.createTenant')}
        onCancel={() => setModalOpen(false)}
        onOk={create}
        confirmLoading={creating}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label={t('register.tenantName')} rules={[{ required: true }]}>
            <Input placeholder={t('register.tenantNamePh')} />
          </Form.Item>
          <Form.Item name="code" label="tenant_code" extra={t('platform.codeHint')}>
            <Input placeholder="可选，留空自动生成" />
          </Form.Item>
          <Form.Item name="admin_username" label={t('login.username')} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="admin_password" label={t('login.password')} rules={[{ required: true, min: 6 }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="admin_email" label="Email">
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
