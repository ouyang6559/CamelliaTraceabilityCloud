import { useEffect, useRef, useState } from 'react'
import { Alert, Badge, Card, List, Space, Tag, Typography } from 'antd'
import { ThunderboltOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { subscribeStream } from '../api/stream'

interface LiveEvent {
  key: string
  event: string
  text: string
  at: string
  level: 'info' | 'warning' | 'error'
}

const eventStyle: Record<string, { label: string; color: string }> = {
  'alert.created': { label: '告警', color: 'red' },
  'mission.updated': { label: '任务', color: 'green' },
  'mission.started': { label: '任务下发', color: 'green' },
  'device.telemetry': { label: '遥测', color: 'blue' },
  'device.status': { label: '设备状态', color: 'blue' },
  'remote-sensing.updated': { label: '遥感', color: 'geekblue' },
  'remote-sensing.stac': { label: '遥感资产', color: 'geekblue' },
  'sync.updated': { label: '同步', color: 'purple' },
  'proof.created': { label: '存证', color: 'gold' },
  'agri.task.updated': { label: '农事', color: 'green' },
  'trace.created': { label: '溯源', color: 'green' },
}

/** 实时事件流（specs/09.10 Realtime，ADR-010 补全）：SSE 订阅租户域事件，
 *  显示 live feed；告警事件弹出置顶提醒。 */
export default function LiveEvents() {
  const { t } = useTranslation()
  const [events, setEvents] = useState<LiveEvent[]>([])
  const [connected, setConnected] = useState(false)
  const [latestAlert, setLatestAlert] = useState<string | null>(null)
  const seq = useRef(0)

  useEffect(() => {
    const off = subscribeStream((event, data) => {
      setConnected(true)
      const style = eventStyle[event] ?? { label: event, color: 'default' }
      let text = style.label
      if (event === 'alert.created') {
        text = `${data.title ?? t('nav.alerts')}`
        setLatestAlert(text)
      } else if (event === 'mission.updated' || event === 'mission.started') {
        text = `${data.message ?? data.status ?? ''} ${data.mission_id ? `#${String(data.mission_id).slice(0, 8)}` : ''}`.trim()
      } else if (event === 'device.telemetry') {
        text = `${data.device_code ?? ''} ${Object.keys((data.metrics as Record<string, unknown>) ?? {}).join(' ')}`
      } else if (event === 'device.status') {
        text = `${data.device_code ?? ''} → ${data.status ?? ''}`
      } else if (event === 'remote-sensing.updated') {
        text = `${data.status ?? ''} ${data.progress != null ? `${data.progress}%` : ''}`.trim()
      } else if (event === 'sync.updated') {
        text = `${data.status ?? ''} ${data.error ?? ''}`.trim()
      } else if (event === 'trace.created' || event === 'proof.created') {
        text = `${data.event_type ?? data.ref_type ?? ''} ${data.ref_id ? String(data.ref_id).slice(0, 8) : ''}`.trim()
      } else if (event === 'agri.task.updated') {
        text = `${data.status ?? ''}`
      }
      seq.current += 1
      setEvents((list) => [{
        key: `${Date.now()}-${seq.current}`,
        event,
        text,
        at: new Date().toLocaleTimeString(),
        level: (event === 'alert.created' ? 'warning' : 'info') as LiveEvent['level'],
      }, ...list].slice(0, 30))
    })
    return off
  }, [t])

  return (
    <Card
      size="small"
      title={
        <Space>
          <ThunderboltOutlined />
          {t('dashboard.liveEvents')}
          <Badge status={connected ? 'success' : 'error'} text={connected ? 'SSE' : 'reconnecting'} />
        </Space>
      }
    >
      {latestAlert && (
        <Alert
          type="warning" showIcon closable style={{ marginBottom: 10 }}
          message={`${t('dashboard.newAlert')}: ${latestAlert}`}
          onClose={() => setLatestAlert(null)}
        />
      )}
      <List
        size="small"
        dataSource={events}
        locale={{ emptyText: t('dashboard.liveEmpty') }}
        renderItem={(e) => (
          <List.Item style={{ padding: '4px 0' }}>
            <Tag color={eventStyle[e.event]?.color ?? 'default'} style={{ fontSize: 11 }}>
              {eventStyle[e.event]?.label ?? e.event}
            </Tag>
            <Typography.Text style={{ fontSize: 12, flex: 1 }} ellipsis>{e.text}</Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>{e.at}</Typography.Text>
          </List.Item>
        )}
      />
    </Card>
  )
}
