import { useEffect, useState } from 'react'
import axios from 'axios'
import {
  Alert, Card, Col, Descriptions, Image, Result, Row, Skeleton, Space, Spin,
  Tag, Timeline, Typography, Button, message,
} from 'antd'
import { CheckCircleFilled, FileImageOutlined, SafetyOutlined } from '@ant-design/icons'
import { useParams } from 'react-router-dom'

const api = axios.create({ baseURL: '/api/v1' })

interface Summary {
  product?: { name: string; spec: string; batch_code: string }
  origin?: { farm: string; plot: string; address: string }
}
interface TimelineItem {
  event_type: string
  event_time: string
  title: string
  description: string
  quantity: number | null
  unit: string
  sequence: number
  hash: string
}
interface EvidenceItem {
  id: string
  evidence_type: string
  filename: string
  url?: string
  sha256: string
}
interface ProofItem {
  algorithm: string
  hash: string
  provider: string
  provider_tx_id: string
  confirmed_at: string
}
interface TracePayload {
  code: string
  summary: Summary
  timeline: TimelineItem[]
  evidence: EvidenceItem[]
  proofs: ProofItem[]
}

const EVENT_ICONS: Record<string, string> = {
  planting: '🌱', fertilize: '🧪', spray: '🚿', irrigate: '💧', prune: '✂️',
  inspect: '🔍', uav_mission: '🛸', robot_mission: '🤖', harvest: '🧺',
  warehouse_in: '🏭', processing: '⚙️', quality_test: '📋', packing: '📦',
  logistics: '🚚', warehouse_out: '🛒', note: '📝',
}

const EVENT_LABELS: Record<string, string> = {
  planting: '种植定植', fertilize: '施肥', spray: '施药', irrigate: '灌溉',
  prune: '修剪', inspect: '巡田', uav_mission: '无人机作业', robot_mission: '机器人作业',
  harvest: '采收', warehouse_in: '入库', processing: '加工', quality_test: '质检',
  packing: '包装', logistics: '物流', warehouse_out: '出库', note: '记录',
}

/** 消费者溯源页（specs/11.5）：无登录，只展示消费侧信息。 */
export default function TraceView() {
  const { code } = useParams<{ code: string }>()
  const [data, setData] = useState<TracePayload | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    setError(null)
    api.get<TracePayload>(`/public/trace/${code}`)
      .then((res) => setData(res.data))
      .catch((err) => setError(err.response?.data?.message ?? '加载失败'))
      .finally(() => setLoading(false))
  }, [code])

  if (loading) {
    return (
      <div style={{ maxWidth: 720, margin: '40px auto', padding: 16 }}>
        <Card><Skeleton active paragraph={{ rows: 8 }} /></Card>
      </div>
    )
  }

  if (error || !data) {
    return (
      <div style={{ maxWidth: 720, margin: '40px auto', padding: 16 }}>
        <Result
          status="404"
          title="未找到溯源记录"
          subTitle="该二维码无效或已被撤销 · The trace code is invalid or revoked."
        />
      </div>
    )
  }

  const product = data.summary?.product
  const origin = data.summary?.origin

  return (
    <div style={{ maxWidth: 720, margin: '0 auto', padding: '24px 16px 48px' }}>
      <div style={{ textAlign: 'center', marginBottom: 24 }}>
        <Typography.Title level={3} style={{ marginBottom: 4 }}>🍃 茶源智控溯源云</Typography.Title>
        <Typography.Text type="secondary">CamelliaTraceabilityCloud · 产品溯源档案</Typography.Text>
      </div>

      {product ? (
        <Card style={{ marginBottom: 16 }}>
          <Row gutter={16} align="middle">
            <Col span={16}>
              <Typography.Title level={4} style={{ marginTop: 0 }}>{product.name}</Typography.Title>
              <Typography.Text type="secondary">{product.spec}</Typography.Text>
              <p><Tag color="green">批次 {product.batch_code}</Tag></p>
            </Col>
            <Col span={8} style={{ textAlign: 'center' }}>
              <CheckCircleFilled style={{ fontSize: 44, color: '#2e7d32' }} />
              <div><Typography.Text type="secondary">溯源码已验证</Typography.Text></div>
            </Col>
          </Row>
        </Card>
      ) : null}

      {origin ? (
        <Card title="产地 Origin" size="small" style={{ marginBottom: 16 }}>
          <Descriptions column={1} size="small">
            <Descriptions.Item label="基地">{origin.farm}</Descriptions.Item>
            <Descriptions.Item label="地块">{origin.plot}</Descriptions.Item>
            <Descriptions.Item label="地址">{origin.address || '-'}</Descriptions.Item>
          </Descriptions>
        </Card>
      ) : null}

      <Card title="生产过程 Production Timeline" size="small" style={{ marginBottom: 16 }}>
        <Timeline
          items={data.timeline.map((e) => ({
            color: e.event_type === 'quality_test' ? 'green' : 'blue',
            children: (
              <div style={{ paddingBottom: 4 }}>
                <b>{EVENT_ICONS[e.event_type] ?? '•'} {e.title || EVENT_LABELS[e.event_type] || e.event_type}</b>
                <div style={{ fontSize: 12, color: '#888' }}>
                  {new Date(e.event_time).toLocaleString('zh-CN')}
                  {e.quantity != null && e.unit ? ` · ${e.quantity} ${e.unit}` : ''}
                </div>
                {e.description ? (
                  <div style={{ fontSize: 13, color: '#555' }}>{e.description}</div>
                ) : null}
              </div>
            ),
          }))}
        />
      </Card>

      {data.evidence.length > 0 ? (
        <Card title="过程影像 Evidence" size="small" style={{ marginBottom: 16 }}>
          <Space wrap>
            {data.evidence.filter((e) => e.url && e.evidence_type === 'photo').map((e) => (
              <Image key={e.id} width={120} src={e.url} fallback="data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIxMjAiIGhlaWdodD0iMTIwIj48dGV4dCB4PSI2MCIgeT0iNjUiIHRleHQtYW5jaG9yPSJtaWRkbGUiIGZvbnQtc2l6ZT0iMTQiIGZpbGw9IiM5OTkiPuWBh+aooTwvdGV4dD48L3N2Zz4=" />
            ))}
          </Space>
          <div style={{ marginTop: 8 }}>
            {data.evidence.map((e) => (
              <div key={e.id} style={{ fontSize: 12, color: '#888' }}>
                <FileImageOutlined /> {e.filename || e.evidence_type} ·
                SHA-256: <span className="mono">{e.sha256.slice(0, 16)}…</span>
              </div>
            ))}
          </div>
        </Card>
      ) : null}

      <Card
        title={<span><SafetyOutlined /> 可信验证 Verification</span>}
        size="small"
        style={{ marginBottom: 16 }}
        extra={
          <Button
            size="small"
            onClick={async () => {
              try {
                const res = await api.get(`/public/trace/${data.code}/verify`)
                message.success(res.data.valid ? '验证通过：存证一致' : '暂无存证记录')
              } catch {
                message.error('验证请求失败')
              }
            }}
          >
            重新验证
          </Button>
        }
      >
        {data.proofs.length === 0 ? (
          <Typography.Text type="secondary">该批次暂未上链存证（可由企业端发起区块链存证）</Typography.Text>
        ) : (
          data.proofs.map((p, i) => (
            <Alert
              key={i}
              type="success"
              showIcon
              style={{ marginBottom: 8 }}
              message={`${p.provider} 存证 · ${p.algorithm.toUpperCase()}`}
              description={
                <span className="mono" style={{ fontSize: 11, wordBreak: 'break-all' }}>
                  hash: {p.hash}
                  <br />tx: {p.provider_tx_id}
                  {p.confirmed_at ? <><br />time: {new Date(p.confirmed_at).toLocaleString('zh-CN')}</> : null}
                </span>
              }
            />
          ))
        )}
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          溯源数据采用逐事件哈希链（SHA-256）保护，任何篡改都会被检测。
          原始数据保存在企业自有平台，不依赖第三方。
        </Typography.Text>
      </Card>

      <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', textAlign: 'center' }}>
        溯源码：{data.code} · Powered by CamelliaTraceabilityCloud (Apache-2.0 Open Source)
      </Typography.Text>
    </div>
  )
}
