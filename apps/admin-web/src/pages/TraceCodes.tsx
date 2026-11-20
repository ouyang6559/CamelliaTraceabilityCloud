import { useState } from 'react'
import { Button, Card, Input, message, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import { QrcodeOutlined } from '@ant-design/icons'
import { QRCodeSVG } from 'qrcode.react'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, listPage, postJSON } from '../api/client'

/** 一物一码：生成/管理溯源码，扫码链接指向 trace-web 公共页。 */
export default function TraceCodes() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [form, setForm] = useState({ ref_type: 'package', ref_id: '', code_type: 'consumer', product_batch_id: '' })
  const [creating, setCreating] = useState(false)
  const [qrCode, setQrCode] = useState<{ code: string; url: string } | null>(null)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')

  const query = useQuery({
    queryKey: ['trace-codes', page, search],
    queryFn: () => {
      const params: Record<string, unknown> = { page, page_size: 20 }
      if (search) params.search = search
      return listPage<Record<string, unknown>>('/trace-codes', params)
    },
  })

  const traceBase = window.location.protocol + '//' + window.location.hostname + ':8081/t/'

  async function generate() {
    if (!form.ref_id) {
      message.error('需要对象 ID')
      return
    }
    setCreating(true)
    try {
      const body: Record<string, unknown> = {
        ref_type: form.ref_type,
        ref_id: form.ref_id,
        code_type: form.code_type,
      }
      if (form.product_batch_id) body.product_batch_id = form.product_batch_id
      const res = await postJSON<{ items: { code: string }[] }>('/trace-codes', body)
      const code = res.items[0]?.code
      if (code) setQrCode({ code, url: traceBase + code })
      message.success(t('common.success'))
      void qc.invalidateQueries({ queryKey: ['trace-codes'] })
    } catch (err) {
      message.error(apiError(err))
    } finally {
      setCreating(false)
    }
  }

  const columns: ColumnsType<Record<string, unknown>> = [
    { key: 'code', title: '码', dataIndex: 'code', render: (v: string, row) => (
      <a onClick={() => setQrCode({ code: v, url: traceBase + v })}>
        <QrcodeOutlined /> <span className="mono">{v}</span>
      </a>
    ) },
    { key: 'code_type', title: '类型', dataIndex: 'code_type', width: 100 },
    { key: 'ref_type', title: '对象', width: 200,
      render: (_, row) => `${String(row.ref_type ?? '')} ${String(row.ref_id ?? '').slice(0, 12)}` },
    { key: 'status', title: t('common.status'), dataIndex: 'status', width: 90,
      render: (v: string) => <Tag color={v === 'active' ? 'green' : 'red'}>{t(`status.${v}`)}</Tag> },
    { key: 'scan_count', title: t('trace.scanCount'), dataIndex: 'scan_count', width: 100 },
    { key: 'last_scanned_at', title: '最近扫码', dataIndex: 'last_scanned_at', width: 170,
      render: (v: string) => v?.slice(0, 19).replace('T', ' ') ?? '-' },
    { key: 'created_at', title: t('common.createdAt'), dataIndex: 'created_at', width: 170,
      render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
  ]

  return (
    <Card className="page-card">
      <div className="toolbar">
        <Input.Search placeholder="搜索码" style={{ width: 220 }} onSearch={setSearch} allowClear />
        <Typography.Text type="secondary">
          扫码链接前缀：<span className="mono">{traceBase}</span>
        </Typography.Text>
      </div>

      <Card size="small" style={{ marginBottom: 16, background: '#fafafa' }}>
        <Space wrap>
          <Select
            value={form.ref_type} style={{ width: 180 }}
            options={['package', 'product_batch', 'crop_batch', 'batch'].map((v) => ({ value: v, label: v }))}
            onChange={(v) => setForm({ ...form, ref_type: v })}
          />
          <Input placeholder="对象 ID" style={{ width: 260 }}
            value={form.ref_id} onChange={(e) => setForm({ ...form, ref_id: e.target.value })} />
          <Select
            value={form.code_type} style={{ width: 140 }}
            options={['consumer', 'package', 'batch', 'carton', 'pallet', 'logistics'].map((v) => ({ value: v, label: v }))}
            onChange={(v) => setForm({ ...form, code_type: v })}
          />
          <Button type="primary" icon={<QrcodeOutlined />} loading={creating} onClick={generate}>
            {t('trace.generateQR')}
          </Button>
        </Space>
      </Card>

      <Table
        rowKey="id"
        columns={columns}
        dataSource={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={{ current: page, pageSize: 20, total: query.data?.total ?? 0, onChange: (p) => setPage(p) }}
      />

      <Modal open={qrCode !== null} title={t('trace.generateQR')} footer={null}
        onCancel={() => setQrCode(null)} width={420}>
        {qrCode && (
          <div className="qr-box">
            <QRCodeSVG value={qrCode.url} size={220} />
            <Typography.Text copyable={{ text: qrCode.url }} className="mono">{qrCode.code}</Typography.Text>
            <Typography.Text type="secondary">{qrCode.url}</Typography.Text>
          </div>
        )}
      </Modal>
    </Card>
  )
}
