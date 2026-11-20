import { useState } from 'react'
import { Button, Card, Form, Input, message, Modal, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, deleteJSON, getJSON, listPage, patchJSON, postJSON } from '../api/client'

interface RoleRow {
  id: string; code: string; name: string; description: string
  builtin: boolean; permissions: string[]; user_count: number
}

/** 角色管理：内置角色只读 + 自定义角色权限配置。 */
export default function Roles() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [form] = Form.useForm()
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<RoleRow | null>(null)

  const query = useQuery({
    queryKey: ['roles'],
    queryFn: () => listPage<RoleRow>('/roles'),
  })

  const catalog = useQuery({
    queryKey: ['role-permissions'],
    queryFn: () => getJSON<{ code: string; resource: string; action: string }[]>('/roles/permissions'),
  })

  async function save() {
    const values = await form.validateFields()
    try {
      if (editing) {
        await patchJSON(`/roles/${editing.id}`, values)
      } else {
        await postJSON('/roles', values)
      }
      message.success(t('common.success'))
      setModalOpen(false)
      setEditing(null)
      void qc.invalidateQueries({ queryKey: ['roles'] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  const columns: ColumnsType<RoleRow> = [
    { key: 'code', title: 'Code', dataIndex: 'code', render: (v: string, row) => (
      <Space direction="vertical" size={0}>
        <span><Tag color={row.builtin ? 'green' : 'blue'}>{v}</Tag></span>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>{row.name}</Typography.Text>
      </Space>
    ) },
    { key: 'permissions', title: '权限', dataIndex: 'permissions', render: (perms: string[]) => {
      if ((perms ?? []).length > 12) {
        return <Tag color="gold">{perms.length} permissions</Tag>
      }
      return (
        <Space wrap size={2}>
          {(perms ?? []).slice(0, 8).map((p) => <Tag key={p} style={{ fontSize: 11 }}>{p}</Tag>)}
          {(perms?.length ?? 0) > 8 && <Tag>+{perms.length - 8}</Tag>}
        </Space>
      )
    } },
    { key: 'user_count', title: '用户数', dataIndex: 'user_count', width: 90 },
    { key: 'actions', title: t('common.actions'), width: 150, render: (_, row) => (
      <Space>
        {!row.builtin && <Button size="small" onClick={() => { setEditing(row); setModalOpen(true) }}>{t('common.edit')}</Button>}
        {!row.builtin && (
          <Popconfirm title={t('common.confirmDelete')} onConfirm={() => {
            void (async () => {
              try {
                await deleteJSON(`/roles/${row.id}`)
                void qc.invalidateQueries({ queryKey: ['roles'] })
              } catch (err) { message.error(apiError(err)) }
            })()
          }}>
            <Button size="small" danger>{t('common.delete')}</Button>
          </Popconfirm>
        )}
      </Space>
    ) },
  ]

  const grouped = (catalog.data ?? []).reduce<Record<string, { code: string; action: string }[]>>((acc, p) => {
    ;(acc[p.resource] = acc[p.resource] ?? []).push({ code: p.code, action: p.action })
    return acc
  }, {})

  return (
    <Card className="page-card">
      <div className="toolbar">
        <Button icon={<ReloadOutlined />} onClick={() => void query.refetch()}>{t('common.refresh')}</Button>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditing(null); setModalOpen(true) }}>
          {t('common.create')}
        </Button>
        <Typography.Text type="secondary">内置角色（tenant_admin/operator/viewer）不可修改</Typography.Text>
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
        title={editing ? `${t('common.edit')} · ${t('nav.roles')}` : `${t('common.create')} · ${t('nav.roles')}`}
        width={640}
        onCancel={() => { setModalOpen(false); setEditing(null) }}
        onOk={save}
        destroyOnClose
      >
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item name="code" label="Code" rules={[{ required: !editing }]}>
            <Input disabled={!!editing} placeholder="如 agronomist" />
          </Form.Item>
          <Form.Item name="name" label={t('common.name')} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label={t('common.description')}>
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="permissions" label="权限（选择多个，或全选）">
            <Select
              mode="multiple"
              allowClear
              showSearch
              placeholder="选择权限；支持 * 通配由后端检查"
              options={Object.entries(grouped).map(([res, perms]) => ({
                label: res,
                options: perms.map((p) => ({ value: p.code, label: p.code })),
              }))}
              style={{ maxHeight: 300 }}
            />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
