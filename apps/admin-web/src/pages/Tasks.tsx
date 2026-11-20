import { useState } from 'react'
import { Button, Card, Form, Input, message, Modal, Popconfirm, Select, Space, Table, Tag, Timeline } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, deleteJSON, getJSON, listPage, postJSON } from '../api/client'

const TASK_TYPES = [
  { value: 'tillage', label: '整地' },
  { value: 'fertilize', label: '施肥' },
  { value: 'prune', label: '修剪' },
  { value: 'spray', label: '施药' },
  { value: 'irrigate', label: '灌溉' },
  { value: 'weed', label: '除草' },
  { value: 'inspect', label: '巡田' },
  { value: 'harvest', label: '采收' },
  { value: 'other', label: '其他' },
]

const statusColor: Record<string, string> = {
  planned: 'blue', in_progress: 'orange', completed: 'green', cancelled: 'default',
}

/** 农事任务：创建/流转/完成（完成即生成 TraceEvent，specs/02.3）。 */
export default function Tasks() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [form] = Form.useForm()
  const [modalOpen, setModalOpen] = useState(false)
  const [recordTarget, setRecordTarget] = useState<string | null>(null)
  const [recordForm] = Form.useForm()
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)

  const params: Record<string, unknown> = { page, page_size: 20 }
  if (statusFilter) params.status = statusFilter
  if (search) params.search = search

  const query = useQuery({
    queryKey: ['agricultural-tasks', params],
    queryFn: () => listPage<Record<string, unknown>>('/agricultural-tasks', params),
  })

  async function transition(id: string, action: 'start' | 'complete', body?: unknown) {
    try {
      await postJSON(`/agricultural-tasks/${id}/${action}`, body)
      message.success(t('common.success'))
      void qc.invalidateQueries({ queryKey: ['agricultural-tasks'] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  async function completeTask() {
    if (!recordTarget) return
    const values = await recordForm.validateFields()
    const body: Record<string, unknown> = { note: values.note || '' }
    if (values.latitude && values.longitude) {
      body.latitude = Number(values.latitude)
      body.longitude = Number(values.longitude)
    }
    await transition(recordTarget, 'complete', body)
    setRecordTarget(null)
    recordForm.resetFields()
  }

  const columns: ColumnsType<Record<string, unknown>> = [
    { key: 'title', title: '任务', dataIndex: 'title', render: (v: string, row) => (
      <Space direction="vertical" size={0}>
        <span><b>{v}</b></span>
        <span style={{ fontSize: 12, color: '#999' }}>{String(row.description ?? '')}</span>
      </Space>
    ) },
    { key: 'type', title: t('tasks.type'), dataIndex: 'type', width: 90, render: (v: string) =>
      TASK_TYPES.find((tp) => tp.value === v)?.label ?? v },
    { key: 'status', title: t('common.status'), dataIndex: 'status', width: 100, render: (v: string) =>
      <Tag color={statusColor[v] ?? 'default'}>{t(`status.${v}`)}</Tag> },
    { key: 'priority', title: t('tasks.priority'), dataIndex: 'priority', width: 90,
      render: (v: string) => <Tag color={v === 'urgent' || v === 'high' ? 'volcano' : 'default'}>{v}</Tag> },
    { key: 'planned_start_at', title: t('tasks.planned'), dataIndex: 'planned_start_at', width: 170,
      render: (v: string) => (v ? v.slice(0, 19).replace('T', ' ') : '-') },
    {
      key: 'actions', title: t('common.actions'), width: 260,
      render: (_, row) => {
        const id = String(row.id)
        const status = String(row.status)
        return (
          <Space size="small">
            {status === 'planned' && (
              <Button size="small" onClick={() => transition(id, 'start')}>{t('tasks.start')}</Button>
            )}
            {(status === 'planned' || status === 'in_progress') && (
              <Button size="small" type="primary" onClick={() => setRecordTarget(id)}>{t('tasks.complete')}</Button>
            )}
            {status !== 'completed' && status !== 'cancelled' && (
              <Popconfirm title={t('common.confirmDelete')} onConfirm={() => {
                void (async () => {
                  try {
                    await deleteJSON(`/agricultural-tasks/${id}`)
                    void qc.invalidateQueries({ queryKey: ['agricultural-tasks'] })
                  } catch (err) { message.error(apiError(err)) }
                })()
              }}>
                <Button size="small" danger>{t('common.delete')}</Button>
              </Popconfirm>
            )}
          </Space>
        )
      },
    },
  ]

  return (
    <Card className="page-card">
      <div className="toolbar">
        <Input.Search
          placeholder={t('common.search')} allowClear style={{ width: 240 }}
          onSearch={(v) => { setSearch(v); setPage(1) }}
        />
        <Select
          allowClear placeholder={t('common.status')} style={{ width: 140 }}
          options={['planned', 'in_progress', 'completed', 'cancelled'].map((v) => ({ value: v, label: t(`status.${v}`) }))}
          onChange={(v) => { setStatusFilter(v ?? ''); setPage(1) }}
        />
        <Button icon={<ReloadOutlined />} onClick={() => void query.refetch()}>{t('common.refresh')}</Button>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalOpen(true)}>
          {t('common.create')}
        </Button>
      </div>

      <Table
        rowKey="id"
        columns={columns}
        dataSource={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={{
          current: page, pageSize: 20, total: query.data?.total ?? 0,
          onChange: (p) => setPage(p),
        }}
      />

      <Modal open={modalOpen} title={`${t('common.create')} · ${t('nav.tasks')}`}
        onCancel={() => setModalOpen(false)}
        onOk={async () => {
          const values = await form.validateFields()
          try {
            await postJSON('/agricultural-tasks', values)
            message.success(t('common.success'))
            setModalOpen(false)
            form.resetFields()
            void qc.invalidateQueries({ queryKey: ['agricultural-tasks'] })
          } catch (err) {
            message.error(apiError(err))
          }
        }}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="title" label="任务标题" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="type" label={t('tasks.type')} initialValue="inspect">
            <Select options={TASK_TYPES} />
          </Form.Item>
          <Form.Item name="description" label={t('common.description')}>
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="plot_id" label="地块 ID (可选)">
            <Input />
          </Form.Item>
          <Form.Item name="crop_batch_id" label="种植批次 ID (可选)">
            <Input />
          </Form.Item>
          <Form.Item name="priority" label={t('tasks.priority')} initialValue="normal">
            <Select options={['low', 'normal', 'high', 'urgent'].map((v) => ({ value: v, label: v }))} />
          </Form.Item>
          <Form.Item name="assignee_id" label="负责人 ID (可选)">
            <Input />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={recordTarget !== null}
        title={t('tasks.complete')}
        onCancel={() => setRecordTarget(null)}
        onOk={completeTask}
        okText={t('tasks.complete')}
      >
        <Form form={recordForm} layout="vertical">
          <Form.Item name="note" label={t('tasks.note')}>
            <Input.TextArea rows={3} />
          </Form.Item>
          <Space>
            <Form.Item name="latitude" label="纬度 (GPS)">
              <Input placeholder="28.1950" style={{ width: 140 }} />
            </Form.Item>
            <Form.Item name="longitude" label="经度 (GPS)">
              <Input placeholder="113.6200" style={{ width: 140 }} />
            </Form.Item>
          </Space>
        </Form>
      </Modal>
    </Card>
  )
}

/** Detail timeline used by the drawer in detail views. */
export function TaskRecords({ taskId }: { taskId: string }) {
  const { data } = useQuery({
    queryKey: ['task-records', taskId],
    queryFn: () => getJSON<Record<string, unknown>[]>(`/agricultural-tasks/${taskId}/records`),
    enabled: !!taskId,
  })
  return (
    <Timeline
      items={(data ?? []).map((r) => ({
        children: (
          <span>
            {String(r.note || '-')} · {String(r.finished_at ?? r.created_at ?? '').slice(0, 19).replace('T', ' ')}
          </span>
        ),
      }))}
    />
  )
}
