import { useState } from 'react'
import { Button, Card, Input, message, Modal, Space, Table, Tag, Typography } from 'antd'
import { QrcodeOutlined } from '@ant-design/icons'
import { QRCodeSVG } from 'qrcode.react'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, listPage, postJSON } from '../api/client'

/** 包装管理：对包装一键生成消费者溯源码。 */
export default function Packages() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [page, setPage] = useState(1)
  const [target, setTarget] = useState<{ id: string; code: string } | null>(null)
  const [qr, setQr] = useState<{ code: string; url: string } | null>(null)

  const query = useQuery({
    queryKey: ['packages', page],
    queryFn: () => listPage<Record<string, unknown>>('/packages', { page, page_size: 20 }),
  })

  const traceBase = window.location.protocol + '//' + window.location.hostname + ':8081/t/'

  async function generateCode(pkgId: string, pkgCode: string) {
    try {
      const res = await postJSON<{ items: { code: string }[] }>('/trace-codes', {
        ref_type: 'package',
        ref_id: pkgId,
        code_type: 'consumer',
      })
      const code = res.items[0]?.code
      setQr({ code, url: traceBase + code })
      message.success(t('common.success'))
      void qc.invalidateQueries({ queryKey: ['trace-codes'] })
    } catch (err) {
      message.error(apiError(err))
    }
    void pkgCode
  }

  const columns: ColumnsType<Record<string, unknown>> = [
    { key: 'code', title: t('common.code'), dataIndex: 'code' },
    { key: 'product_batch_id', title: t('nav.productBatches'), dataIndex: 'product_batch_id',
      render: (v: string) => <span className="mono">{v.slice(0, 14)}…</span> },
    { key: 'quantity', title: '数量', dataIndex: 'quantity', render: (v: unknown) => Number(v ?? 0) },
    { key: 'unit', title: '单位', dataIndex: 'unit' },
    { key: 'status', title: t('common.status'), dataIndex: 'status',
      render: (v: string) => <Tag>{t(`status.${v}`) !== `status.${v}` ? t(`status.${v}`) : v}</Tag> },
    { key: 'packed_at', title: '包装时间', dataIndex: 'packed_at',
      render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
    { key: 'actions', title: t('common.actions'), render: (_, row) => (
      <Button size="small" icon={<QrcodeOutlined />}
        onClick={() => { setTarget({ id: String(row.id), code: String(row.code) }) }}>
        {t('trace.generateQR')}
      </Button>
    ) },
  ]

  return (
    <Card className="page-card">
      <Table
        rowKey="id"
        columns={columns}
        dataSource={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={{ current: page, pageSize: 20, total: query.data?.total ?? 0, onChange: (p) => setPage(p) }}
      />

      <Modal open={target !== null} title={`${t('trace.generateQR')} · ${target?.code}`} footer={null}
        onCancel={() => { setTarget(null); setQr(null) }} width={420}>
        <Space direction="vertical" style={{ width: '100%' }} align="center">
          <p>为包装 {target?.code} 生成消费者溯源码：</p>
          <Button type="primary" icon={<QrcodeOutlined />} onClick={() => target && generateCode(target.id, target.code)}>
            生成
          </Button>
          {qr && (
            <div className="qr-box">
              <QRCodeSVG value={qr.url} size={220} />
              <Typography.Text copyable={{ text: qr.url }} className="mono">{qr.code}</Typography.Text>
              <Typography.Text type="secondary">{qr.url}</Typography.Text>
            </div>
          )}
        </Space>
      </Modal>
    </Card>
  )
}
