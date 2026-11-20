import { useState } from 'react'
import { Button, Card, Form, Input, message, Modal, Popconfirm, Select, Space, Table, Tag } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, deleteJSON, getJSON, listPage, patchJSON, postJSON } from '../api/client'

interface RoleRow { id: string; code: string; name: string; builtin: boolean }

/** 用户管理：多角色分配 + 密码重置。 */
export default function Users() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [form] = Form.useForm()
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<Record<string, unknown> | null>(null)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')

  const query = useQuery({
    queryKey: ['users', page, search],
    queryFn: () => {
      const params: Record<string, unknown> = { page, page_size: 20 }
      if (search) params.search = search
      return listPage<Record<string, unknown>>('/users', params)
    },
  })

  const roles = useQuery({
    queryKey: ['roles'],
    queryFn: () => getJSON<{ items: RoleRow[] }>('/roles'),
  })

  async function save() {
    const values = await form.validateFields()
    try {
      if (editing) {
        await patchJSON(`/users/${editing.id}`, values)
      } else {
        await postJSON('/users', values)
      }
      message.success(t('common.success'))
      setModalOpen(false)
      setEditing(null)
      void qc.invalidateQueries({ queryKey: ['users'] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  const columns: ColumnsType<Record<string, unknown>> = [
    { key: 'username', title: '用户名', dataIndex: 'username' },
    { key: 'display_name', title: '姓名', dataIndex: 'display_name' },
    { key: 'roles', title: t('nav.roles'), dataIndex: 'roles',
      render: (roles: string[]) => (roles ?? []).map((r) => <Tag key={r}>{r}</Tag>) },
    { key: 'status', title: t('common.status'), dataIndex: 'status',
      render: (v: string) => <Tag color={v === 'active' ? 'green' : 'red'}>{t(`status.${v}`)}</Tag> },
    { key: 'last_login_at', title: '最后登录', dataIndex: 'last_login_at',
      render: (v: string) => v?.slice(0, 19).replace('T', ' ') ?? '-' },
    { key: 'actions', title: t('common.actions'), width: 150, render: (_, row) => (
      <Space>
        <Button size="small" onClick={() => { setEditing(row); setModalOpen(true) }}>{t('common.edit')}</Button>
        <Popconfirm title={t('common.confirmDelete')} onConfirm={() => {
          void (async () => {
            try {
              await deleteJSON(`/users/${row.id}`)
              void qc.invalidateQueries({ queryKey: ['users'] })
            } catch (err) { message.error(apiError(err)) }
          })()
        }}>
          <Button size="small" danger>{t('common.delete')}</Button>
        </Popconfirm>
      </Space>
    ) },
  ]

  return (
    <Card className="page-card">
      <div className="toolbar">
        <Input.Search placeholder={t('common.search')} style={{ width: 220 }} onSearch={setSearch} allowClear />
        <Button icon={<ReloadOutlined />} onClick={() => void query.refetch()}>{t('common.refresh')}</Button>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditing(null); setModalOpen(true) }}>
          {t('common.create')}
        </Button>
      </div>
      <Table
        rowKey="id"
        columns={columns}
        dataSource={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={{ current: page, pageSize: 20, total: query.data?.total ?? 0, onChange: (p) => setPage(p) }}
      />
      <Modal
        open={modalOpen}
        title={editing ? `${t('common.edit')} · ${t('nav.users')}` : `${t('common.create')} · ${t('nav.users')}`}
        onCancel={() => { setModalOpen(false); setEditing(null) }}
        onOk={save}
        destroyOnClose
      >
        <Form form={form} layout="vertical">
          <Form.Item name="username" label={t('login.username')} rules={[{ required: !editing }]}>
            <Input disabled={!!editing} />
          </Form.Item>
          <Form.Item
            name="password"
            label={t('login.password')}
            rules={[{ required: !editing, min: 6, message: '≥ 6 chars' }]}
          >
            <Input.Password placeholder={editing ? '留空不修改密码' : ''} />
          </Form.Item>
          <Form.Item name="display_name" label="姓名">
            <Input />
          </Form.Item>
          <Form.Item name="email" label="Email">
            <Input />
          </Form.Item>
          <Form.Item name="phone" label="电话">
            <Input />
          </Form.Item>
          <Form.Item name="roles" label={t('nav.roles')}>
            <Select
              mode="multiple"
              placeholder="选择角色"
              options={(roles.data?.items ?? []).map((r) => ({ value: r.code, label: `${r.name} (${r.code})` }))}
            />
          </Form.Item>
          <Form.Item name="status" label={t('common.status')} initialValue="active">
            <Select options={[{ value: 'active', label: t('status.active') }, { value: 'disabled', label: '禁用' }]} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
