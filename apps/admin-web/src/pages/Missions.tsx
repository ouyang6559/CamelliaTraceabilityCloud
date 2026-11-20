import { useState } from 'react'
import {
  Alert, Button, Card, Form, Input, message, Modal, Popconfirm, Select, Space,
  Steps, Table, Tag, Timeline,
} from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, getJSON, listPage, postJSON } from '../api/client'

const statusColor: Record<string, string> = {
  draft: 'default', validation_failed: 'red', safety_check: 'orange',
  awaiting_confirmation: 'gold', approved: 'cyan', executing: 'green',
  paused: 'purple', completed: 'green', failed: 'red', cancelled: 'default',
}

const MISSION_TYPES = ['spray', 'mow', 'plow', 'seed', 'fertilize', 'map', 'inspect', 'transport']

const PIPELINE = ['draft', 'safety_check', 'awaiting_confirmation', 'approved', 'executing', 'completed'] as const

/**
 * 设备任务：完整的安全流水线 UI（specs/19 第六原则）
 * 校验 -> 安全检查 -> 人工批准 -> 执行 -> 完成（自动生成 TraceEvent）。
 */
export default function Missions() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [form] = Form.useForm()
  const [modalOpen, setModalOpen] = useState(false)
  const [statusFilter, setStatusFilter] = useState('')
  const [detailId, setDetailId] = useState<string | null>(null)
  const [page, setPage] = useState(1)

  const params: Record<string, unknown> = { page, page_size: 20 }
  if (statusFilter) params.status = statusFilter

  const query = useQuery({
    queryKey: ['missions', params],
    queryFn: () => listPage<Record<string, unknown>>('/missions', params),
  })

  const detail = useQuery({
    queryKey: ['mission', detailId],
    queryFn: () => getJSON<Record<string, unknown>>(`/missions/${detailId}`),
    enabled: detailId !== null,
  })

  const detailEvents = useQuery({
    queryKey: ['mission-events', detailId],
    queryFn: () => getJSON<Record<string, unknown>[]>(`/missions/${detailId}/events`),
    enabled: detailId !== null,
  })

  async function action(id: string, act: string) {
    try {
      await postJSON(`/missions/${id}/${act}`)
      message.success(t('common.success'))
      void qc.invalidateQueries({ queryKey: ['missions'] })
      void qc.invalidateQueries({ queryKey: ['mission'] })
      void qc.invalidateQueries({ queryKey: ['mission-events'] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  function currentStep(status: string): number {
    const idx = PIPELINE.indexOf(status as typeof PIPELINE[number])
    if (idx >= 0) return idx
    if (status === 'paused') return 4
    if (['validation_failed', 'failed', 'cancelled'].includes(status)) return 0
    return 0
  }

  const columns: ColumnsType<Record<string, unknown>> = [
    { key: 'name', title: '任务', dataIndex: 'name', render: (v: string, row) => (
      <Space direction="vertical" size={0}>
        <a onClick={() => setDetailId(String(row.id))}><b>{v}</b></a>
        <span style={{ fontSize: 12, color: '#999' }}>
          {String(row.mission_type ?? '')} · {String(row.device_name ?? row.device_code ?? '')}
        </span>
      </Space>
    ) },
    { key: 'status', title: t('common.status'), dataIndex: 'status', width: 130, render: (v: string) =>
      <Tag color={statusColor[v] ?? 'default'}>{t(`status.${v}`)}</Tag> },
    { key: 'risk_level', title: t('mission.riskLevel'), dataIndex: 'risk_level', width: 90,
      render: (v: string) => <Tag color={v === 'high' ? 'red' : v === 'medium' ? 'orange' : 'green'}>{v}</Tag> },
    { key: 'progress', title: '进度', dataIndex: 'progress', width: 120,
      render: (v: unknown) => `${Number(v ?? 0).toFixed(0)}%` },
    {
      key: 'actions', title: t('common.actions'), width: 300,
      render: (_, row) => {
        const id = String(row.id)
        const status = String(row.status)
        return (
          <Space size="small" wrap>
            {(status === 'draft' || status === 'validation_failed') && (
              <Button size="small" onClick={() => action(id, 'validate')}>{t('mission.validate')}</Button>
            )}
            {status === 'safety_check' && (
              <Button size="small" onClick={() => action(id, 'safety-check')}>{t('mission.safetyCheck')}</Button>
            )}
            {status === 'awaiting_confirmation' && (
              <Popconfirm title={t('mission.approveWarning')} onConfirm={() => action(id, 'approve')}>
                <Button size="small" type="primary" danger>{t('mission.approve')}</Button>
              </Popconfirm>
            )}
            {status === 'approved' && (
              <Button size="small" type="primary" onClick={() => action(id, 'start')}>{t('mission.start')}</Button>
            )}
            {status === 'executing' && (
              <>
                <Button size="small" onClick={() => action(id, 'pause')}>{t('mission.pause')}</Button>
                <Popconfirm title="确认停止？" onConfirm={() => action(id, 'stop')}>
                  <Button size="small" danger>{t('mission.stop')}</Button>
                </Popconfirm>
              </>
            )}
            {status === 'paused' && (
              <Button size="small" onClick={() => action(id, 'resume')}>{t('mission.resume')}</Button>
            )}
            {!['completed', 'cancelled', 'failed'].includes(status) && (
              <Popconfirm title="确认取消？" onConfirm={() => action(id, 'cancel')}>
                <Button size="small" danger>{t('mission.cancel')}</Button>
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
        <Select
          allowClear placeholder={t('common.status')} style={{ width: 160 }}
          options={Object.keys(statusColor).map((v) => ({ value: v, label: t(`status.${v}`) }))}
          onChange={(v) => { setStatusFilter(v ?? ''); setPage(1) }}
        />
        <Button icon={<ReloadOutlined />} onClick={() => void query.refetch()}>{t('common.refresh')}</Button>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalOpen(true)}>
          {t('common.create')}
        </Button>
        <Alert
          type="info" showIcon style={{ flex: 1, minWidth: 260 }}
          message={t('mission.approveWarning')}
        />
      </div>

      <Table
        rowKey="id"
        columns={columns}
        dataSource={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={{ current: page, pageSize: 20, total: query.data?.total ?? 0, onChange: (p) => setPage(p) }}
      />

      <Modal open={modalOpen} title={`${t('common.create')} · ${t('nav.missions')}`} width={760}
        onCancel={() => setModalOpen(false)}
        onOk={async () => {
          const values = await form.validateFields()
          let route: unknown = []
          try {
            route = JSON.parse(values.route)
          } catch {
            message.error('航线必须是 JSON 数组')
            return
          }
          try {
            await postJSON('/missions', {
              device_id: values.device_id,
              name: values.name,
              mission_type: values.mission_type,
              route,
            })
            message.success(t('common.success'))
            setModalOpen(false)
            form.resetFields()
            void qc.invalidateQueries({ queryKey: ['missions'] })
          } catch (err) {
            message.error(apiError(err))
          }
        }}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="device_id" label="设备 ID (devices)" rules={[{ required: true }]}>
            <Input placeholder="选择在线且具备对应能力的设备" />
          </Form.Item>
          <Form.Item name="name" label="任务名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="mission_type" label="类型" initialValue="inspect" rules={[{ required: true }]}>
            <Select options={MISSION_TYPES.map((v) => ({ value: v, label: v }))} />
          </Form.Item>
          <Form.Item
            name="route" label="航线 waypoints (JSON 数组)"
            initialValue={JSON.stringify([
              { lat: 28.1905, lng: 113.616, alt: 50, action: 'takeoff' },
              { lat: 28.198, lng: 113.624, alt: 60, action: 'photo' },
              { lat: 28.196, lng: 113.6275, alt: 50, action: 'land' },
            ], null, 0)}
            rules={[{ required: true }]}
          >
            <Input.TextArea rows={6} className="mono" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal open={detailId !== null} title={t('common.detail')} width={820} footer={null}
        onCancel={() => setDetailId(null)}>
        {detail.data && (
          <>
            <Steps
              size="small" style={{ margin: '12px 0 24px' }}
              current={currentStep(String(detail.data.status))}
              items={PIPELINE.map((s) => ({ title: t(`status.${s}`) }))}
              status={String(detail.data.status) === 'validation_failed' ? 'error' : 'process'}
            />
            <p>
              <Tag color={statusColor[String(detail.data.status)]}>{t(`status.${String(detail.data.status)}`)}</Tag>
              <Tag>{t('mission.riskLevel')}: {String(detail.data.risk_level)}</Tag>
              <Tag>进度: {Number(detail.data.progress ?? 0).toFixed(0)}%</Tag>
              <Tag>设备: {String(detail.data.device_name ?? detail.data.device_id)}</Tag>
            </p>
            {detail.data.safety_check ? (
              <Card size="small" title={t('mission.safetyChecks')} style={{ marginBottom: 12 }}>
                {(detail.data.safety_check as { checks?: { name: string; passed: boolean; detail: string }[] })?.checks?.map((c, i) => (
                  <p key={i}>
                    {c.passed ? '✅' : '❌'} <b>{c.name}</b> — {c.detail}
                  </p>
                ))}
              </Card>
            ) : null}
            <Card size="small" title={t('mission.route')} style={{ marginBottom: 12 }}>
              <pre className="mono" style={{ whiteSpace: 'pre-wrap', maxHeight: 180, overflow: 'auto' }}>
                {JSON.stringify(detail.data.route, null, 2)}
              </pre>
            </Card>
            <Card size="small" title={t('mission.events')}>
              <Timeline
                items={(detailEvents.data ?? []).map((e) => ({
                  color: String(e.type) === 'failed' ? 'red' : 'blue',
                  children: (
                    <span>
                      <Tag>{String(e.type)}</Tag> {String(e.message)} · {String(e.occurred_at).slice(0, 19).replace('T', ' ')}
                    </span>
                  ),
                }))}
              />
            </Card>
          </>
        )}
      </Modal>
    </Card>
  )
}
