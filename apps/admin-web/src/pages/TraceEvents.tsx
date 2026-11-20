import { useMemo, useState } from 'react'
import { Alert, Button, Card, Input, message, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import { SafetyOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, getJSON, listPage, postJSON } from '../api/client'

const EVENT_TYPES = [
  'planting', 'fertilize', 'spray', 'irrigate', 'weed', 'prune', 'inspect',
  'uav_mission', 'robot_mission', 'harvest', 'warehouse_in', 'processing',
  'quality_test', 'packing', 'logistics', 'warehouse_out', 'note',
]

interface VerifyResult { valid: boolean; events?: number; break_at?: { event_id: string; sequence: number } }

/** 溯源事件：全链路事件流 + 哈希链完整性验证（specs/05）。 */
export default function TraceEvents() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [eventType, setEventType] = useState('')
  const [refType, setRefType] = useState('')
  const [refId, setRefId] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [detail, setDetail] = useState<Record<string, unknown> | null>(null)
  const [verifyResult, setVerifyResult] = useState<VerifyResult | null>(null)

  const params = useMemo(() => {
    const p: Record<string, unknown> = { page, page_size: 20 }
    if (eventType) p.event_type = eventType
    if (refType) p.ref_type = refType
    if (refId) p.ref_id = refId
    if (search) p.search = search
    return p
  }, [page, eventType, refType, refId, search])

  const query = useQuery({
    queryKey: ['trace-events', params],
    queryFn: () => listPage<Record<string, unknown>>('/trace-events', params),
  })

  async function verifyChain() {
    try {
      const res = await postJSON<VerifyResult>('/trace-events/verify-chain')
      setVerifyResult(res)
      if (res.valid) message.success(t('trace.chainValid'))
      else message.error(t('trace.chainBroken', { seq: res.break_at?.sequence }))
    } catch (err) {
      message.error(apiError(err))
    } finally {
      void qc.invalidateQueries({ queryKey: ['trace-events'] })
    }
  }

  const columns: ColumnsType<Record<string, unknown>> = [
    { key: 'sequence', title: '#', dataIndex: 'sequence', width: 70 },
    { key: 'event_type', title: '类型', dataIndex: 'event_type', width: 130,
      render: (v: string) => <Tag color="green">{v}</Tag> },
    { key: 'title', title: '事件', dataIndex: 'title', render: (v: string, row) => (
      <a onClick={() => setDetail(row)}>{v || String(row.event_type)}</a>
    ) },
    { key: 'ref_type', title: '关联对象', width: 200,
      render: (_, row) => `${String(row.ref_type ?? '')} ${String(row.ref_id ?? '').slice(0, 12)}` },
    { key: 'operator_id', title: '操作人', width: 120, render: (v: unknown) => String(v ?? '-').slice(0, 10) },
    { key: 'event_time', title: '时间', dataIndex: 'event_time', width: 170,
      render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
    { key: 'hash', title: 'Hash', width: 150, render: (v: string) =>
      <span className="mono">{String(v ?? '').slice(0, 14)}…</span> },
  ]

  return (
    <Card className="page-card">
      <div className="toolbar">
        <Input.Search placeholder={t('common.search')} style={{ width: 200 }} onSearch={setSearch} allowClear />
        <Select allowClear placeholder="事件类型" style={{ width: 150 }}
          options={EVENT_TYPES.map((v) => ({ value: v, label: v }))}
          onChange={(v) => { setEventType(v ?? ''); setPage(1) }} />
        <Select allowClear placeholder="关联类型" style={{ width: 170 }}
          options={['crop_batch', 'harvest', 'raw_material_batch', 'processing_batch',
            'product_batch', 'package', 'task', 'mission'].map((v) => ({ value: v, label: v }))}
          onChange={(v) => { setRefType(v ?? ''); setPage(1) }} />
        <Input placeholder="对象 ID" style={{ width: 220 }} onChange={(e) => setRefId(e.target.value)} allowClear />
        <Button icon={<SafetyOutlined />} onClick={verifyChain}>{t('trace.verifyChain')}</Button>
      </div>

      {verifyResult && (
        <Alert
          style={{ marginBottom: 12 }}
          type={verifyResult.valid ? 'success' : 'error'}
          showIcon
          message={verifyResult.valid
            ? `${t('trace.chainValid')} · ${verifyResult.events} events`
            : `${t('trace.chainBroken', { seq: verifyResult.break_at?.sequence })}`}
        />
      )}

      <Table
        rowKey="id"
        columns={columns}
        dataSource={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={{ current: page, pageSize: 20, total: query.data?.total ?? 0, onChange: (p) => setPage(p) }}
      />

      <Modal open={detail !== null} title={t('common.detail')} footer={null} width={760}
        onCancel={() => setDetail(null)}>
        {detail && (
          <div>
            <p>
              <Tag color="green">{String(detail.event_type)}</Tag>
              <b>{String(detail.title ?? '')}</b>
            </p>
            <p>{String(detail.description ?? '')}</p>
            <Typography.Paragraph>
              关联：{String(detail.ref_type ?? '-')} · {String(detail.ref_id ?? '-')}
              <br />操作人：{String(detail.operator_id ?? '-')}
              <br />时间：{String(detail.event_time ?? '').slice(0, 19).replace('T', ' ')}
            </Typography.Paragraph>
            {detail.location ? <p className="mono">📍 {JSON.stringify(detail.location)}</p> : null}
            <Typography.Title level={5}>Hash Chain</Typography.Title>
            <p className="mono">sequence: {String(detail.sequence)}<br />prev: {String(detail.prev_hash)}<br />hash: {String(detail.hash)}</p>
            {detail.inputs ? (
              <>
                <Typography.Title level={5}>投入品</Typography.Title>
                <pre className="mono" style={{ whiteSpace: 'pre-wrap' }}>{JSON.stringify(detail.inputs, null, 2)}</pre>
              </>
            ) : null}
            {detail.metadata ? (
              <>
                <Typography.Title level={5}>Metadata</Typography.Title>
                <pre className="mono" style={{ whiteSpace: 'pre-wrap' }}>{JSON.stringify(detail.metadata, null, 2)}</pre>
              </>
            ) : null}
          </div>
        )}
      </Modal>
    </Card>
  )
}
