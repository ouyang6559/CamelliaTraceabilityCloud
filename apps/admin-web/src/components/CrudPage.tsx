import { useEffect, useMemo, useState } from 'react'
import {
  Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table, message,
} from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, deleteJSON, listPage, patchJSON, postJSON } from '../api/client'
import { useTranslation } from 'react-i18next'

export interface FieldDef {
  name: string
  label: string
  type?: 'text' | 'textarea' | 'number' | 'select' | 'password' | 'json'
  options?: { label: string; value: string }[]
  required?: boolean
  hidden?: boolean
  initialValue?: unknown
  span?: number
}

export interface CrudColumn {
  key: string
  title: string
  render?: (row: Record<string, unknown>) => React.ReactNode
}

export interface CrudPageProps {
  resourcePath: string
  perm: string
  title: string
  columns: CrudColumn[]
  fields?: FieldDef[]
  filters?: { name: string; label: string; options?: { label: string; value: string }[] }[]
  rowKey?: string
  searchPlaceholder?: string
  renderDetail?: (row: Record<string, unknown>) => React.ReactNode
  /** Optional content (e.g. a map) rendered above the table. */
  extra?: React.ReactNode
  /** Disable create/edit/delete (read-only views such as observations). */
  readOnly?: boolean
  /** Disable edit but allow create (e.g. append-only records). */
  noEdit?: boolean
}

/** Uniform list/create/edit/delete page for the generic CRUD REST contract. */
export default function CrudPage(props: CrudPageProps) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [form] = Form.useForm()
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<Record<string, unknown> | null>(null)
  const [detail, setDetail] = useState<Record<string, unknown> | null>(null)
  const [search, setSearch] = useState('')
  const [filterValues, setFilterValues] = useState<Record<string, string>>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const fields = props.fields ?? []
  const [saving, setSaving] = useState(false)

  const params = useMemo(() => {
    const p: Record<string, unknown> = { page, page_size: pageSize }
    if (search) p.search = search
    Object.entries(filterValues).forEach(([k, v]) => {
      if (v) p[k] = v
    })
    return p
  }, [page, pageSize, search, filterValues])

  const query = useQuery({
    queryKey: [props.resourcePath, params],
    queryFn: () => listPage<Record<string, unknown>>(props.resourcePath, params),
  })

  useEffect(() => {
    if (modalOpen) {
      if (editing) {
        form.setFieldsValue(editing)
      } else {
        form.resetFields()
        const defaults: Record<string, unknown> = {}
        fields.forEach((f) => {
          if (f.initialValue !== undefined) defaults[f.name] = f.initialValue
        })
        form.setFieldsValue(defaults)
      }
    }
  }, [modalOpen, editing, form, fields])

  const columns: ColumnsType<Record<string, unknown>> = props.columns.map((c) => ({
    key: c.key,
    dataIndex: c.key,
    title: c.title,
    render: (_: unknown, row) => (c.render ? c.render(row) : formatCell(row[c.key])),
  }))
  columns.push({
    key: 'actions',
    title: t('common.actions'),
    width: 180,
    render: (_, row) => (
      <Space>
        {props.renderDetail && (
          <Button size="small" onClick={() => setDetail(row)}>
            {t('common.detail')}
          </Button>
        )}
        {!props.readOnly && !props.noEdit && (
          <Button size="small" onClick={() => { setEditing(row); setModalOpen(true) }}>
            {t('common.edit')}
          </Button>
        )}
        {!props.readOnly && (
          <Popconfirm
            title={t('common.confirmDelete')}
            onConfirm={() => handleDelete(String(row.id))}
          >
            <Button size="small" danger>{t('common.delete')}</Button>
          </Popconfirm>
        )}
      </Space>
    ),
  })

  async function handleDelete(id: string) {
    try {
      await deleteJSON(`${props.resourcePath}/${id}`)
      message.success(t('common.success'))
      void qc.invalidateQueries({ queryKey: [props.resourcePath] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  async function handleSave() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      if (editing) {
        await patchJSON(`${props.resourcePath}/${editing.id}`, values)
      } else {
        await postJSON(props.resourcePath, values)
      }
      message.success(t('common.success'))
      setModalOpen(false)
      setEditing(null)
      void qc.invalidateQueries({ queryKey: [props.resourcePath] })
    } catch (err) {
      message.error(apiError(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card className="page-card">
      <div className="toolbar">
        <Input.Search
          placeholder={props.searchPlaceholder ?? t('common.search')}
          allowClear
          style={{ width: 240 }}
          onSearch={(v) => { setSearch(v); setPage(1) }}
        />
        {(props.filters ?? []).map((f) => (
          <Select
            key={f.name}
            allowClear
            placeholder={f.label}
            style={{ width: 160 }}
            options={f.options ?? []}
            onChange={(v) => {
              setFilterValues((prev) => ({ ...prev, [f.name]: v ?? '' }))
              setPage(1)
            }}
          />
        ))}
        <Button icon={<ReloadOutlined />} onClick={() => void query.refetch()}>
          {t('common.refresh')}
        </Button>
        {!props.readOnly && (
          <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditing(null); setModalOpen(true) }}>
            {t('common.create')}
          </Button>
        )}
      </div>

      {props.extra}

      <Table
        rowKey={props.rowKey ?? 'id'}
        columns={columns}
        dataSource={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={{
          current: page,
          pageSize,
          total: query.data?.total ?? 0,
          showSizeChanger: true,
          showTotal: (total) => t('common.total', { total }),
          onChange: (p, ps) => { setPage(p); setPageSize(ps) },
        }}
        scroll={{ x: true }}
      />

      <Modal
        open={modalOpen}
        title={editing ? `${t('common.edit')} · ${props.title}` : `${t('common.create')} · ${props.title}`}
        onCancel={() => { setModalOpen(false); setEditing(null) }}
        onOk={handleSave}
        confirmLoading={saving}
        width={720}
        destroyOnClose
      >
        <Form form={form} layout="vertical">
          {fields
            .filter((f) => !f.hidden)
            .map((f) => (
              <Form.Item
                key={f.name}
                name={f.name}
                label={f.label}
                rules={f.required ? [{ required: true, message: `${f.label} 必填` }] : []}
              >
                {renderField(f)}
              </Form.Item>
            ))}
        </Form>
      </Modal>

      <Modal
        open={detail !== null}
        title={t('common.detail')}
        footer={null}
        onCancel={() => setDetail(null)}
        width={720}
      >
        {detail && props.renderDetail?.(detail)}
      </Modal>
    </Card>
  )
}

function renderField(f: FieldDef) {
  switch (f.type) {
    case 'textarea':
      return <Input.TextArea rows={3} />
    case 'number':
      return <Input type="number" />
    case 'password':
      return <Input.Password />
    case 'select':
      return <Select options={f.options ?? []} />
    case 'json':
      return <Input.TextArea rows={4} placeholder='{"key": "value"}' />
    default:
      return <Input />
  }
}

export function formatCell(value: unknown): React.ReactNode {
  if (value === null || value === undefined || value === '') return '-'
  if (typeof value === 'boolean') return value ? '✅' : '—'
  if (typeof value === 'object') return <code className="mono">{JSON.stringify(value).slice(0, 60)}</code>
  const s = String(value)
  if (/^\d{4}-\d{2}-\d{2}T/.test(s)) return s.slice(0, 19).replace('T', ' ')
  return s
}
