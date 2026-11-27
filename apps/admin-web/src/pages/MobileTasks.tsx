import React, { useEffect, useState } from 'react'
import {
  Alert, Button, Card, Empty, Input, List, message, Modal, Space, Tag, Typography,
} from 'antd'
import { EnvironmentOutlined, ReloadOutlined, CarryOutOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, listPage, postJSON } from '../api/client'
import { offlineEnqueue, offlineLen } from '../api/offline'

interface TaskRow {
  id: string
  title: string
  type: string
  status: string
  priority: string
  planned_start_at?: string
  plot_id?: string
}

const statusColor: Record<string, string> = {
  planned: 'blue', in_progress: 'orange', completed: 'green', cancelled: 'default',
}

/**
 * 移动端 H5 任务执行页（specs/11.3）：今日任务列表 + GPS 定位完成任务。
 * 响应式全宽布局，按钮/字体放大便于户外操作。
 */
export default function MobileTasks() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [completing, setCompleting] = useState<TaskRow | null>(null)
  const [note, setNote] = useState('')
  const [gps, setGps] = useState<{ lat: number; lng: number; accuracy?: number } | null>(null)
  const [gpsBusy, setGpsBusy] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [queueLen, setQueueLen] = useState(0)

  function refreshQueueLen() {
    void offlineLen().then(setQueueLen)
  }
  useEffect(() => { refreshQueueLen() }, [])

  const today = useQuery({
    queryKey: ['mobile-today'],
    queryFn: () => {
      const start = dayjs().startOf('day').format('YYYY-MM-DDTHH:mm:ssZ')
      const end = dayjs().endOf('day').format('YYYY-MM-DDTHH:mm:ssZ')
      return listPage<TaskRow>('/agricultural-tasks', { from: start, to: end, page_size: 50 })
    },
    refetchInterval: 30_000,
  })

  // 尝试首次进入自动定位
  useEffect(() => {
    if ('geolocation' in navigator) {
      navigator.geolocation.getCurrentPosition(
        (pos) => setGps({ lat: pos.coords.latitude, lng: pos.coords.longitude, accuracy: pos.coords.accuracy }),
        () => undefined,
        { enableHighAccuracy: true, timeout: 8000 },
      )
    }
  }, [])

  function isOfflineError(err: unknown): boolean {
  if (!navigator.onLine) return true
  const msg = err instanceof Error ? err.message : String(err)
  return msg.includes('Network Error') || msg.includes('timeout') || msg.includes('ERR_INTERNET')
}

function captureGPS() {
    if (!('geolocation' in navigator)) {
      message.error(t('mobile.gpsUnsupported'))
      return
    }
    setGpsBusy(true)
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        setGps({ lat: pos.coords.latitude, lng: pos.coords.longitude, accuracy: pos.coords.accuracy })
        setGpsBusy(false)
        message.success(t('mobile.gpsCaptured'))
      },
      (err) => {
        setGpsBusy(false)
        message.error(`${t('mobile.gpsFailed')}: ${err.message}`)
      },
      { enableHighAccuracy: true, timeout: 10000 },
    )
  }

  async function complete() {
    if (!completing) return
    setSubmitting(true)
    const body: Record<string, unknown> = { note: note || '' }
    if (gps) {
      body.latitude = gps.lat
      body.longitude = gps.lng
    }
    try {
      await postJSON(`/agricultural-tasks/${completing.id}/complete`, body)
      message.success(t('mobile.completed'))
      setCompleting(null)
      setNote('')
      void qc.invalidateQueries({ queryKey: ['mobile-today'] })
    } catch (err) {
      // Offline capture (specs/11.4): queue for auto-flush on connectivity regain.
      if (isOfflineError(err)) {
        await offlineEnqueue({
          kind: 'task-complete',
          endpoint: `/api/v1/agricultural-tasks/${completing.id}/complete`,
          body,
        })
        message.warning(`${t('mobile.queued')} (${t('mobile.queuedHint')})`)
        setCompleting(null)
        setNote('')
        refreshQueueLen()
        return
      }
      message.error(apiError(err))
    } finally {
      setSubmitting(false)
    }
  }

  async function start(id: string) {
    try {
      await postJSON(`/agricultural-tasks/${id}/start`)
      void qc.invalidateQueries({ queryKey: ['mobile-today'] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  const openTasks = (today.data?.items ?? []).filter((t: TaskRow) => t.status !== 'completed' && t.status !== 'cancelled')
  const doneTasks = (today.data?.items ?? []).filter((t: TaskRow) => t.status === 'completed')

  return (
    <div style={{ padding: 12, maxWidth: 640, margin: '0 auto' }}>
      <Space style={{ marginBottom: 12, width: '100%', justifyContent: 'space-between' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          <CarryOutOutlined /> {t('mobile.title')}
        </Typography.Title>
        <Space>
          {queueLen > 0 && <Tag color="orange">{t('mobile.pendingOffline')}: {queueLen}</Tag>}
          <Typography.Text type="secondary">{dayjs().format('YYYY-MM-DD dddd')}</Typography.Text>
        </Space>
      </Space>

      <Card size="small" style={{ marginBottom: 12 }}>
        <Space>
          <Button icon={<EnvironmentOutlined />} loading={gpsBusy} onClick={captureGPS}>
            {t('mobile.captureGPS')}
          </Button>
          {gps && (
            <Typography.Text className="mono" style={{ fontSize: 12 }}>
              {gps.lat.toFixed(5)}, {gps.lng.toFixed(5)} ±{gps.accuracy?.toFixed(0)}m
            </Typography.Text>
          )}
        </Space>
      </Card>

      <Typography.Text strong>{t('mobile.openTasks')} ({openTasks.length})</Typography.Text>
      <List
        dataSource={openTasks}
        locale={{ emptyText: <Empty description={t('mobile.allDone')} /> }}
        renderItem={(task: TaskRow) => (
          <List.Item
            style={{ padding: '12px 4px' }}
            actions={[
              task.status === 'planned' && (
                <Button key="s" size="middle" onClick={() => start(task.id)}>{t('tasks.start')}</Button>
              ),
              <Button key="c" size="middle" type="primary" onClick={() => setCompleting(task)}>
                {t('tasks.complete')}
              </Button>,
            ].filter(Boolean)}
          >
            <List.Item.Meta
              title={
                <Space wrap>
                  <Tag color={statusColor[task.status] ?? 'default'}>{t(`status.${task.status}`)}</Tag>
                  <b style={{ fontSize: 15 }}>{task.title}</b>
                  <Tag color={task.priority === 'high' || task.priority === 'urgent' ? 'volcano' : 'default'}>
                    {task.priority}
                  </Tag>
                </Space>
              }
              description={
                <span>
                  {task.type} · {task.planned_start_at ? dayjs(task.planned_start_at).format('HH:mm') : '-'}
                </span>
              }
            />
          </List.Item>
        )}
      />

      {doneTasks.length > 0 && (
        <>
          <Typography.Text strong type="secondary">{t('mobile.doneTasks')} ({doneTasks.length})</Typography.Text>
          <List
            size="small"
            dataSource={doneTasks}
            renderItem={(task: TaskRow) => (
              <List.Item>
                <List.Item.Meta
                  title={<span>✅ {task.title}</span>}
                  description={task.planned_start_at ? dayjs(task.planned_start_at).format('HH:mm') : undefined}
                />
              </List.Item>
            )}
          />
        </>
      )}

      <Modal
        open={completing !== null}
        title={`${t('tasks.complete')} · ${completing?.title ?? ''}`}
        onCancel={() => setCompleting(null)}
        onOk={complete}
        confirmLoading={submitting}
        okText={t('tasks.complete')}
      >
        <Alert
          type="info" showIcon style={{ marginBottom: 12 }}
          message={gps
            ? `${t('mobile.gpsAttached')}: ${gps.lat.toFixed(5)}, ${gps.lng.toFixed(5)}`
            : t('mobile.gpsHint')}
        />
        {completing?.status === 'planned' && (
          <Button
            block style={{ marginBottom: 12 }}
            onClick={() => { if (completing) { void start(completing.id); setCompleting({ ...completing, status: 'in_progress' }) } }}
          >
            {t('tasks.start')} →
          </Button>
        )}
        <Typography.Text>{t('tasks.note')}</Typography.Text>
        <Input.TextArea
          rows={3}
          value={note}
          onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => setNote(e.target.value)}
          style={{ marginTop: 6 }}
        />
        <Space style={{ marginTop: 10 }}>
          <Button size="small" icon={<EnvironmentOutlined />} loading={gpsBusy} onClick={captureGPS}>
            {t('mobile.captureGPS')}
          </Button>
          <Button size="small" icon={<ReloadOutlined />} onClick={() => void qc.invalidateQueries({ queryKey: ['mobile-today'] })}>
            {t('common.refresh')}
          </Button>
        </Space>
      </Modal>
    </div>
  )
}
